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
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	policyaddon "open-cluster-management.io/governance-policy-addon-controller/pkg/addon"
)

const (
	addonName                       = "config-policy-controller"
	evaluationConcurrencyAnnotation = "policy-evaluation-concurrency"
	clientQPSAnnotation             = "client-qps"
	clientBurstAnnotation           = "client-burst"
	prometheusEnabledAnnotation     = "prometheus-metrics-enabled"
)

var log = ctrl.Log.WithName("configpolicy")

type UserArgs struct {
	policyaddon.UserArgs
	EvaluationConcurrency uint8 `json:"evaluationConcurrency,omitempty"`
	ClientQPS             uint8 `json:"clientQPS,omitempty"` //nolint:tagliatelle
	ClientBurst           uint8 `json:"clientBurst,omitempty"`
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
			// Defaults from `values.yaml` will be used if these stay at 0.
			EvaluationConcurrency: 0,
			ClientQPS:             0, // will be set based on concurrency if not explicitly set
			ClientBurst:           0, // will be set based on concurrency if not explicitly set
		},
	}

	for _, cc := range cluster.Status.ClusterClaims {
		if cc.Name == "product.open-cluster-management.io" {
			userValues.KubernetesDistribution = cc.Value

			break
		}
	}

	// Enable Prometheus metrics by default on OpenShift
	userValues.Prometheus["enabled"] = userValues.KubernetesDistribution == "OpenShift"
	if userValues.KubernetesDistribution == "OpenShift" {
		userValues.Prometheus["serviceMonitor"] = map[string]interface{}{"namespace": "openshift-monitoring"}
	}

	annotations := addon.GetAnnotations()

	if val, ok := annotations[policyaddon.PolicyLogLevelAnnotation]; ok {
		logLevel := policyaddon.GetLogLevel(addonName, val)
		userValues.UserArgs.LogLevel = logLevel
		userValues.UserArgs.PkgLogLevel = logLevel - 2
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

	return addonfactory.JsonStructToValues(userValues)
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
		).
		WithAgentRegistrationOption(registrationOption).
		WithScheme(policyaddon.Scheme).
		WithAgentHostedModeEnabledOption().
		BuildHelmAgentAddon()
}

func GetAndAddAgent(mgr addonmanager.AddonManager, controllerContext *controllercmd.ControllerContext) error {
	return policyaddon.GetAndAddAgent(mgr, addonName, controllerContext, GetAgentAddon)
}
