package addon

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/blang/semver/v4"
	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterlistersv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var log = ctrl.Log.WithName("common")

const (
	PolicyAddonPauseAnnotation      = "policy-addon-pause"
	PolicyLogLevelAnnotation        = "log-level"
	EvaluationConcurrencyAnnotation = "policy-evaluation-concurrency"
	ClientQPSAnnotation             = "client-qps"
	ClientBurstAnnotation           = "client-burst"
	PrometheusEnabledAnnotation     = "prometheus-metrics-enabled"

	AnnotationParseErrorFmt = "Failed to verify '%s' annotation value '%s' for component %s " +
		"(falling back to default value %v)"
)

// CommonValues contains common values for the addon chart.
type CommonValues struct {
	BaseValues `json:",inline"`
	UserArgs   `json:",inline"`

	KubernetesDistribution        string `json:"kubernetesDistribution,omitempty"`
	HostingKubernetesDistribution string `json:"hostingKubernetesDistribution,omitempty"`
}

// UserArgs contains common controller flags for the addon chart.
type UserArgs struct {
	LogEncoder            string `json:"logEncoder,omitempty"`
	LogLevel              int8   `json:"logLevel,omitempty"`
	PkgLogLevel           int8   `json:"pkgLogLevel,omitempty"`
	EvaluationConcurrency uint8  `json:"evaluationConcurrency,omitempty"`
	ClientQPS             uint8  `json:"clientQPS,omitempty"` //nolint:tagliatelle
	ClientBurst           uint8  `json:"clientBurst,omitempty"`
}

