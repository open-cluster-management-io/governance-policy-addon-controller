package policyframework

import (
	"embed"
	"strings"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	policyaddon "github.com/stolostron/governance-policy-addon-controller/pkg/addon"
)

const (
	addonName = "governance-policy-framework"
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

type userValues struct {
	OnMulticlusterHub bool `json:"onMulticlusterHub"`
}

func getValues(cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (addonfactory.Values, error) {
	userValues := userValues{
		OnMulticlusterHub: false,
	}
	// special case for local-cluster
	if cluster.Name == "local-cluster" {
		userValues.OnMulticlusterHub = true
	}
	if val, ok := addon.GetAnnotations()["addon.open-cluster-management.io/on-multicluster-hub"]; ok {
		if strings.EqualFold(val, "true") {
			userValues.OnMulticlusterHub = true
		} else if strings.EqualFold(val, "false") {
			// the special case can still be overridden by this annotation
			userValues.OnMulticlusterHub = false
		} else {
			// TODO: should this log or return an error? The annotation should be true or false
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
		BuildHelmAgentAddon()
}

func GetAndAddAgent(mgr addonmanager.AddonManager, controllerContext *controllercmd.ControllerContext) error {
	return policyaddon.GetAndAddAgent(mgr, addonName, controllerContext, GetAgentAddon)
}
