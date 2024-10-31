package policyframework

import (
	"context"
	"embed"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	policyaddon "open-cluster-management.io/governance-policy-addon-controller/pkg/addon"
)

const (
	addonName                       = "governance-policy-framework"
	evaluationConcurrencyAnnotation = "policy-evaluation-concurrency"
	clientQPSAnnotation             = "client-qps"
	clientBurstAnnotation           = "client-burst"
	prometheusEnabledAnnotation     = "prometheus-metrics-enabled"
	onMulticlusterHubAnnotation     = "addon.open-cluster-management.io/on-multicluster-hub"
	// Should only be set when the hub cluster is imported in a global hub
	syncPoliciesOnMulticlusterHubAnnotation = "policy.open-cluster-management.io/sync-policies-on-multicluster-hub"
)

var log = ctrl.Log.WithName("policyframework")

// FS go:embed
//
//go:embed manifests
//go:embed manifests/managedclusterchart
//go:embed manifests/managedclusterchart/templates/_helpers.tpl
var FS embed.FS

var agentPermissionFiles = []string{
	// role with RBAC rules to access resources on hub
	"manifests/hubpermissions/role.yaml",
	// rolebinding to bind the above role to a certain user group
	"manifests/hubpermissions/rolebinding.yaml",
}

type UserArgs struct {
	policyaddon.UserArgs
	SyncPoliciesOnMulticlusterHub bool  `json:"syncPoliciesOnMulticlusterHub,omitempty"`
	EvaluationConcurrency         uint8 `json:"evaluationConcurrency,omitempty"`
	ClientQPS                     uint8 `json:"clientQPS,omitempty"` //nolint:tagliatelle
	ClientBurst                   uint8 `json:"clientBurst,omitempty"`
}

type userValues struct {
	OnMulticlusterHub             bool                     `json:"onMulticlusterHub"`
	GlobalValues                  policyaddon.GlobalValues `json:"global"`
	KubernetesDistribution        string                   `json:"kubernetesDistribution"`
	HostingKubernetesDistribution string                   `json:"hostingKubernetesDistribution"`
	Prometheus                    map[string]interface{}   `json:"prometheus"`
	UserArgs                      UserArgs                 `json:"args"`
}

func getValues(ctx context.Context, clusterClient *clusterv1client.Clientset) func(*clusterv1.ManagedCluster,
	*addonapiv1alpha1.ManagedClusterAddOn,
) (addonfactory.Values, error) {
	return func(
		cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn,
	) (addonfactory.Values, error) {
		userValues := userValues{
			OnMulticlusterHub: false,
			GlobalValues: policyaddon.GlobalValues{
				ImagePullPolicy: "IfNotPresent",
				ImagePullSecret: "open-cluster-management-image-pull-credentials",
				ImageOverrides: map[string]string{
					"governance_policy_framework_addon": os.Getenv("GOVERNANCE_POLICY_FRAMEWORK_ADDON_IMAGE"),
				},
				ProxyConfig: map[string]string{
					"HTTP_PROXY":  "",
					"HTTPS_PROXY": "",
					"NO_PROXY":    "",
				},
			},
			Prometheus: map[string]interface{}{},
			UserArgs: UserArgs{
				UserArgs: policyaddon.UserArgs{
					LogEncoder:  "console",
					LogLevel:    0,
					PkgLogLevel: -1,
				},
				SyncPoliciesOnMulticlusterHub: false,
				// Defaults from `values.yaml` will be used if these stay at 0.
				EvaluationConcurrency: 0,
				ClientQPS:             0,
				ClientBurst:           0,
			},
		}

		annotations := addon.GetAnnotations()
		hostingClusterName := annotations[addonapiv1alpha1.HostingClusterNameAnnotationKey]

		// special case for local-cluster
		isLocal := cluster.Name == "local-cluster" ||
			hostingClusterName == "local-cluster" ||
			cluster.GetLabels()["local-cluster"] == "true"
		if isLocal {
			userValues.OnMulticlusterHub = true
		}

		userValues.KubernetesDistribution = policyaddon.GetClusterVendor(cluster)

		if hostingClusterName != "" {
			hostingCluster, err := clusterClient.ClusterV1().ManagedClusters().Get(
				ctx, hostingClusterName, metav1.GetOptions{},
			)
			if err != nil {
				return nil, err
			}

			userValues.HostingKubernetesDistribution = policyaddon.GetClusterVendor(hostingCluster)
		} else {
			userValues.HostingKubernetesDistribution = userValues.KubernetesDistribution
		}

		// The ManagedClusterAddOn's annotation has higher priority, though it'd be quite unusual to set conflicting
		// values.
		for _, annotations := range []map[string]string{cluster.GetAnnotations(), annotations} {
			if val, ok := annotations[onMulticlusterHubAnnotation]; ok {
				if strings.EqualFold(val, "true") {
					userValues.OnMulticlusterHub = true
				} else if strings.EqualFold(val, "false") {
					userValues.OnMulticlusterHub = false
				}
			}

			if val, ok := annotations[syncPoliciesOnMulticlusterHubAnnotation]; ok {
				if strings.EqualFold(val, "true") {
					userValues.UserArgs.SyncPoliciesOnMulticlusterHub = true
				} else if strings.EqualFold(val, "false") {
					userValues.UserArgs.SyncPoliciesOnMulticlusterHub = false
				}
			}
		}

		if val, ok := annotations[policyaddon.PolicyLogLevelAnnotation]; ok {
			logLevel := policyaddon.GetLogLevel(addonName, val)
			userValues.UserArgs.UserArgs.LogLevel = logLevel
			userValues.UserArgs.UserArgs.PkgLogLevel = logLevel - 2
		}

		if val, ok := annotations[evaluationConcurrencyAnnotation]; ok {
			value, err := strconv.ParseUint(val, 10, 8)
			if err != nil {
				log.Error(err, fmt.Sprintf(
					"Failed to verify '%s' annotation value '%s' for component %s (falling back to default value %d)",
					evaluationConcurrencyAnnotation, val, addonName, userValues.UserArgs.EvaluationConcurrency),
				)
			} else {
				// This is safe because we specified the uint8 in ParseUint
				userValues.UserArgs.EvaluationConcurrency = uint8(value)
			}
		}

		if val, ok := annotations[clientQPSAnnotation]; ok {
			value, err := strconv.ParseUint(val, 10, 8)
			if err != nil {
				log.Error(err, fmt.Sprintf(
					"Failed to verify '%s' annotation value '%s' for component %s (falling back to default value %d)",
					clientQPSAnnotation, val, addonName, userValues.UserArgs.ClientQPS),
				)
			} else {
				// This is safe because we specified the uint8 in ParseUint
				userValues.UserArgs.ClientQPS = uint8(value)
			}
		} else { // not set explicitly
			userValues.UserArgs.ClientQPS = userValues.UserArgs.EvaluationConcurrency * 15
		}

		if val, ok := annotations[clientBurstAnnotation]; ok {
			value, err := strconv.ParseUint(val, 10, 8)
			if err != nil {
				log.Error(err, fmt.Sprintf(
					"Failed to verify '%s' annotation value '%s' for component %s (falling back to default value %d)",
					clientBurstAnnotation, val, addonName, userValues.UserArgs.ClientBurst),
				)
			} else {
				// This is safe because we specified the uint8 in ParseUint
				userValues.UserArgs.ClientBurst = uint8(value)
			}
		} else if userValues.UserArgs.EvaluationConcurrency != 0 {
			// only scale with concurrency if concurrency was set.
			userValues.UserArgs.ClientBurst = userValues.UserArgs.EvaluationConcurrency*22 + 1
		}

		// Enable Prometheus metrics by default on OpenShift
		userValues.Prometheus["enabled"] = userValues.HostingKubernetesDistribution == "OpenShift"

		if val, ok := annotations[prometheusEnabledAnnotation]; ok {
			valBool, err := strconv.ParseBool(val)
			if err != nil {
				log.Error(err, fmt.Sprintf(
					"Failed to verify '%s' annotation value '%s' for component %s (falling back to default value %v)",
					prometheusEnabledAnnotation, val, addonName, userValues.Prometheus["enabled"]),
				)
			} else {
				userValues.Prometheus["enabled"] = valBool
			}
		}

		return addonfactory.JsonStructToValues(userValues)
	}
}

