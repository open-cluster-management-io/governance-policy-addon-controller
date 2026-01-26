package policyframework

import (
	"context"
	"embed"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	corev1 "k8s.io/api/core/v1"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterlistersv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	policyaddon "open-cluster-management.io/governance-policy-addon-controller/pkg/addon"
)

const (
	addonName                   = "governance-policy-framework"
	onMulticlusterHubAnnotation = "addon.open-cluster-management.io/on-multicluster-hub"
	// Should only be set when the hub cluster is imported in a global hub
	syncPoliciesOnMulticlusterHubAnnotation = "policy.open-cluster-management.io/sync-policies-on-multicluster-hub"
)

type policyFrameworkUserValues struct {
	policyaddon.CommonValues `json:",inline"`

	SyncPoliciesOnMulticlusterHub bool `json:"syncPoliciesOnMulticlusterHub,omitempty"`
	OnMulticlusterHub             bool `json:"onMulticlusterHub,omitempty"`
}

var (
	// FS go:embed
	//
	//go:embed manifests
	//go:embed manifests/managedclusterchart
	//go:embed manifests/managedclusterchart/templates/_helpers.tpl
	FS embed.FS

	log = ctrl.Log.WithName("policyframework")

	agentPermissionFiles = []string{
		// role with RBAC rules to access resources on hub
		"manifests/hubpermissions/role.yaml",
		// rolebinding to bind the above role to a certain user group
		"manifests/hubpermissions/rolebinding.yaml",
	}
)

func getSkeletonValues() policyFrameworkUserValues {
	return policyFrameworkUserValues{
		CommonValues: policyaddon.CommonValues{
			BaseValues: policyaddon.BaseValues{
				GlobalValues: policyaddon.GlobalValues{
					ImagePullPolicy: corev1.PullIfNotPresent,
					ImageOverrides: map[string]string{
						"governance_policy_framework_addon": os.Getenv("GOVERNANCE_POLICY_FRAMEWORK_ADDON_IMAGE"),
					},
					ProxyConfig: policyaddon.ProxyConfig{},
				},
			},
		},
	}
}

func getValuesFromAnnotations(clusterClient clusterlistersv1.ManagedClusterLister) func(*clusterv1.ManagedCluster,
	*addonapiv1alpha1.ManagedClusterAddOn,
) (addonfactory.Values, error) {
	return func(
		cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn,
	) (addonfactory.Values, error) {
		userValues := getSkeletonValues()
		err := userValues.CommonValues.SetCommonValues(cluster, addon, clusterClient)
		if err != nil {
			return nil, err
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

		// The ManagedClusterAddOn's annotation has higher priority,
		// though it'd be quite unusual to set conflicting values.
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
					userValues.SyncPoliciesOnMulticlusterHub = true
				} else if strings.EqualFold(val, "false") {
					userValues.SyncPoliciesOnMulticlusterHub = false
				}
			}
		}

		if err := userValues.CommonValues.SetCommonValuesFromAnnotations(addon); err != nil {
			log.Error(err, "failed to set common values from annotations")
		}

		return addonfactory.JsonStructToValues(userValues)
	}
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
			getValuesFromAnnotations(clusterInformer.Lister()),
			addonfactory.GetValuesFromAddonAnnotation,
			policyaddon.MandateValues,
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
