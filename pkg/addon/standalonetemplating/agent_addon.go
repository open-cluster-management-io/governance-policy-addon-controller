package standalonetemplating

import (
	"context"
	"embed"
	"fmt"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"k8s.io/apimachinery/pkg/runtime"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	policyaddon "open-cluster-management.io/governance-policy-addon-controller/pkg/addon"
)

const (
	addonName       = "governance-standalone-hub-templating"
	cfgpolAddonName = "config-policy-controller"
)

// FS go:embed
//
//go:embed manifests
//go:embed manifests/managedclusterchart
//go:embed manifests/managedclusterchart/templates/_helpers.tpl
var FS embed.FS

var agentPermissionFiles = []string{
	"manifests/hubpermissions/role.yaml",
	"manifests/hubpermissions/rolebinding.yaml",
}

func getValues(_ *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
) (addonfactory.Values, error) {
	values := addonfactory.Values{}

	// Inform users of the cluster-specific group they can bind more permissions to
	values["hubGroup"] = agent.DefaultGroups(addon.Namespace, addon.Name)[0]

	return values, nil
}

func getAgentAddon(ctx context.Context, controllerContext *controllercmd.ControllerContext) (agent.AgentAddon, error) {
	registrationOption := policyaddon.NewRegistrationOption(
		controllerContext,
		addonName,
		agentPermissionFiles,
		FS,
		true)

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
			getValues).
		WithManagedClusterClient(clusterClient).
		WithAgentRegistrationOption(registrationOption).
		WithAgentInstallNamespace(
			policyaddon.
				CommonAgentInstallNamespaceFromDeploymentConfigFunc(utils.NewAddOnDeploymentConfigGetter(addonClient)),
		).
		WithAgentHostedModeEnabledOption().
		BuildHelmAgentAddon()
}

type StandaloneAgentAddon struct {
	agent.AgentAddon
	manager addonmanager.AddonManager
}

func (sa *StandaloneAgentAddon) Manifests(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
) ([]runtime.Object, error) {
	// config-policy addon needs to update itself whenever this addon is created/updated/deleted
	sa.manager.Trigger(cluster.Name, cfgpolAddonName)

	return sa.AgentAddon.Manifests(cluster, addon)
}

func GetAndAddAgent(
	ctx context.Context, mgr addonmanager.AddonManager, controllerContext *controllercmd.ControllerContext,
) error {
	agentAddon, err := getAgentAddon(ctx, controllerContext)
	if err != nil {
		return fmt.Errorf("failed getting the %v agent addon: %w", addonName, err)
	}

	standaloneAgentAddon := &StandaloneAgentAddon{
		AgentAddon: agentAddon,
		manager:    mgr,
	}

	err = mgr.AddAgent(standaloneAgentAddon)
	if err != nil {
		return fmt.Errorf("failed adding the %v agent addon to the manager: %w", addonName, err)
	}

	return nil
}
