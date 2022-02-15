package iampolicy

import (
	"embed"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	policyaddon "github.com/stolostron/governance-policy-addon-controller/pkg/addon"
)

const (
	addonName = "iam-policy-controller"
)

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

type userValues struct{}

func getValues(cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (addonfactory.Values, error) {
	userValues := userValues{}
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
		BuildHelmAgentAddon()
}

func GetAndAddAgent(mgr addonmanager.AddonManager, controllerContext *controllercmd.ControllerContext) error {
	return policyaddon.GetAndAddAgent(mgr, addonName, controllerContext, GetAgentAddon)
}
