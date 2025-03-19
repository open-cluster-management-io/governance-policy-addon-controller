// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

const (
	case2ManagedClusterAddOnName         string = "config-policy-controller"
	case2ManagedClusterAddOnCR           string = "../resources/config_policy_addon_cr.yaml"
	case2ClusterManagementAddOnCRDefault string = "../resources/config_policy_clustermanagementaddon.yaml"
	case2ClusterManagementAddOnCR        string = "../resources/config_policy_clustermanagementaddon_config.yaml"
	case2CMAAddonWithInstallNs           string = "../resources/config_policy_cma_config_agentInstallNs.yaml"
	case2DeploymentName                  string = "config-policy-controller"
	case2PodSelector                     string = "app=config-policy-controller"
	case2OpenShiftClusterClaim           string = "../resources/openshift_cluster_claim.yaml"
	policyCrdName                        string = "policies.policy.open-cluster-management.io"
	deletionOrphanAnnotationKey          string = "addon.open-cluster-management.io/deletion-orphan"
)

func verifyConfigPolicyDeployment(
	ctx context.Context, logPrefix string, client dynamic.Interface, clusterName, namespace string, clusterNum int,
) {
	By(logPrefix + "checking the number of containers in the deployment")

	deploy := GetWithTimeout(
		ctx, client, gvrDeployment, case2DeploymentName, namespace, true, 60,
	)
	Expect(deploy).NotTo(BeNil())

	Eventually(func() []interface{} {
		deploy = GetWithTimeout(
			ctx, client, gvrDeployment, case2DeploymentName, namespace, true, 30,
		)
		containers, _, _ := unstructured.NestedSlice(deploy.Object, "spec", "template", "spec", "containers")

		return containers
	}, 60, 1).Should(HaveLen(1))

	if startupProbeInCluster(clusterNum) {
		By(logPrefix + "verifying all replicas in config-policy-controller deployment are available")
		Eventually(func(g Gomega) {
			deploy = GetWithTimeout(
				ctx, client, gvrDeployment, case2DeploymentName, namespace, true, 30,
			)

			replicas, found, err := unstructured.NestedInt64(deploy.Object, "status", "replicas")
			g.Expect(found).To(BeTrue(), "status.replicas should exist in the deployment")
			g.Expect(err).ToNot(HaveOccurred())

			available, found, err := unstructured.NestedInt64(deploy.Object, "status", "availableReplicas")
			g.Expect(found).To(BeTrue(), "status.availableReplicas should exist in the deployment")
			g.Expect(err).ToNot(HaveOccurred())

			g.Expect(available).To(Equal(replicas), "available replicas should equal expected replicas")
		}, 240, 1).Should(Succeed())
	}

	By(logPrefix + "verifying a running config-policy-controller pod")
	Eventually(func() string {
		opts := metav1.ListOptions{
			LabelSelector: case2PodSelector,
		}
		pods := ListWithTimeoutByNamespace(ctx, client, gvrPod, opts, namespace, 1, true, 30)

		phase, _, _ := unstructured.NestedString(pods.Items[0].Object, "status", "phase")

		return phase
	}, 60, 1).Should(Equal("Running"))

	By(logPrefix + "showing the config-policy-controller managedclusteraddon as available")
	Eventually(func() bool {
		addon := GetWithTimeout(
			ctx, clientDynamic, gvrManagedClusterAddOn, case2DeploymentName, clusterName, true, 30,
		)

		return getAddonStatus(addon)
	}, 240, 1).Should(BeTrue())
}