// mandateValues sets deployment variables regardless of user overrides. As a result, caution should
// be taken when adding settings to this function.
func mandateValues(
	cluster *clusterv1.ManagedCluster,
	mcao *addonapiv1alpha1.ManagedClusterAddOn,
) (addonfactory.Values, error) {
	values := addonfactory.Values{}

	// Don't allow replica overrides for older Kubernetes
	if policyaddon.IsOldKubernetes(cluster) {
		values["replicas"] = 1
	}

	if !mcao.DeletionTimestamp.IsZero() {
		values["uninstallationAnnotation"] = "true"
	}

	return values, nil
}

func GetAgentAddon(ctx context.Context, controllerContext *controllercmd.ControllerContext) (agent.AgentAddon, error) {
	registrationOption := policyaddon.NewRegistrationOption(
		controllerContext,
		addonName,
		agentPermissionFiles,
		FS)

	addonClient, err := addonv1alpha1client.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve addon client: %w", err)
	}

	clusterClient, err := policyaddon.GetManagedClusterClient(ctx, controllerContext.KubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize a managed cluster client: %w", err)
	}

	return addonfactory.NewAgentAddonFactory(addonName, FS, "manifests/managedclusterchart").
		WithConfigGVRs(utils.AddOnDeploymentConfigGVR).
		WithGetValuesFuncs(
			addonfactory.GetAddOnDeploymentConfigValues(
				addonfactory.NewAddOnDeploymentConfigGetter(addonClient),
				addonfactory.ToAddOnNodePlacementValues,
				addonfactory.ToAddOnCustomizedVariableValues,
			),
			getValues(ctx, clusterClient),
			addonfactory.GetValuesFromAddonAnnotation,
			mandateValues,
		).
		WithManagedClusterClient(clusterClient).
		WithAgentRegistrationOption(registrationOption).
		WithAgentInstallNamespace(
			policyaddon.
				CommonAgentInstallNamespaceFromDeploymentConfigFunc(utils.NewAddOnDeploymentConfigGetter(addonClient)),
		).
		WithScheme(policyaddon.Scheme).
		WithAgentHostedModeEnabledOption().
		BuildHelmAgentAddon()
}

func GetAndAddAgent(
	ctx context.Context, mgr addonmanager.AddonManager, controllerContext *controllercmd.ControllerContext,
) error {
	return policyaddon.GetAndAddAgent(ctx, mgr, addonName, controllerContext, GetAgentAddon)
}
