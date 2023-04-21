package policyframework

import (
	"embed"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/blang/semver/v4"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	policyaddon "open-cluster-management.io/governance-policy-addon-controller/pkg/addon"
)

const (
	addonName                   = "governance-policy-framework"
	prometheusEnabledAnnotation = "prometheus-metrics-enabled"
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

type userValues struct {
	OnMulticlusterHub      bool                     `json:"onMulticlusterHub"`
	GlobalValues           policyaddon.GlobalValues `json:"global"`
	KubernetesDistribution string                   `json:"kubernetesDistribution"`
	Prometheus             map[string]interface{}   `json:"prometheus"`
	UserArgs               policyaddon.UserArgs     `json:"args"`
}

func getValues(cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
) (addonfactory.Values, error) {
	userValues := userValues{
		OnMulticlusterHub: false,
		GlobalValues: policyaddon.GlobalValues{
			ImagePullPolicy: "IfNotPresent",
			ImagePullSecret: "open-cluster-management-image-pull-credentials",
			ImageOverrides: map[string]string{
				"governance_policy_framework_addon": os.Getenv("GOVERNANCE_POLICY_FRAMEWORK_ADDON_IMAGE"),
				"kube_rbac_proxy":                   os.Getenv("KUBE_RBAC_PROXY_IMAGE"),
			},
			ProxyConfig: map[string]string{
				"HTTP_PROXY":  "",
				"HTTPS_PROXY": "",
				"NO_PROXY":    "",
			},
		},
		Prometheus: map[string]interface{}{},
		UserArgs: policyaddon.UserArgs{
			LogEncoder:  "console",
			LogLevel:    0,
			PkgLogLevel: -1,
		},
	}
	// special case for local-cluster
	if cluster.Name == "local-cluster" {
		userValues.OnMulticlusterHub = true
	}

	for _, cc := range cluster.Status.ClusterClaims {
		if cc.Name == "product.open-cluster-management.io" {
			userValues.KubernetesDistribution = cc.Value

			break
		}
	}

	annotations := addon.GetAnnotations()

	if val, ok := annotations["addon.open-cluster-management.io/on-multicluster-hub"]; ok {
		if strings.EqualFold(val, "true") {
			userValues.OnMulticlusterHub = true
		} else if strings.EqualFold(val, "false") {
			// the special case can still be overridden by this annotation
			userValues.OnMulticlusterHub = false
		}
	}

	if val, ok := annotations[policyaddon.PolicyLogLevelAnnotation]; ok {
		logLevel := policyaddon.GetLogLevel(addonName, val)
		userValues.UserArgs.LogLevel = logLevel
		userValues.UserArgs.PkgLogLevel = logLevel - 2
	}

	// Enable Prometheus metrics by default on OpenShift
	userValues.Prometheus["enabled"] = userValues.KubernetesDistribution == "OpenShift"
	if userValues.KubernetesDistribution == "OpenShift" {
		userValues.Prometheus["serviceMonitor"] = map[string]interface{}{"namespace": "openshift-monitoring"}
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

	return addonfactory.JsonStructToValues(userValues)
}

// mandateValues sets deployment variables regardless of user overrides. As a result, caution should
// be taken when adding settings to this function.
func mandateValues(
	cluster *clusterv1.ManagedCluster,
	_ *addonapiv1alpha1.ManagedClusterAddOn,
) (addonfactory.Values, error) {
	values := addonfactory.Values{}

	oldKubernetes := false

	for _, cc := range cluster.Status.ClusterClaims {
		if cc.Name == "kubeversion.open-cluster-management.io" {
			k8sVersion, err := semver.ParseTolerant(cc.Value)
			if err != nil {
				continue
			}

			if k8sVersion.Major <= 1 && k8sVersion.Minor < 14 {
				oldKubernetes = true
			}

			break
		}
	}

	// Don't allow replica overrides for older Kubernetes
	if oldKubernetes {
		values["replicas"] = 1
	}

	return values, nil
}

func GetAgentAddon(controllerContext *controllercmd.ControllerContext) (agent.AgentAddon, error) {
	registrationOption := policyaddon.NewRegistrationOption(
		controllerContext,
		addonName,
		agentPermissionFiles,
		FS)

	addonClient, err := addonv1alpha1client.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve addon client: %w", err)
	}

	return addonfactory.NewAgentAddonFactory(addonName, FS, "manifests/managedclusterchart").
		WithConfigGVRs(addonfactory.AddOnDeploymentConfigGVR).
		WithGetValuesFuncs(
			addonfactory.GetAddOnDeploymentConfigValues(
				addonfactory.NewAddOnDeploymentConfigGetter(addonClient),
				addonfactory.ToAddOnNodePlacementValues,
				addonfactory.ToAddOnCustomizedVariableValues,
			),
			getValues,
			addonfactory.GetValuesFromAddonAnnotation,
			mandateValues,
		).
		WithAgentRegistrationOption(registrationOption).
		WithScheme(policyaddon.Scheme).
		WithAgentHostedModeEnabledOption().
		BuildHelmAgentAddon()
}

func GetAndAddAgent(mgr addonmanager.AddonManager, controllerContext *controllercmd.ControllerContext) error {
	return policyaddon.GetAndAddAgent(mgr, addonName, controllerContext, GetAgentAddon)
}
