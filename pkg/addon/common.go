package addon

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var log = ctrl.Log.WithName("common")

const (
	PolicyAddonPauseAnnotation = "policy-addon-pause"
	PolicyLogLevelAnnotation   = "log-level"
)

type GlobalValues struct {
	ImagePullPolicy string            `json:"imagePullPolicy,"`
	ImagePullSecret string            `json:"imagePullSecret"`
	ImageOverrides  map[string]string `json:"imageOverrides,"`
	NodeSelector    map[string]string `json:"nodeSelector,"`
	ProxyConfig     map[string]string `json:"proxyConfig,"`
}

type UserArgs struct {
	LogEncoder  string `json:"logEncoder,"`
	LogLevel    int8   `json:"logLevel,"`
	PkgLogLevel int8   `json:"pkgLogLevel,"`
}

type UserValues struct {
	GlobalValues GlobalValues `json:"global,"`
	UserArgs     UserArgs     `json:"args,"`
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

func NewRegistrationOption(
	controllerContext *controllercmd.ControllerContext,
	addonName string,
	agentPermissionFiles []string,
	filesystem embed.FS,
) *agent.RegistrationOption {
	applyManifestFromFile := func(file, clusterName string,
		kubeclient *kubernetes.Clientset, recorder events.Recorder,
	) error {
		groups := agent.DefaultGroups(clusterName, addonName)
		config := struct {
			ClusterName string
			Group       string
		}{
			ClusterName: clusterName,
			Group:       groups[0],
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
		PermissionConfig: func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
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

func GetAndAddAgent(
	mgr addonmanager.AddonManager,
	addonName string,
	controllerContext *controllercmd.ControllerContext,
	getAgent func(*controllercmd.ControllerContext) (agent.AgentAddon, error),
) error {
	agentAddon, err := getAgent(controllerContext)
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

func (pa *PolicyAgentAddon) Manifests(cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
) ([]runtime.Object, error) {
	pauseAnnotation := addon.GetAnnotations()[PolicyAddonPauseAnnotation]
	if pauseAnnotation == "true" {
		return nil, errors.New("the Policy Addon controller is paused due to the policy-addon-pause annotation")
	}

	return pa.AgentAddon.Manifests(cluster, addon)
}

// getLogLevel verifies the user-provided log level against Zap, returning 0 if the check fails.
func GetLogLevel(component string, level string) int8 {
	logDefault := int8(0)

	logLevel, err := strconv.ParseInt(level, 10, 8)
	if err != nil || logLevel < 0 {
		log.Error(err, fmt.Sprintf(
			"Failed to verify '%s' annotation value '%s' for component %s (falling back to default value %d)",
			PolicyLogLevelAnnotation, level, component, logDefault),
		)

		return logDefault
	}

	// This is safe because we specified the int8 in ParseInt
	return int8(logLevel)
}
