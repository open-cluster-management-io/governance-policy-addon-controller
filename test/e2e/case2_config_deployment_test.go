// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	case2ManagedClusterAddOnCR string = "../resources/config_policy_addon_cr.yaml"
	case2DeploymentName        string = "config-policy-controller"
	case2PodSelector           string = "app=config-policy-controller"
)

var _ = Describe("Test config-policy-controller deployment", func() {
	It("should create the default config-policy-controller deployment on the managed cluster", func() {
		for i, cluster := range managedClusterList {
			logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "
			By(logPrefix + "deploying the default config-policy-controller managedclusteraddon")
			Kubectl("apply", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR)
			deploy := GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, true, 30,
			)
			Expect(deploy).NotTo(BeNil())

			By(logPrefix + "checking the number of containers in the deployment")
			Eventually(func() int {
				deploy = GetWithTimeout(
					cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, true, 30,
				)
				containers, _, _ := unstructured.NestedSlice(deploy.Object, "spec", "template", "spec", "containers")

				return len(containers)
			}, 60, 1).Should(Equal(1))

			if startupProbeInCluster(i) {
				By(logPrefix + "verifying all replicas in config-policy-controller deployment are available")
				Eventually(func() bool {
					deploy = GetWithTimeout(
						cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, true, 30,
					)

					replicas, found, err := unstructured.NestedInt64(deploy.Object, "status", "replicas")
					if !found || err != nil {
						return false
					}

					available, found, err := unstructured.NestedInt64(deploy.Object, "status", "availableReplicas")
					if !found || err != nil {
						return false
					}

					return available == replicas
				}, 240, 1).Should(Equal(true))
			}

			By(logPrefix + "verifying a running config-policy-controller pod")
			Eventually(func() bool {
				opts := metav1.ListOptions{
					LabelSelector: case2PodSelector,
				}
				pods := ListWithTimeoutByNamespace(cluster.clusterClient, gvrPod, opts, addonNamespace, 1, true, 30)

				phase, _, _ := unstructured.NestedString(pods.Items[0].Object, "status", "phase")

				return phase == "Running"
			}, 60, 1).Should(Equal(true))

			By(logPrefix + "showing the config-policy-controller managedclusteraddon as available")
			Eventually(func() bool {
				addon := GetWithTimeout(
					clientDynamic, gvrManagedClusterAddOn, case2DeploymentName, cluster.clusterName, true, 30,
				)

				return getAddonStatus(addon)
			}, 240, 1).Should(Equal(true))

			By(logPrefix +
				"removing the config-policy-controller deployment when the ManagedClusterAddOn CR is removed")
			Kubectl("delete", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR)
			deploy = GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, false, 30,
			)
			Expect(deploy).To(BeNil())
		}
	})

	It("should create a config-policy-controller deployment with custom logging levels and concurrency", func() {
		for _, cluster := range managedClusterList {
			logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "
			By(logPrefix + "deploying the default config-policy-controller managedclusteraddon")
			Kubectl("apply", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR)
			deploy := GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, true, 30,
			)
			Expect(deploy).NotTo(BeNil())

			By(logPrefix + "showing the config-policy-controller managedclusteraddon as available")
			Eventually(func() bool {
				addon := GetWithTimeout(
					clientDynamic, gvrManagedClusterAddOn, case2DeploymentName, cluster.clusterName, true, 30,
				)

				return getAddonStatus(addon)
			}, 240, 1).Should(Equal(true))

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

			By(logPrefix + "verifying the pod has been deployed with a new logging level and concurrency")
			Eventually(func(g Gomega) {
				opts := metav1.ListOptions{
					LabelSelector: case2PodSelector,
				}
				pods := ListWithTimeoutByNamespace(cluster.clusterClient, gvrPod, opts, addonNamespace, 1, true, 60)
				phase := pods.Items[0].Object["status"].(map[string]interface{})["phase"]

				g.Expect(phase.(string)).To(Equal("Running"))
				containerList, _, err := unstructured.NestedSlice(pods.Items[0].Object, "spec", "containers")
				g.Expect(err).To(BeNil())
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
					}
				}
			}, 180, 10).Should(Succeed())

			By(logPrefix +
				"removing the config-policy-controller deployment when the ManagedClusterAddOn CR is removed")
			Kubectl("delete", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR)
			deploy = GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, false, 30,
			)
			Expect(deploy).To(BeNil())
		}
	})
})
