package configpolicy

import (
	"context"
	"embed"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"k8s.io/apimachinery/pkg/api/errors"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	addonlistersv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterlistersv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	policyaddon "open-cluster-management.io/governance-policy-addon-controller/pkg/addon"
)

const (
	addonName                        = "config-policy-controller"
	evaluationConcurrencyAnnotation  = "policy-evaluation-concurrency"
	clientQPSAnnotation              = "client-qps"
	clientBurstAnnotation            = "client-burst"
	prometheusEnabledAnnotation      = "prometheus-metrics-enabled"
	operatorPolicyDisabledAnnotation = "operator-policy-disabled"
	standaloneTemplatingAddonName    = "governance-standalone-hub-templating"
)

var log = ctrl.Log.WithName("configpolicy")

type UserArgs struct {
	policyaddon.UserArgs
	EvaluationConcurrency uint8 `json:"evaluationConcurrency,omitempty"`
	ClientQPS             uint8 `json:"clientQPS,omitempty"` //nolint:tagliatelle
	ClientBurst           uint8 `json:"clientBurst,omitempty"`
}

type UserValues struct {
	GlobalValues                  policyaddon.GlobalValues `json:"global,"`
	KubernetesDistribution        string                   `json:"kubernetesDistribution"`
	HostingKubernetesDistribution string                   `json:"hostingKubernetesDistribution"`
	Prometheus                    map[string]interface{}   `json:"prometheus"`
	OperatorPolicy                map[string]interface{}   `json:"operatorPolicy"`
	UserArgs                      UserArgs                 `json:"args,"`
	StandaloneHubTemplatingSecret string                   `json:"standaloneHubTemplatingSecret"`
}

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

func getValues(
	clusterClient clusterlistersv1.ManagedClusterLister,
	addonClient addonlistersv1alpha1.ManagedClusterAddOnLister,
) func(*clusterv1.ManagedCluster, *addonapiv1alpha1.ManagedClusterAddOn) (addonfactory.Values, error) {
	return func(
		cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn,
	) (addonfactory.Values, error) {
		userValues := UserValues{
			GlobalValues: policyaddon.GlobalValues{
				ImagePullPolicy: "IfNotPresent",
				ImagePullSecret: "open-cluster-management-image-pull-credentials",
				ImageOverrides: map[string]string{
					"config_policy_controller": os.Getenv("CONFIG_POLICY_CONTROLLER_IMAGE"),
				},
				ProxyConfig: map[string]string{
					"HTTP_PROXY":  "",
					"HTTPS_PROXY": "",
					"NO_PROXY":    "",
				},
			},
			Prometheus:     map[string]interface{}{},
			OperatorPolicy: map[string]interface{}{},
			UserArgs: UserArgs{
				UserArgs: policyaddon.UserArgs{
					LogEncoder:  "console",
					LogLevel:    0,
					PkgLogLevel: -1,
				},
				// Defaults from `values.yaml` will be used if these stay at 0.
				EvaluationConcurrency: 0,
				ClientQPS:             0, // will be set based on concurrency if not explicitly set
				ClientBurst:           0, // will be set based on concurrency if not explicitly set
			},
		}

		userValues.KubernetesDistribution = policyaddon.GetClusterVendor(cluster)

		hostingClusterName := addon.Annotations["addon.open-cluster-management.io/hosting-cluster-name"]
		if hostingClusterName != "" {
			hostingCluster, err := clusterClient.Get(hostingClusterName)
			if err != nil {
				return nil, err
			}

			userValues.HostingKubernetesDistribution = policyaddon.GetClusterVendor(hostingCluster)
		} else {
			userValues.HostingKubernetesDistribution = userValues.KubernetesDistribution
		}

		_, err := addonClient.ManagedClusterAddOns(addon.Namespace).Get(standaloneTemplatingAddonName)
		if !errors.IsNotFound(err) {
			if err != nil {
				return nil, err
			}

			userValues.StandaloneHubTemplatingSecret = standaloneTemplatingAddonName + "-hub-kubeconfig"
		}

		// Enable Prometheus metrics by default on OpenShift
		userValues.Prometheus["enabled"] = userValues.HostingKubernetesDistribution == "OpenShift"

		// Disable OperatorPolicy if the cluster is not on OpenShift version 4.y
		userValues.OperatorPolicy["disabled"] = cluster.Labels["openshiftVersion-major"] != "4"

		// Set the default namespace for OperatorPolicy for OpenShift 4
		if cluster.Labels["openshiftVersion-major"] == "4" {
			userValues.OperatorPolicy["defaultNamespace"] = "openshift-operators"
		}

		annotations := addon.GetAnnotations()

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

		if val, ok := annotations[operatorPolicyDisabledAnnotation]; ok {
			valBool, err := strconv.ParseBool(val)
			if err != nil {
				log.Error(err, fmt.Sprintf(
					"Failed to verify '%s' annotation value '%s' for component %s (falling back to default value %v)",
					operatorPolicyDisabledAnnotation, val, addonName, userValues.OperatorPolicy["disabled"]),
				)
			} else {
				userValues.OperatorPolicy["disabled"] = valBool
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
		FS,
		false)

	addonClient, err := addonv1alpha1client.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve addon client: %w", err)
	}

	addonInformer := addoninformers.NewSharedInformerFactory(addonClient, 10*time.Minute).
		Addon().V1alpha1().ManagedClusterAddOns()
	go addonInformer.Informer().Run(ctx.Done())

	clusterClient, err := clusterv1client.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize a managed cluster client: %w", err)
	}

	clusterInformer := clusterv1informers.NewSharedInformerFactory(clusterClient, 10*time.Minute).
		Cluster().V1().ManagedClusters()
	go clusterInformer.Informer().Run(ctx.Done())

	return addonfactory.NewAgentAddonFactory(addonName, FS, "manifests/managedclusterchart").
		WithConfigGVRs(utils.AddOnDeploymentConfigGVR).
		WithGetValuesFuncs(
			addonfactory.GetAddOnDeploymentConfigValues(
				addonfactory.NewAddOnDeploymentConfigGetter(addonClient),
				addonfactory.ToAddOnNodePlacementValues,
				addonfactory.ToAddOnResourceRequirementsValues,
				addonfactory.ToAddOnCustomizedVariableValues,
			),
			getValues(clusterInformer.Lister(), addonInformer.Lister()),
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
