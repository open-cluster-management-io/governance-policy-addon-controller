package addon

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/blang/semver/v4"
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
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterlister "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
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
	ProxyConfig     map[string]string `json:"proxyConfig,"`
}

type UserArgs struct {
	LogEncoder  string `json:"logEncoder,omitempty"`
	LogLevel    int8   `json:"logLevel,omitempty"`
	PkgLogLevel int8   `json:"pkgLogLevel,omitempty"`
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
	ctx context.Context,
	mgr addonmanager.AddonManager,
	addonName string,
	controllerContext *controllercmd.ControllerContext,
	getAgent func(*controllercmd.ControllerContext) (agent.AgentAddon, error),
) error {
	agentAddon, err := getAgent(controllerContext)
	if err != nil {
		return fmt.Errorf("failed getting the %v agent addon: %w", addonName, err)
	}

	clusterClient, err := clusterv1client.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	clusterInformers := clusterv1informers.NewSharedInformerFactory(clusterClient, 10*time.Minute)
	go clusterInformers.Cluster().V1().ManagedClusters().Informer().Run(ctx.Done())

	agentAddon = &PolicyAgentAddon{agentAddon, clusterInformers.Cluster().V1().ManagedClusters().Lister(), nil}

	err = mgr.AddAgent(agentAddon)
	if err != nil {
		return fmt.Errorf("failed adding the %v agent addon to the manager: %w", addonName, err)
	}

	return nil
}

// PolicyAgentAddon wraps the AgentAddon created from the addonfactory to override some behavior
type PolicyAgentAddon struct {
	agent.AgentAddon
	clusterLister  clusterlister.ManagedClusterLister
	hostingCluster *clusterv1.ManagedCluster
}

func (pa *PolicyAgentAddon) Manifests(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
) ([]runtime.Object, error) {
	// Return error when pause annotation is set to short-circuit automatic addon updates
	pauseAnnotation := addon.GetAnnotations()[PolicyAddonPauseAnnotation]
	if pauseAnnotation == "true" {
		return nil, errors.New("the Policy Addon controller is paused due to the policy-addon-pause annotation")
	}

	// Fetch ManagedCluster for hosting cluster in hosted mode
	hostingClusterName := addon.Annotations[addonapiv1alpha1.HostingClusterNameAnnotationKey]
	if pa.GetAgentAddonOptions().HostedModeEnabled && hostingClusterName != "" {
		hostingCluster, err := pa.clusterLister.Get(hostingClusterName)
		if err != nil {
			return nil, err
		}

		pa.hostingCluster = hostingCluster
	}

	return pa.AgentAddon.Manifests(cluster, addon)
}

// getLogLevel verifies the user-provided log level against Zap, returning 0 if the check fails.
func GetLogLevel(component string, level string) int8 {
	logDefault := int8(0)

	if level == "error" {
		return int8(-1)
	}

	logLevel, err := strconv.ParseInt(level, 10, 8)
	if err != nil || logLevel < -1 {
		log.Error(err, fmt.Sprintf(
			"Failed to verify '%s' annotation value '%s' for component %s (falling back to default value %d)",
			PolicyLogLevelAnnotation, level, component, logDefault),
		)

		return logDefault
	}

	// This is safe because we specified the int8 in ParseInt
	return int8(logLevel)
}

// IsOldKubernetes returns a boolean for whether a cluster is running an older Kubernetes that
// doesn't support current leader election methods.
func IsOldKubernetes(cluster *clusterv1.ManagedCluster) bool {
	for _, cc := range cluster.Status.ClusterClaims {
		if cc.Name == "kubeversion.open-cluster-management.io" {
			k8sVersion, err := semver.ParseTolerant(cc.Value)
			if err != nil {
				continue
			}

			if k8sVersion.Major <= 1 && k8sVersion.Minor < 14 {
				return true
			}

			return false
		}
	}

	return false
}
