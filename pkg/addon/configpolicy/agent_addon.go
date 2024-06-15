package configpolicy

import (
	"context"
	"embed"
	"fmt"
	"os"
	"strconv"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
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
	OperatorPolicy         map[string]interface{}   `json:"operatorPolicy"`
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

	// Don't just set it to the value in the label, it might be something like "auto-detect"
	if cluster.Labels["vendor"] == "OpenShift" {
		userValues.KubernetesDistribution = "OpenShift"
	}

	for _, cc := range cluster.Status.ClusterClaims {
		if cc.Name == "product.open-cluster-management.io" {
			userValues.KubernetesDistribution = cc.Value

			break
		}
	}

	// Enable Prometheus metrics by default on OpenShift
	userValues.Prometheus["enabled"] = userValues.KubernetesDistribution == "OpenShift"

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

func toAddonResources(config addonapiv1alpha1.AddOnDeploymentConfig) (addonfactory.Values, error) {
	defaultRequestMem, err := resource.ParseQuantity("128Mi")
	if err != nil {
		return nil, fmt.Errorf("failed to parse default memory request: %w", err)
	}

	defaultLimitMem, err := resource.ParseQuantity("512Mi")
	if err != nil {
		return nil, fmt.Errorf("failed to parse default memory limit: %w", err)
	}

	jsonStruct := struct {
		Resources corev1.ResourceRequirements `json:"resources"`
	}{
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: defaultRequestMem,
			},
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: defaultLimitMem,
			},
		},
	}

	var newRequestMem, newLimitMem resource.Quantity

	for _, variable := range config.Spec.CustomizedVariables {
		switch variable.Name {
		case "RequestsMemory":
			newRequestMem, err = resource.ParseQuantity(variable.Value)
			if err != nil {
				klog.Info(fmt.Sprintf(
					"Failed to parse configured memory request '%s'. Falling back to the default %s.",
					variable.Value, defaultRequestMem.String(),
				))

				continue
			} else if newRequestMem.Cmp(defaultRequestMem) == -1 {
				klog.Info(fmt.Sprintf(
					"Refusing to set lower configured memory request '%s'. Falling back to the default %s.",
					newRequestMem.String(), defaultRequestMem.String(),
				))

				continue
			}

			jsonStruct.Resources.Requests = corev1.ResourceList{
				corev1.ResourceMemory: newRequestMem,
			}
		case "LimitsMemory":
			newLimitMem, err = resource.ParseQuantity(variable.Value)
			if err != nil {
				klog.Info(fmt.Sprintf(
					"Failed to parse configured memory limit '%s'. Falling back to the default %s.",
					variable.Value, defaultLimitMem.String(),
				))

				continue
			}

			if newLimitMem.Cmp(defaultLimitMem) == -1 {
				klog.Info(fmt.Sprintf(
					"Refusing to set a lower configured memory limit '%s'. Falling back to the default %s.",
					newLimitMem.String(), defaultLimitMem.String(),
				))

				continue
			}

			jsonStruct.Resources.Limits = corev1.ResourceList{
				corev1.ResourceMemory: newLimitMem,
			}
		}
	}

	if newRequestMem.Cmp(newLimitMem) == 1 {
		klog.Error(fmt.Sprintf("Configured request memory '%s' may not exceed configured limit '%s'. "+
			"Setting request equal to limit.", newRequestMem.String(), newLimitMem.String()))

		jsonStruct.Resources.Requests = corev1.ResourceList{
			corev1.ResourceMemory: newLimitMem,
		}
	}

	values, err := addonfactory.JsonStructToValues(jsonStruct)
	if err != nil {
		return nil, err
	}

	return values, nil
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
				toAddonResources,
			),
			getValues,
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
