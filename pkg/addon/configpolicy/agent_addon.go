package configpolicy

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	addonlistersv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterlistersv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	policyaddon "open-cluster-management.io/governance-policy-addon-controller/pkg/addon"
)

const (
	addonName                        = "config-policy-controller"
	operatorPolicyDisabledAnnotation = "operator-policy-disabled"
	standaloneTemplatingAddonName    = "governance-standalone-hub-templating"
)

type configPolicyUserValues struct {
	policyaddon.CommonValues `json:",inline"`

	ManagedKubeConfigSecret       string          `json:"managedKubeConfigSecret,omitempty"`
	OperatorPolicy                *operatorPolicy `json:"operatorPolicy,omitempty"`
	StandaloneHubTemplatingSecret string          `json:"standaloneHubTemplatingSecret,omitempty"`
}

type operatorPolicy struct {
	Disabled         bool   `json:"disabled"`
	DefaultNamespace string `json:"defaultNamespace,omitempty"`
}

var (
	// FS go:embed
	//
	//go:embed manifests
	//go:embed manifests/managedclusterchart
	//go:embed manifests/managedclusterchart/templates/_helpers.tpl
	FS embed.FS

	log = ctrl.Log.WithName("configpolicy")

	agentPermissionFiles = []string{
		// role with RBAC rules to access resources on hub
		"manifests/hubpermissions/role.yaml",
		// rolebinding to bind the above role to a certain user group
		"manifests/hubpermissions/rolebinding.yaml",
	}
)

func getSkeletonValues() configPolicyUserValues {
	return configPolicyUserValues{
		CommonValues: policyaddon.CommonValues{
			BaseValues: policyaddon.BaseValues{
				GlobalValues: &policyaddon.GlobalValues{
					ImagePullPolicy: corev1.PullIfNotPresent,
					ImageOverrides: map[string]string{
						"config_policy_controller": os.Getenv("CONFIG_POLICY_CONTROLLER_IMAGE"),
					},
				},
			},
		},
		OperatorPolicy: &operatorPolicy{
			Disabled: false,
		},
	}
}

func (cpv *configPolicyUserValues) setOperatorPolicyDisabled(value string) error {
	valBool, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}

	cpv.OperatorPolicy.Disabled = valBool

	return nil
}

func getValuesFromAnnotations(
	clusterClient clusterlistersv1.ManagedClusterLister,
	addonClient addonlistersv1alpha1.ManagedClusterAddOnLister,
) func(*clusterv1.ManagedCluster, *addonapiv1alpha1.ManagedClusterAddOn) (addonfactory.Values, error) {
	return func(
		cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn,
	) (addonfactory.Values, error) {
		userValues := getSkeletonValues()

		err := userValues.CommonValues.SetCommonValues(cluster, addon, clusterClient)
		if err != nil {
			return nil, err
		}

		// Set the standalone hub templating secret if enabled
		_, err = addonClient.ManagedClusterAddOns(addon.Namespace).Get(standaloneTemplatingAddonName)
		if !k8serrors.IsNotFound(err) {
			if err != nil {
				return nil, err
			}

			userValues.StandaloneHubTemplatingSecret = standaloneTemplatingAddonName + "-hub-kubeconfig"
		}

		// Configure OperatorPolicy based on the cluster's OpenShift version
		if cluster.Labels["openshiftVersion-major"] == "4" {
			userValues.OperatorPolicy.DefaultNamespace = "openshift-operators"
		} else {
			userValues.OperatorPolicy.Disabled = true
		}

		if err := userValues.CommonValues.SetCommonValuesFromAnnotations(addon); err != nil {
			log.Error(err, "failed to set common values from annotations")
		}

		if val, ok := addon.GetAnnotations()[operatorPolicyDisabledAnnotation]; ok {
			err := userValues.setOperatorPolicyDisabled(val)
			if err != nil {
				log.Error(err, fmt.Sprintf(
					policyaddon.AnnotationParseErrorFmt,
					operatorPolicyDisabledAnnotation, val, addonName, false),
				)
			}
		}

		return addonfactory.JsonStructToValues(userValues)
	}
}

func getValuesFromCustomizedVariableValues(config addonapiv1alpha1.AddOnDeploymentConfig) (addonfactory.Values, error) {
	userValues := getSkeletonValues()

	userValuesMap, err := userValues.CommonValues.SetCommonValuesFromCustomizedVariables(config)
	if err != nil {
		log.Error(err, "error setting common addon values from customized variables")
	}

	//nolint:unparam
	variableToFuncMap := map[string]func(string) error{
		"operatorPolicyDisabled": userValues.setOperatorPolicyDisabled,
		"managedKubeConfigSecret": func(value string) error {
			userValues.ManagedKubeConfigSecret = value

			return nil
		},
	}

	for key, value := range userValuesMap {
		if fn, ok := variableToFuncMap[key]; ok {
			err := fn(value)
			if err != nil {
				log.Error(err, "error setting customized variable", "variable", key, "value", value)
			}
		} else {
			log.Error(errors.New("unknown customized variable"),
				"variable is not supported",
				"variable", key,
				"value", value)
		}
	}

	return addonfactory.JsonStructToValues(userValues)
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

	addonInformer := addoninformers.NewSharedInformerFactory(addonClient, 10*time.Minute).
		Addon().V1alpha1().ManagedClusterAddOns()
	go addonInformer.Informer().Run(ctx.Done())

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
			getValuesFromAnnotations(clusterInformer.Lister(), addonInformer.Lister()),
			addonfactory.GetValuesFromAddonAnnotation,
			addonfactory.GetAddOnDeploymentConfigValues(
				utils.NewAddOnDeploymentConfigGetter(addonClient),
				addonfactory.ToAddOnNodePlacementValues,
				addonfactory.ToAddOnResourceRequirementsValues,
				getValuesFromCustomizedVariableValues,
			),
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
