package addon

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"os"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

var genericScheme = runtime.NewScheme()

func init() {
	scheme.AddToScheme(genericScheme)
}

func NewRegistrationOption(
	controllerContext *controllercmd.ControllerContext,
	addonName string,
	agentPermissionFiles []string,
	filesystem embed.FS,
) *agent.RegistrationOption {
	applyManifestFromFile := func(file, clusterName string, kubeclient *kubernetes.Clientset, recorder events.Recorder) error {
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

	if os.Getenv("SIMULATION_MODE") == "on" {
		agentAddon = &SimulationAgent{a: agentAddon}
	}

	err = mgr.AddAgent(agentAddon)
	if err != nil {
		return fmt.Errorf("failed adding the %v agent addon to the manager: %w", addonName, err)
	}

	return nil
}

type SimulationAgent struct {
	a agent.AgentAddon
}

func (sim *SimulationAgent) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
	realObjs, err := sim.a.Manifests(cluster, addon)

	fmt.Println("Simulation Agent Manifests:")
	for _, obj := range realObjs {
		b, err := json.Marshal(obj)
		if err != nil {
			fmt.Println(err)
		}
		fmt.Println(string(b))
	}

	return []runtime.Object{}, err
}

func (sim *SimulationAgent) GetAgentAddonOptions() agent.AgentAddonOptions {
	opts := sim.a.GetAgentAddonOptions()

	opts.Registration.PermissionConfig = func(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
		fmt.Println("Simulation Agent permission config skipped")
		return nil
	}

	return opts
}