// GlobalValues contains global values for the addon chart.
type GlobalValues struct {
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`
	ImagePullSecret string            `json:"imagePullSecret,omitempty"`
	ImageOverrides  map[string]string `json:"imageOverrides,omitempty"`
	ProxyConfig     ProxyConfig       `json:"proxyConfig"`
}

// ProxyConfig contains proxy configuration values for the addon chart.
//
//nolint:tagliatelle
type ProxyConfig struct {
	HTTPProxy  string `json:"HTTP_PROXY,omitempty"`
	HTTPSProxy string `json:"HTTPS_PROXY,omitempty"`
	NoProxy    string `json:"NO_PROXY,omitempty"`
}

// BaseValues contains base values for the addon chart.
type BaseValues struct {
	GlobalValues                  GlobalValues `json:"global"`
	OnMulticlusterHub             bool         `json:"onMulticlusterHub,omitempty"`
	KubernetesDistribution        string       `json:"kubernetesDistribution,omitempty"`
	HostingKubernetesDistribution string       `json:"hostingKubernetesDistribution,omitempty"`
	Prometheus                    Prometheus   `json:"prometheus"`
}

// Prometheus contains Prometheus metrics configuration values for the addon chart.
type Prometheus struct {
	Enabled        bool           `json:"enabled,omitempty"`
	ServiceMonitor ServiceMonitor `json:"serviceMonitor"`
}

// ServiceMonitor contains Prometheus ServiceMonitor configuration values for the addon chart.
type ServiceMonitor struct {
	Namespace *string `json:"namespace,omitempty"`
}

var Scheme = runtime.NewScheme()

func init() {
	err := scheme.AddToScheme(Scheme)
	if err != nil {
		log.Error(err, "Failed to add to scheme")
		os.Exit(1)
	}

	err = prometheusv1.AddToScheme(Scheme)
	if err != nil {
		log.Error(err, "Failed to add the Prometheus scheme to scheme")
		os.Exit(1)
	}
}

// NewRegistrationOption creates a new registration option for the addon.
func NewRegistrationOption(
	controllerContext *controllercmd.ControllerContext,
	addonName string,
	agentPermissionFiles []string,
	filesystem embed.FS,
	useClusterRole bool,
) *agent.RegistrationOption {
	applyManifestFromFile := func(file, clusterName string,
		kubeclient *kubernetes.Clientset, recorder events.Recorder,
	) error {
		groupIdx := 0 // 0 is a cluster-specific group

		if useClusterRole {
			groupIdx = 1 // 1 is a group for the entire addon
		}

		groups := agent.DefaultGroups(clusterName, addonName)
		config := struct {
			ClusterName string
			Group       string
		}{
			ClusterName: clusterName,
			Group:       groups[groupIdx],
		}

		results := resourceapply.ApplyDirectly(context.Background(),
			resourceapply.NewKubeClientHolder(kubeclient),
			recorder,
			resourceapply.NewResourceCache(),
			func(name string) ([]byte, error) {
				template, err := filesystem.ReadFile(file)
				if err != nil {
					return nil, err
				}

				return assets.MustCreateAssetFromTemplate(name, template, config).Data, nil
			},
			file,
		)

		for _, result := range results {
			if result.Error != nil {
				return result.Error
			}
		}

		return nil
	}

	kubeConfig := controllerContext.KubeConfig
	recorder := controllerContext.EventRecorder

	return &agent.RegistrationOption{
		CSRConfigurations: agent.KubeClientSignerConfigurations(addonName, addonName),
		CSRApproveCheck:   utils.DefaultCSRApprover(addonName),
		PermissionConfig: func(cluster *clusterv1.ManagedCluster, _ *addonapiv1alpha1.ManagedClusterAddOn) error {
			kubeclient, err := kubernetes.NewForConfig(kubeConfig)
			if err != nil {
				return err
			}

			for _, file := range agentPermissionFiles {
				if err := applyManifestFromFile(file, cluster.Name, kubeclient, recorder); err != nil {
					return err
				}
			}

			return nil
		},
	}
}

// GetClusterVendor determines the vendor of the cluster based on the labels
// and cluster claims.
func GetClusterVendor(cluster *clusterv1.ManagedCluster) string {
	var vendor string
	// Don't just set it to the value in the label, it might be something like "auto-detect"
	if cluster.Labels["vendor"] == "OpenShift" {
		vendor = "OpenShift"
	}

	for _, cc := range cluster.Status.ClusterClaims {
		if cc.Name == "product.open-cluster-management.io" {
			vendor = cc.Value

			break
		}
	}

	return vendor
}

// GetAndAddAgent adds the agent to the manager.
func GetAndAddAgent(
	ctx context.Context,
	mgr addonmanager.AddonManager,
	addonName string,
	controllerContext *controllercmd.ControllerContext,
	getAgent func(context.Context, *controllercmd.ControllerContext) (agent.AgentAddon, error),
) error {
	agentAddon, err := getAgent(ctx, controllerContext)
	if err != nil {
		return fmt.Errorf("failed getting the %v agent addon: %w", addonName, err)
	}

	agentAddon = &PolicyAgentAddon{agentAddon}

	err = mgr.AddAgent(agentAddon)
	if err != nil {
		return fmt.Errorf("failed adding the %v agent addon to the manager: %w", addonName, err)
	}

	return nil
}

// PolicyAgentAddon wraps the AgentAddon created from the addonfactory to override some behavior
type PolicyAgentAddon struct {
	agent.AgentAddon
}

// Manifests overrides the AgentAddon.Manifests method to return an error when
// the policy addon is paused.
func (pa *PolicyAgentAddon) Manifests(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
) ([]runtime.Object, error) {
	// Return error when pause annotation is set to short-circuit automatic addon updates
	pauseAnnotation := addon.GetAnnotations()[PolicyAddonPauseAnnotation]
	if pauseAnnotation == "true" {
		return nil, errors.New("the Policy Addon controller is paused due to the policy-addon-pause annotation")
	}

	return pa.AgentAddon.Manifests(cluster, addon)
}

// CommonAgentInstallNamespaceFromDeploymentConfigFunc returns a function that
// gets the agent install namespace for the addon from the deployment config.
func CommonAgentInstallNamespaceFromDeploymentConfigFunc(
	adcgetter utils.AddOnDeploymentConfigGetter,
) func(*addonapiv1alpha1.ManagedClusterAddOn) (string, error) {
	return func(addon *addonapiv1alpha1.ManagedClusterAddOn) (string, error) {
		if addon == nil {
			log.Info("failed to get addon install namespace, addon is nil")

			return "", nil
		}

		hostingClusterName := addon.Annotations["addon.open-cluster-management.io/hosting-cluster-name"]
		// Check it is hosted mode
		//nolint:staticcheck
		if hostingClusterName != "" && addon.Spec.InstallNamespace != "" {
			return addon.Spec.InstallNamespace, nil
		}

		config, err := utils.GetDesiredAddOnDeploymentConfig(addon, adcgetter)
		if err != nil {
			log.Error(err, "failed to get deployment config for addon "+addon.Name)

			return "", err
		}

		if config == nil {
			return "", nil
		}

		return config.Spec.AgentInstallNamespace, nil
	}
}

// GetLogLevel verifies the user-provided log level against Zap, returning 0 if the check fails.
func GetLogLevel(level string) (int8, error) {
	logDefault := int8(0)

	if level == "error" {
		return int8(-1), nil
	}

	logLevel, err := strconv.ParseInt(level, 10, 8)
	if err != nil || logLevel < -1 {
		return logDefault, fmt.Errorf("failed to parse log level value '%s' (falling back to default value %d): %w",
			level, logDefault, err)
	}

	// This is safe because we specified the int8 in ParseInt
	return int8(logLevel), nil
}

// SetLogLevel sets the log level for the addon, setting the package log level
// to 2 less than the user log level.
func (cv *CommonValues) SetLogLevel(value string) error {
	logLevel, err := GetLogLevel(value)
	cv.UserArgs.LogLevel = logLevel
	cv.UserArgs.PkgLogLevel = logLevel - 2

	return err
}

// SetEvaluationConcurrency sets the evaluation concurrency for the addon.
func (cv *CommonValues) SetEvaluationConcurrency(value string) error {
	evaluationConcurrency, err := strconv.ParseUint(value, 10, 8)
	if err != nil {
		return fmt.Errorf("failed to parse evaluation concurrency value '%s' (falling back to default value %d): %w",
			value, cv.UserArgs.EvaluationConcurrency, err)
	}

	// This is safe because we specified the uint8 in ParseUint
	cv.UserArgs.EvaluationConcurrency = uint8(evaluationConcurrency)

	return nil
}

// SetClientQPS sets the client QPS for the addon.
func (cv *CommonValues) SetClientQPS(value string) error {
	clientQPS, err := strconv.ParseUint(value, 10, 8)
	if err != nil {
		return fmt.Errorf("failed to parse client QPS value '%s' (falling back to default value %d): %w",
			value, cv.UserArgs.ClientQPS, err)
	}

	// This is safe because we specified the uint8 in ParseUint
	cv.UserArgs.ClientQPS = uint8(clientQPS)

	return nil
}

// SetClientBurstFromEvaluationConcurrency sets the client burst for the addon
// based on the evaluation concurrency.
func (cv *CommonValues) SetClientBurstFromEvaluationConcurrency() {
	if cv.UserArgs.EvaluationConcurrency != 0 && cv.UserArgs.ClientBurst == 0 {
		cv.UserArgs.ClientBurst = cv.UserArgs.EvaluationConcurrency*22 + 1
	}
}

// SetClientBurst sets the client burst for the addon.
func (cv *CommonValues) SetClientBurst(value string) error {
	clientBurst, err := strconv.ParseUint(value, 10, 8)
	if err != nil {
		return fmt.Errorf("failed to parse client burst value '%s' (falling back to default value %d): %w",
			value, cv.UserArgs.ClientBurst, err)
	}

	// This is safe because we specified the uint8 in ParseUint
	cv.UserArgs.ClientBurst = uint8(clientBurst)

	return nil
}

// SetPrometheusEnabled sets the Prometheus metrics enabled boolean for the
// addon chart, enabling metrics configurations to be deployed.
func (cv *CommonValues) SetPrometheusEnabled(value string) error {
	prometheusEnabled, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("failed to parse prometheus enabled boolean '%s' (falling back to default value %t): %w",
			value, false, err)
	}

	cv.Prometheus.Enabled = prometheusEnabled

	return nil
}

// SetCommonValues populates settings in the common chart values for the addon
// based on the environment. It returns an error for the respective component
// addon handler.
//
// Currently the only error is a fetch error for the hosting cluster, which
// would warrant a retry.
func (cv *CommonValues) SetCommonValues(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
	clusterClient clusterlistersv1.ManagedClusterLister,
) error {
	// Set the Kubernetes distribution for the current cluster
	cv.KubernetesDistribution = GetClusterVendor(cluster)

	// Set the Kubernetes distribution for the hosting cluster
	mangedClusterAddOnAnnotations := addon.GetAnnotations()

	hostingClusterName := mangedClusterAddOnAnnotations[addonapiv1alpha1.HostingClusterNameAnnotationKey]
	if hostingClusterName != "" {
		hostingCluster, err := clusterClient.Get(hostingClusterName)
		if err != nil {
			return err
		}

		cv.HostingKubernetesDistribution = GetClusterVendor(hostingCluster)
	} else {
		cv.HostingKubernetesDistribution = cv.KubernetesDistribution
	}

	// Enable Prometheus metrics by default on OpenShift
	cv.Prometheus.Enabled = cv.HostingKubernetesDistribution == "OpenShift"

	return nil
}

// SetCommonValuesFromAnnotations sets the common values for the addon chart
// using annotations on the ManagedClusterAddOn. It returns an aggregated error
// for the respective component addon handler.
func (cv *CommonValues) SetCommonValuesFromAnnotations(addon *addonapiv1alpha1.ManagedClusterAddOn) error {
	addonName := addon.Name
	mcaoAnnotations := addon.GetAnnotations()
	var aggregateErr error

	annotationToFuncMap := map[string]func(string) error{
		PolicyLogLevelAnnotation:        cv.SetLogLevel,
		EvaluationConcurrencyAnnotation: cv.SetEvaluationConcurrency,
		ClientQPSAnnotation:             cv.SetClientQPS,
		ClientBurstAnnotation:           cv.SetClientBurst,
		PrometheusEnabledAnnotation:     cv.SetPrometheusEnabled,
	}

	for annotation, fn := range annotationToFuncMap {
		if val, ok := mcaoAnnotations[annotation]; ok {
			if err := fn(val); err != nil {
				aggregateErr = errors.Join(aggregateErr,
					fmt.Errorf("failed to set value from annotation '%s' for addon '%s': %w",
						annotation, addonName, err))
			}
		}
	}

	cv.SetClientBurstFromEvaluationConcurrency()

	return nil
}

// MandateValues sets deployment variables regardless of user overrides. As a result, caution should
// be taken when adding settings to this function.
func MandateValues(
	_ *clusterv1.ManagedCluster,
	mcao *addonapiv1alpha1.ManagedClusterAddOn,
) (addonfactory.Values, error) {
	values := addonfactory.Values{}

	if !mcao.DeletionTimestamp.IsZero() {
		values["uninstallationAnnotation"] = "true"
	}

	return values, nil
}