var _ = Describe("Test config-policy-controller deployment", Ordered, func() {
	BeforeAll(func() {
		By("Deploying the default config-policy-controller ClusterManagementAddon to the hub cluster")
		Kubectl("apply", "-f", case2ClusterManagementAddOnCRDefault)
	})

	AfterAll(func() {
		if CurrentSpecReport().Failed() {
			debugCollection(case2PodSelector)
		}

		By("Deleting the default config-policy-controller ClusterManagementAddon from the hub cluster")
		Kubectl("delete", "-f", case2ClusterManagementAddOnCRDefault)
	})
	It("should create the config-policy-controller deployment in hosted mode in user's custom namespace",
		func(ctx SpecContext) {
			By("Creating the AddOnDeploymentConfig")
			Kubectl("apply", "-f", addOnDeploymentConfigWithAgentInstallNs)
			DeferCleanup(func() {
				By("Delete the AddOnDeploymentConfig")
				Kubectl("delete", "-f", addOnDeploymentConfigWithAgentInstallNs)
			})

			By("Applying the config-policy-controller ClusterManagementAddOn to use the AddOnDeploymentConfig")
			Kubectl("apply", "-f", case2CMAAddonWithInstallNs)
			DeferCleanup(func() {
				By("Apply Default ClusterManagementAdd")
				Kubectl("apply", "-f", case2ClusterManagementAddOnCRDefault)
			})

			for i, cluster := range managedClusterList[1:] {
				logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "
				By(logPrefix + "deploying the default config-policy-controller managedclusteraddon")
				Kubectl("apply", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR)
				By("Addon should be installed in " + agentInstallNs)
				// Use i+1 since the for loop ranges over a slice skipping first index
				verifyConfigPolicyDeployment(
					ctx, logPrefix, cluster.clusterClient, cluster.clusterName, agentInstallNs, i+1)

				By(logPrefix +
					"removing the config-policy-controller deployment when the ManagedClusterAddOn CR is removed")
				Kubectl("delete", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR, "--timeout=90s")
				deploy := GetWithTimeout(
					ctx, cluster.clusterClient, gvrDeployment, case2DeploymentName, agentInstallNs, false, 180,
				)
				Expect(deploy).To(BeNil())

				opts := metav1.ListOptions{
					LabelSelector: case2PodSelector,
				}
				pods := ListWithTimeoutByNamespace(
					ctx, cluster.clusterClient, gvrPod, opts, agentInstallNs, 0, false, 180)
				Expect(pods).To(BeNil())
			}
		})

	It("should create the default config-policy-controller deployment on the managed cluster", func(ctx SpecContext) {
		for i, cluster := range managedClusterList {
			logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "
			By(logPrefix + "deploying the default config-policy-controller managedclusteraddon")
			Kubectl("apply", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR)

			verifyConfigPolicyDeployment(ctx, logPrefix, cluster.clusterClient, cluster.clusterName, addonNamespace, i)

			By(logPrefix +
				"removing the config-policy-controller deployment when the ManagedClusterAddOn CR is removed")
			Kubectl("delete", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR, "--timeout=90s")
			deploy := GetWithTimeout(
				ctx, cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, false, 180,
			)
			Expect(deploy).To(BeNil())

			opts := metav1.ListOptions{
				LabelSelector: case2PodSelector,
			}
			pods := ListWithTimeoutByNamespace(ctx, cluster.clusterClient, gvrPod, opts, addonNamespace, 0, false, 180)
			Expect(pods).To(BeNil())
		}
	})

	It("should create a config-policy-controller deployment with node selector on the managed cluster",
		func(ctx SpecContext) {
			By("Creating the AddOnDeploymentConfig")
			Kubectl("apply", "-f", addOnDeploymentConfigCR)
			By("Applying the config-policy-controller ClusterManagementAddOn to use the AddOnDeploymentConfig")
			Kubectl("apply", "-f", case2ClusterManagementAddOnCR)

			for i, cluster := range managedClusterList {
				logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "
				By(logPrefix + "deploying the default config-policy-controller managedclusteraddon")
				Kubectl("apply", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR)

				verifyConfigPolicyDeployment(
					ctx, logPrefix, cluster.clusterClient, cluster.clusterName, addonNamespace, i)

				By(logPrefix + "verifying the nodeSelector")
				deploy := GetWithTimeout(
					ctx, cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, true, 30,
				)

				nodeSelector, _, _ := unstructured.NestedStringMap(
					deploy.Object, "spec", "template", "spec", "nodeSelector",
				)
				Expect(nodeSelector).To(Equal(map[string]string{"kubernetes.io/os": "linux"}))

				By(logPrefix + "verifying the tolerations")
				tolerations, _, _ := unstructured.NestedSlice(deploy.Object, "spec", "template", "spec", "tolerations")
				Expect(tolerations).To(HaveLen(1))
				expected := map[string]interface{}{
					"key":      "dedicated",
					"operator": "Equal",
					"value":    "something-else",
					"effect":   "NoSchedule",
				}
				Expect(tolerations[0]).To(Equal(expected))

				By(logPrefix +
					"removing the config-policy-controller deployment when the ManagedClusterAddOn CR is removed")
				Kubectl("delete", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR, "--timeout=90s")
				deploy = GetWithTimeout(
					ctx, cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, false, 180,
				)
				Expect(deploy).To(BeNil())
			}

			By("Deleting the AddOnDeploymentConfig")
			Kubectl("delete", "-f", addOnDeploymentConfigCR, "--timeout=15s")
			By("Restoring the default config-policy-controller ClusterManagementAddOn")
			Kubectl("apply", "-f", case2ClusterManagementAddOnCRDefault)
		})

	It("should create a config-policy-controller deployment with resource requirements on the managed cluster",
		func(ctx SpecContext) {
			deploymentConfigTests := map[string]map[string]interface{}{
				"../resources/addondeploymentconfig_empty.yaml": {
					"requests": map[string]interface{}{"memory": "128Mi"},
					"limits":   map[string]interface{}{"memory": "512Mi"},
				},
				"../resources/addondeploymentconfig_resourceRequirements_individual.yaml": {
					"requests": map[string]interface{}{"memory": "75Mi"},
					"limits":   map[string]interface{}{"memory": "150Mi"},
				},
				"../resources/addondeploymentconfig_resourceRequirements_reduced.yaml": {
					"requests": map[string]interface{}{"memory": "32Mi"},
					"limits":   map[string]interface{}{"memory": "128Mi"},
				},
				"../resources/addondeploymentconfig_resourceRequirements_universal.yaml": {
					"requests": map[string]interface{}{"memory": "512Mi"},
					"limits":   map[string]interface{}{"memory": "1Gi"},
				},
			}

			for configFile, expected := range deploymentConfigTests {
				By("Creating the AddOnDeploymentConfig")
				Kubectl("apply", "-f", configFile)
				By("Applying the config-policy-controller ClusterManagementAddOn to use the AddOnDeploymentConfig")
				Kubectl("apply", "-f", case2ClusterManagementAddOnCR)

				for i, cluster := range managedClusterList {
					logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "
					By(logPrefix + "deploying the default config-policy-controller managedclusteraddon")
					Kubectl("apply", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR)

					verifyConfigPolicyDeployment(
						ctx, logPrefix, cluster.clusterClient, cluster.clusterName, addonNamespace, i)

					By(logPrefix + "verifying the resources")
					Eventually(func(g Gomega) {
						deploy := GetWithTimeout(
							ctx, cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, true, 30,
						)
						g.Expect(deploy).NotTo(BeNil())
						containerSlice, _, err := unstructured.NestedSlice(
							deploy.Object, "spec", "template", "spec", "containers")
						g.Expect(err).NotTo(HaveOccurred())
						g.Expect(containerSlice).To(HaveLen(1))
						container, ok := containerSlice[0].(map[string]interface{})
						g.Expect(ok).To(BeTrue(), "Deployment container should be a map[string]interface{}")
						resources, _, _ := unstructured.NestedMap(container, "resources")
						g.Expect(resources).To(Equal(expected))
					}, 30, 1).Should(Succeed())
				}
			}

			for _, cluster := range managedClusterList {
				logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "
				By(logPrefix + "removing the config-policy-controller " +
					"deployment when the ManagedClusterAddOn CR is removed")
				Kubectl("delete", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR, "--timeout=90s")
				deploy := GetWithTimeout(
					ctx, cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, false, 30,
				)
				Expect(deploy).To(BeNil())
			}

			By("Deleting the AddOnDeploymentConfig")
			Kubectl("delete", "-f", addOnDeploymentConfigCR, "--timeout=15s")
			By("Restoring the config-policy-controller ClusterManagementAddOn")
			Kubectl("apply", "-f", case2ClusterManagementAddOnCRDefault)
		})

	It("should create the default config-policy-controller deployment in hosted mode",
		Label("hosted-mode"),
		func(ctx SpecContext) {
			for i, cluster := range managedClusterList[1:] {
				Expect(cluster.clusterType).To(Equal("managed"))

				hubClusterConfig := managedClusterList[0]
				hubClient := hubClusterConfig.clusterClient

				cluster = managedClusterConfig{
					clusterClient: cluster.clusterClient,
					clusterName:   cluster.clusterName,
					clusterType:   cluster.clusterType,
					hostedOnHub:   true,
					kubeconfig:    cluster.kubeconfig,
				}

				logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "

				By(logPrefix + "setting the vendor label to OpenShift")
				Kubectl(
					"label",
					"managedcluster",
					cluster.clusterName,
					"vendor=OpenShift",
					"--overwrite",
					fmt.Sprintf("--kubeconfig=%s1_e2e", kubeconfigFilename),
				)

				DeferCleanup(func() {
					By(logPrefix + " removing the vendor label")
					Kubectl(
						"label",
						"managedcluster",
						cluster.clusterName,
						"vendor-",
						fmt.Sprintf("--kubeconfig=%s1_e2e", kubeconfigFilename),
					)
				})

				installNamespace := cluster.clusterName + "-hosted"

				setupClusterSecretForHostedMode(
					ctx, logPrefix, hubClient, "config-policy-controller-managed-kubeconfig",
					string(cluster.kubeconfig), installNamespace)

				installAddonInHostedMode(
					ctx, logPrefix, hubClient, case2ManagedClusterAddOnName,
					cluster.clusterName, hubClusterConfig.clusterName, installNamespace, nil)

				// Use i+1 since the for loop ranges over a slice skipping first index
				verifyConfigPolicyDeployment(ctx, logPrefix, hubClient, cluster.clusterName, installNamespace, i+1)

				deleteCtx, deleteCancel := context.WithTimeout(ctx, 15*time.Second)
				defer deleteCancel()

				By(logPrefix +
					"removing the config-policy-controller deployment when the ManagedClusterAddOn CR is removed")
				err := clientDynamic.Resource(gvrManagedClusterAddOn).Namespace(cluster.clusterName).Delete(
					deleteCtx, case2ManagedClusterAddOnName, metav1.DeleteOptions{},
				)
				Expect(err).ToNot(HaveOccurred())

				deploy := GetWithTimeout(
					ctx, hubClient, gvrDeployment, case2DeploymentName, installNamespace, false, 180,
				)
				Expect(deploy).To(BeNil())

				namespace := GetWithTimeout(ctx, hubClient, gvrNamespace, installNamespace, "", false, 120)
				Expect(namespace).To(BeNil())
			}
		})

	It("should create the default config-policy-controller deployment in hosted mode in klusterlet agent namespace",
		Label("hosted-mode"), func(ctx SpecContext) {
			By("Creating the AddOnDeploymentConfig")
			Kubectl("apply", "-f", addOnDeploymentConfigWithCustomVarsCR)
			By("Applying the config-policy-controller ClusterManagementAddOn to use the AddOnDeploymentConfig")
			Kubectl("apply", "-f", case2ClusterManagementAddOnCR)

			for i, cluster := range managedClusterList[1:] {
				Expect(cluster.clusterType).To(Equal("managed"))

				cluster = managedClusterConfig{
					clusterClient: cluster.clusterClient,
					clusterName:   cluster.clusterName,
					clusterType:   cluster.clusterType,
					hostedOnHub:   true,
					kubeconfig:    cluster.kubeconfig,
				}
				hubClusterConfig := managedClusterList[0]
				hubClient := hubClusterConfig.clusterClient
				installNamespace := "klusterlet-" + cluster.clusterName
				logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "

				setupClusterSecretForHostedMode(
					ctx, logPrefix, hubClient, "external-managed-kubeconfig",
					string(cluster.kubeconfig), installNamespace)

				installAddonInHostedMode(
					ctx, logPrefix, hubClient, case2ManagedClusterAddOnName,
					cluster.clusterName, hubClusterConfig.clusterName, installNamespace, nil)

				// Use i+1 since the for loop ranges over a slice skipping first index
				verifyConfigPolicyDeployment(ctx, logPrefix, hubClient, cluster.clusterName, installNamespace, i+1)

				deleteCtx, deleteCancel := context.WithTimeout(ctx, 15*time.Second)
				defer deleteCancel()

				By(logPrefix + "Removing the ManagedClusterAddOn CR")
				err := clientDynamic.Resource(gvrManagedClusterAddOn).Namespace(cluster.clusterName).Delete(
					deleteCtx, case2ManagedClusterAddOnName, metav1.DeleteOptions{},
				)
				Expect(err).ToNot(HaveOccurred())

				By(logPrefix +
					"Verifying controller deployment is removed when the ManagedClusterAddOn CR is removed")

				deploy := GetWithTimeout(
					ctx, hubClient, gvrDeployment, case2DeploymentName, installNamespace, false, 180,
				)
				Expect(deploy).To(BeNil())

				By(logPrefix + "Verifying install namespace is not removed when the ManagedClusterAddOn CR is removed")
				namespace := GetWithTimeout(ctx, hubClient, gvrNamespace, installNamespace, "", true, 30)
				Expect(namespace).NotTo(BeNil())

				ctxSec, cancelSec := context.WithTimeout(ctx, 15*time.Second)
				defer cancelSec()

				By(logPrefix + "cleaning up  the hosting cluster secret")
				err = hubClient.Resource(gvrSecret).Namespace(installNamespace).Delete(
					ctxSec, "external-managed-kubeconfig", metav1.DeleteOptions{},
				)
				Expect(err).ToNot(HaveOccurred())

				ctxNS, cancelNS := context.WithTimeout(ctx, 15*time.Second)
				defer cancelNS()

				By(logPrefix + "Cleaning up the install namespace")
				err = hubClient.Resource(gvrNamespace).Delete(
					ctxNS, installNamespace, metav1.DeleteOptions{},
				)
				Expect(err).ToNot(HaveOccurred())

				namespace = GetWithTimeout(ctx, hubClient, gvrNamespace, installNamespace, "", false, 120)
				Expect(namespace).To(BeNil())
			}
			By("Deleting the AddOnDeploymentConfig")
			Kubectl("delete", "-f", addOnDeploymentConfigWithCustomVarsCR, "--timeout=15s")
			By("Restoring the default config-policy-controller ClusterManagementAddOn")
			Kubectl("apply", "-f", case2ClusterManagementAddOnCRDefault)
		})

	It("should create a config-policy-controller deployment with customizations", func(ctx SpecContext) {
		for _, cluster := range managedClusterList {
			logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "
			By(logPrefix + "deploying the default config-policy-controller managedclusteraddon")
			Kubectl("apply", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR)
			deploy := GetWithTimeout(
				ctx, cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, true, 30,
			)
			Expect(deploy).NotTo(BeNil())

			By(logPrefix + "verifying the pod has been deployed with some default options")
			Eventually(func(g Gomega) {
				opts := metav1.ListOptions{
					LabelSelector: case2PodSelector,
				}
				pods := ListWithTimeoutByNamespace(
					ctx, cluster.clusterClient, gvrPod, opts, addonNamespace, 1, true, 120)
				phase := pods.Items[0].Object["status"].(map[string]interface{})["phase"]

				g.Expect(phase.(string)).To(Equal("Running"))
				containerList, _, err := unstructured.NestedSlice(pods.Items[0].Object, "spec", "containers")
				g.Expect(err).ToNot(HaveOccurred())
				for _, container := range containerList {
					containerObj, ok := container.(map[string]interface{})
					g.Expect(ok).To(BeTrue())

					if g.Expect(containerObj).To(HaveKey("name")) && containerObj["name"] != case2DeploymentName {
						continue
					}
					if g.Expect(containerObj).To(HaveKey("args")) {
						args := containerObj["args"]
						g.Expect(args).To(ContainElement("--log-encoder=console"))
						g.Expect(args).To(ContainElement("--evaluation-concurrency=2"))
						g.Expect(args).To(ContainElement("--client-max-qps=30"))
						g.Expect(args).NotTo(ContainElement("--enable-operator-policy=true"))
					}
				}
			}, 180, 10).Should(Succeed())

			By(logPrefix + "showing the config-policy-controller managedclusteraddon as available")
			Eventually(func() bool {
				addon := GetWithTimeout(
					ctx, clientDynamic, gvrManagedClusterAddOn, case2DeploymentName, cluster.clusterName, true, 30,
				)

				return getAddonStatus(addon)
			}, 240, 1).Should(BeTrue())

			By(logPrefix + "annotating the managedclusteraddon with the " + loggingLevelAnnotation + " annotation")
			Kubectl("annotate", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR, loggingLevelAnnotation)

			By(
				logPrefix + "annotating the managedclusteraddon with the " + evaluationConcurrencyAnnotation +
					" annotation",
			)
			Kubectl(
				"annotate",
				"-n",
				cluster.clusterName,
				"-f",
				case2ManagedClusterAddOnCR,
				evaluationConcurrencyAnnotation,
			)

			By(logPrefix + "annotating the managedclusteraddon with the " + prometheusEnabledAnnotation + " annotation")
			Kubectl(
				"annotate",
				"-n",
				cluster.clusterName,
				"-f",
				case2ManagedClusterAddOnCR,
				prometheusEnabledAnnotation,
			)

			By(logPrefix + "annotating the managedclusteraddon with the " + clientQPSAnnotation + " annotation")
			Kubectl("annotate", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR, clientQPSAnnotation)

			By(logPrefix + "annotating the managedclusteraddon with the " + opPolicyEnabledAnnotation + " annotation")
			Kubectl(
				"annotate",
				"-n",
				cluster.clusterName,
				"-f",
				case2ManagedClusterAddOnCR,
				opPolicyEnabledAnnotation,
			)

			By(logPrefix + "verifying the pod has been deployed with a new configuration")
			Eventually(func(g Gomega) {
				opts := metav1.ListOptions{
					LabelSelector: case2PodSelector,
				}
				pods := ListWithTimeoutByNamespace(
					ctx, cluster.clusterClient, gvrPod, opts, addonNamespace, 1, true, 120)
				phase := pods.Items[0].Object["status"].(map[string]interface{})["phase"]

				g.Expect(phase.(string)).To(Equal("Running"))
				containerList, _, err := unstructured.NestedSlice(pods.Items[0].Object, "spec", "containers")
				g.Expect(err).ToNot(HaveOccurred())
				for _, container := range containerList {
					containerObj, ok := container.(map[string]interface{})
					g.Expect(ok).To(BeTrue())

					if g.Expect(containerObj).To(HaveKey("name")) && containerObj["name"] != case2DeploymentName {
						continue
					}
					if g.Expect(containerObj).To(HaveKey("args")) {
						args := containerObj["args"]
						g.Expect(args).To(ContainElement("--log-encoder=console"))
						g.Expect(args).To(ContainElement("--log-level=8"))
						g.Expect(args).To(ContainElement("--v=6"))
						g.Expect(args).To(ContainElement("--evaluation-concurrency=5"))
						g.Expect(args).To(ContainElement("--client-max-qps=50"))
						g.Expect(args).To(ContainElement("--leader-elect=false"))
						g.Expect(args).To(ContainElement("--enable-operator-policy=true"))
						g.Expect(args).ToNot(ContainElement(ContainSubstring("operator-policy-default-namespace")))
					}
				}
			}, 180, 10).Should(Succeed())

			By(logPrefix + "verifying that the metrics ServiceMonitor exists")
			Eventually(func(g Gomega) {
				sm, err := cluster.clusterClient.Resource(gvrServiceMonitor).Namespace(addonNamespace).Get(
					ctx, "ocm-config-policy-controller-metrics", metav1.GetOptions{},
				)
				g.Expect(err).ToNot(HaveOccurred())

				endpoints, _, _ := unstructured.NestedSlice(sm.Object, "spec", "endpoints")
				g.Expect(endpoints).ToNot(BeEmpty())
				g.Expect(endpoints[0].(map[string]interface{})["scheme"].(string)).To(Equal("http"))
			}, 60, 3).Should(Succeed())

			By(logPrefix + "verifying that the metrics Service exists")
			Eventually(func(g Gomega) {
				service, err := cluster.clusterClient.Resource(gvrService).Namespace(addonNamespace).Get(
					ctx, "config-policy-controller-metrics", metav1.GetOptions{},
				)
				g.Expect(err).ToNot(HaveOccurred())

				ports, _, _ := unstructured.NestedSlice(service.Object, "spec", "ports")
				g.Expect(ports).To(HaveLen(1))
				port := ports[0].(map[string]interface{})
				g.Expect(port["port"].(int64)).To(Equal(int64(8080)))
				g.Expect(port["targetPort"].(int64)).To(Equal(int64(8383)))
			}, 60, 3).Should(Succeed())

			By(logPrefix +
				"removing the config-policy-controller deployment when the ManagedClusterAddOn CR is removed")
			Kubectl("delete", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR, "--timeout=90s")
			deploy = GetWithTimeout(
				ctx, cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, false, 180,
			)
			Expect(deploy).To(BeNil())
		}
	})

	It("should create a config-policy-controller deployment with metrics monitoring on OpenShift clusters",
		func(ctx SpecContext) {
			Expect(managedClusterList).ToNot(BeEmpty())
			hubClient := managedClusterList[0].clusterClient

			for i, cluster := range managedClusterList {
				logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "

				By(logPrefix + "setting the product.open-cluster-management.io ClusterClaim to OpenShift")
				Kubectl(
					"apply",
					"-f",
					case2OpenShiftClusterClaim,
					fmt.Sprintf("--kubeconfig=%s%d_e2e", kubeconfigFilename, i+1),
				)

				By(logPrefix + "waiting for the ClusterClaim to be in the ManagedCluster status")
				Eventually(func(g Gomega) {
					managedCluster, err := hubClient.Resource(gvrManagedCluster).Get(
						ctx, cluster.clusterName, metav1.GetOptions{},
					)
					g.Expect(err).ToNot(HaveOccurred())

					clusterClaims, _, _ := unstructured.NestedSlice(managedCluster.Object, "status", "clusterClaims")
					g.Expect(clusterClaims).ToNot(BeEmpty())

					var claimValue string
					for _, clusterClaim := range clusterClaims {
						clusterClaim := clusterClaim.(map[string]interface{})
						if clusterClaim["name"].(string) == "product.open-cluster-management.io" {
							claimValue = clusterClaim["value"].(string)

							break
						}
					}

					g.Expect(claimValue).To(Equal("OpenShift"))
				}, 60, 1).Should(Succeed())

				// The status doesn't need to be checked on the deployment because the deployment requires a cert that
				// is auto-generated by OpenShift, which won't be present.
				By(logPrefix + "deploying the default config-policy-controller managedclusteraddon")
				Kubectl("apply", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR)
				deploy := GetWithTimeout(
					ctx, cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, true, 30,
				)
				Expect(deploy).NotTo(BeNil())

				By(logPrefix + "verifying that the metrics ServiceMonitor exists")
				Eventually(func(g Gomega) {
					sm, err := cluster.clusterClient.Resource(gvrServiceMonitor).Namespace(addonNamespace).Get(
						ctx, "ocm-config-policy-controller-metrics", metav1.GetOptions{},
					)
					g.Expect(err).ToNot(HaveOccurred())

					endpoints, _, _ := unstructured.NestedSlice(sm.Object, "spec", "endpoints")
					g.Expect(endpoints).ToNot(BeEmpty())
					g.Expect(endpoints[0].(map[string]interface{})["scheme"].(string)).To(Equal("https"))
				}, 120, 3).Should(Succeed())

				By(logPrefix + "verifying that the metrics Service exists")
				Eventually(func(g Gomega) {
					service, err := cluster.clusterClient.Resource(gvrService).Namespace(addonNamespace).Get(
						ctx, "config-policy-controller-metrics", metav1.GetOptions{},
					)
					g.Expect(err).ToNot(HaveOccurred())

					ports, _, _ := unstructured.NestedSlice(service.Object, "spec", "ports")
					g.Expect(ports).To(HaveLen(1))
					port := ports[0].(map[string]interface{})
					g.Expect(port["port"].(int64)).To(Equal(int64(8443)))
					g.Expect(port["targetPort"].(int64)).To(Equal(int64(8443)))
				}, 120, 3).Should(Succeed())

				By(logPrefix + "verifying that the addon namespace has the openshift.io/cluster-monitoring label set")
				Eventually(func(g Gomega) {
					ns, err := cluster.clusterClient.Resource(gvrNamespace).Get(
						ctx, addonNamespace, metav1.GetOptions{},
					)
					g.Expect(err).ToNot(HaveOccurred())

					g.Expect(ns.GetLabels()["openshift.io/cluster-monitoring"]).To(Equal("true"))
				}, 30, 3).Should(Succeed())

				By(logPrefix + "cleaning up")
				Kubectl(
					"delete",
					"-f",
					case2OpenShiftClusterClaim,
					fmt.Sprintf("--kubeconfig=%s%d_e2e", kubeconfigFilename, i+1),
					"--timeout=15s",
				)

				Kubectl("delete", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR, "--timeout=90s")
				deploy = GetWithTimeout(
					ctx, cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, false, 30,
				)
				Expect(deploy).To(BeNil())

				By(logPrefix + "waiting for the ClusterClaim to not be in the ManagedCluster status")
				Eventually(func(g Gomega) {
					managedCluster, err := hubClient.Resource(gvrManagedCluster).Get(
						ctx, cluster.clusterName, metav1.GetOptions{},
					)
					g.Expect(err).ToNot(HaveOccurred())

					clusterClaims, _, _ := unstructured.NestedSlice(managedCluster.Object, "status", "clusterClaims")
					g.Expect(clusterClaims).ToNot(ContainElement(
						HaveKeyWithValue("name", "product.open-cluster-management.io"),
					))
				}, 60, 1).Should(Succeed())
			}
		})
})

func setupClusterSecretForHostedMode(
	ctx context.Context, logPrefix string, client dynamic.Interface, secretName, secretData, installNamespace string,
) {
	By(logPrefix + "creating  secret " + secretName + " in namespace " + installNamespace)

	installNamespaceObject := unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]interface{}{
			"name": installNamespace,
		},
	}}

	_, err := client.Resource(gvrNamespace).Create(
		ctx, &installNamespaceObject, metav1.CreateOptions{},
	)
	if !errors.IsAlreadyExists(err) {
		Expect(err).ToNot(HaveOccurred())
	}

	secret := unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]interface{}{
			"name": secretName,
		},
		"stringData": map[string]interface{}{
			"kubeconfig": secretData,
		},
	}}
	_, err = client.Resource(gvrSecret).Namespace(installNamespace).Create(
		ctx, &secret, metav1.CreateOptions{},
	)

	if !errors.IsAlreadyExists(err) {
		Expect(err).ToNot(HaveOccurred())
	}
}
