package configpolicy

import (
	"embed"
	"fmt"
	"os"
	"strconv"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	policyaddon "open-cluster-management.io/governance-policy-addon-controller/pkg/addon"
)

const (
	addonName                       = "config-policy-controller"
	evaluationConcurrencyAnnotation = "policy-evaluation-concurrency"
	prometheusEnabledAnnotation     = "prometheus-metrics-enabled"
)

var log = ctrl.Log.WithName("configpolicy")

type UserArgs struct {
	policyaddon.UserArgs
	EvaluationConcurrency uint8 `json:"evaluationConcurrency,"`
}

type UserValues struct {
	GlobalValues           policyaddon.GlobalValues `json:"global,"`
	KubernetesDistribution string                   `json:"kubernetesDistribution"`
	Prometheus             map[string]interface{}   `json:"prometheus"`
	UserArgs               UserArgs                 `json:"args,"`
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

func getValues(cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
) (addonfactory.Values, error) {
	userValues := UserValues{
		GlobalValues: policyaddon.GlobalValues{
			ImagePullPolicy: "IfNotPresent",
			ImagePullSecret: "open-cluster-management-image-pull-credentials",
			ImageOverrides: map[string]string{
				"config_policy_controller": os.Getenv("CONFIG_POLICY_CONTROLLER_IMAGE"),
				"kube_rbac_proxy":          os.Getenv("KUBE_RBAC_PROXY_IMAGE"),
			},
			NodeSelector: map[string]string{},
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
			EvaluationConcurrency: 2,
		},
	}

	for _, cc := range cluster.Status.ClusterClaims {
		if cc.Name == "product.open-cluster-management.io" {
			userValues.KubernetesDistribution = cc.Value

			break
		}
	}

	if val, ok := addon.GetAnnotations()[policyaddon.PolicyLogLevelAnnotation]; ok {
		logLevel := policyaddon.GetLogLevel(addonName, val)
		userValues.UserArgs.LogLevel = logLevel
		userValues.UserArgs.PkgLogLevel = logLevel - 2
	}

	if val, ok := addon.GetAnnotations()[evaluationConcurrencyAnnotation]; ok {
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

	// Enable Prometheus metrics by default on OpenShift
	userValues.Prometheus["enabled"] = userValues.KubernetesDistribution == "OpenShift"
	if userValues.KubernetesDistribution == "OpenShift" {
		userValues.Prometheus["serviceMonitor"] = map[string]interface{}{"namespace": "openshift-monitoring"}
	}

	if val, ok := addon.GetAnnotations()[prometheusEnabledAnnotation]; ok {
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

func GetAgentAddon(controllerContext *controllercmd.ControllerContext) (agent.AgentAddon, error) {
	registrationOption := policyaddon.NewRegistrationOption(
		controllerContext,
		addonName,
		agentPermissionFiles,
		FS)

	return addonfactory.NewAgentAddonFactory(addonName, FS, "manifests/managedclusterchart").
		WithGetValuesFuncs(getValues, addonfactory.GetValuesFromAddonAnnotation).
		WithAgentRegistrationOption(registrationOption).
		WithScheme(policyaddon.Scheme).
		WithAgentHostedModeEnabledOption().
		BuildHelmAgentAddon()
}

func GetAndAddAgent(mgr addonmanager.AddonManager, controllerContext *controllercmd.ControllerContext) error {
	return policyaddon.GetAndAddAgent(mgr, addonName, controllerContext, GetAgentAddon)
}
