// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	case1ManagedClusterAddOnCR   string = "../resources/framework_addon_cr.yaml"
	case1FrameworkDeploymentName string = "governance-policy-framework"
	case1FrameworkPodSelector    string = "app=governance-policy-framework"
	case1MWName                  string = "addon-governance-policy-framework-deploy"
	case1MWPatch                 string = "../resources/manifestwork_add_patch.json"
)

var _ = Describe("Test framework deployment", func() {
	It("should create the default framework deployment", func() {
		for _, cluster := range managedClusterList {
			By(cluster.clusterType + " " + cluster.clusterName +
				": deploying the default framework managedclusteraddon")
			Kubectl("apply", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR)
			deploy := GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case1FrameworkDeploymentName, addonNamespace, true, 30,
			)
			Expect(deploy).NotTo(BeNil())

			By(cluster.clusterType + " " + cluster.clusterName +
				": checking the number of containers in the deployment")
			Eventually(func() int {
				deploy = GetWithTimeout(cluster.clusterClient, gvrDeployment,
					case1FrameworkDeploymentName, addonNamespace, true, 30)
				spec := deploy.Object["spec"].(map[string]interface{})["template"].(map[string]interface{})["spec"]
				containers := spec.(map[string]interface{})["containers"]

				return len(containers.([]interface{}))
			}, 60, 1).Should(Equal(3))

			By(cluster.clusterType + " " + cluster.clusterName +
				": verifying all replicas in framework deployment are available")
			Eventually(func() bool {
				deploy = GetWithTimeout(
					cluster.clusterClient, gvrDeployment, case1FrameworkDeploymentName, addonNamespace, true, 30,
				)
				status := deploy.Object["status"]
				replicas := status.(map[string]interface{})["replicas"]
				availableReplicas := status.(map[string]interface{})["availableReplicas"]

				return (availableReplicas != nil) && replicas.(int64) == availableReplicas.(int64)
			}, 240, 1).Should(Equal(true))

			By(cluster.clusterType + " " + cluster.clusterName + ": verifying a framework pod is running")
			Eventually(func() bool {
				opts := metav1.ListOptions{
					LabelSelector: case1FrameworkPodSelector,
				}
				pods := ListWithTimeoutByNamespace(cluster.clusterClient, gvrPod, opts, addonNamespace, 1, true, 30)
				phase := pods.Items[0].Object["status"].(map[string]interface{})["phase"]

				return phase.(string) == "Running"
			}, 60, 1).Should(Equal(true))

			By(cluster.clusterType + " " + cluster.clusterName +
				": showing the framework managedclusteraddon as available")
			Eventually(func() bool {
				addon := GetWithTimeout(
					clientDynamic, gvrManagedClusterAddOn, case1FrameworkDeploymentName, cluster.clusterName, true, 30,
				)

				return getAddonStatus(addon)
			}, 240, 1).Should(Equal(true))

			By(cluster.clusterType + " " + cluster.clusterName +
				": removing the framework deployment when the ManagedClusterAddOn CR is removed")
			Kubectl("delete", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR)
			deploy = GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case1FrameworkDeploymentName, addonNamespace, false, 30,
			)
			Expect(deploy).To(BeNil())
		}
	})

	It("should deploy with 2 containers if onManagedClusterHub is set in helm values annotation", func() {
		for _, cluster := range managedClusterList {
			By(cluster.clusterType + " " + cluster.clusterName +
				": deploying the default framework managedclusteraddon")
			Kubectl("apply", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR)
			deploy := GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case1FrameworkDeploymentName, addonNamespace, true, 30,
			)
			Expect(deploy).NotTo(BeNil())

			By(cluster.clusterType + " " + cluster.clusterName +
				": annotating the framework managedclusteraddon with helm values")
			Kubectl("annotate", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR,
				"addon.open-cluster-management.io/values={\"onMulticlusterHub\":true}")

			By(cluster.clusterType + " " + cluster.clusterName +
				": checking the number of containers in the deployment")
			Eventually(func() int {
				deploy = GetWithTimeout(cluster.clusterClient, gvrDeployment,
					case1FrameworkDeploymentName, addonNamespace, true, 30)
				spec := deploy.Object["spec"].(map[string]interface{})["template"].(map[string]interface{})["spec"]
				containers := spec.(map[string]interface{})["containers"]

				return len(containers.([]interface{}))
			}, 60, 1).Should(Equal(2))

			By(cluster.clusterType + " " + cluster.clusterName +
				": verifying all replicas in framework deployment are available")
			Eventually(func() bool {
				deploy = GetWithTimeout(
					cluster.clusterClient, gvrDeployment, case1FrameworkDeploymentName, addonNamespace, true, 30,
				)
				status := deploy.Object["status"]
				replicas := status.(map[string]interface{})["replicas"]
				availableReplicas := status.(map[string]interface{})["availableReplicas"]

				return (availableReplicas != nil) && replicas.(int64) == availableReplicas.(int64)
			}, 240, 1).Should(Equal(true))

			By(cluster.clusterType + " " + cluster.clusterName + ": verifying a framework pod is running")
			Eventually(func() bool {
				opts := metav1.ListOptions{
					LabelSelector: case1FrameworkPodSelector,
				}
				pods := ListWithTimeoutByNamespace(cluster.clusterClient, gvrPod, opts, addonNamespace, 1, true, 30)
				phase := pods.Items[0].Object["status"].(map[string]interface{})["phase"]

				return phase.(string) == "Running"
			}, 60, 1).Should(Equal(true))

			By(cluster.clusterType + " " + cluster.clusterName +
				": showing the framework managedclusteraddon as available")
			Eventually(func() bool {
				addon := GetWithTimeout(
					clientDynamic, gvrManagedClusterAddOn, case1FrameworkDeploymentName, cluster.clusterName, true, 30,
				)

				return getAddonStatus(addon)
			}, 240, 1).Should(Equal(true))

			By(cluster.clusterType + " " + cluster.clusterName + ": deleting the managedclusteraddon")
			Kubectl("delete", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR)
			deploy = GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case1FrameworkDeploymentName, addonNamespace, false, 30,
			)
			Expect(deploy).To(BeNil())
		}
	})

	It("should deploy with 2 containers if onManagedClusterHub is set in the custom annotation", func() {
		for _, cluster := range managedClusterList {
			By(cluster.clusterType + " " + cluster.clusterName +
				": deploying the default framework managedclusteraddon")
			Kubectl("apply", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR)
			deploy := GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case1FrameworkDeploymentName, addonNamespace, true, 30,
			)
			Expect(deploy).NotTo(BeNil())

			By(cluster.clusterType + " " + cluster.clusterName +
				": annotating the framework managedclusteraddon with custom annotation")
			Kubectl("annotate", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR,
				"addon.open-cluster-management.io/on-multicluster-hub=true")

			By(cluster.clusterType + " " + cluster.clusterName +
				": checking the number of containers in the deployment")
			Eventually(func() int {
				deploy = GetWithTimeout(cluster.clusterClient, gvrDeployment,
					case1FrameworkDeploymentName, addonNamespace, true, 30)
				spec := deploy.Object["spec"].(map[string]interface{})["template"].(map[string]interface{})["spec"]
				containers := spec.(map[string]interface{})["containers"]

				return len(containers.([]interface{}))
			}, 60, 1).Should(Equal(2))

			By(cluster.clusterType + " " + cluster.clusterName +
				": verifying all replicas in framework deployment are available")
			Eventually(func() bool {
				deploy = GetWithTimeout(
					cluster.clusterClient, gvrDeployment, case1FrameworkDeploymentName, addonNamespace, true, 30,
				)
				status := deploy.Object["status"]
				replicas := status.(map[string]interface{})["replicas"]
				availableReplicas := status.(map[string]interface{})["availableReplicas"]

				return (availableReplicas != nil) && replicas.(int64) == availableReplicas.(int64)
			}, 240, 1).Should(Equal(true))

			By(cluster.clusterType + " " + cluster.clusterName + ": verifying a framework pod is running")
			Eventually(func() bool {
				opts := metav1.ListOptions{
					LabelSelector: case1FrameworkPodSelector,
				}
				pods := ListWithTimeoutByNamespace(cluster.clusterClient, gvrPod, opts, addonNamespace, 1, true, 30)
				phase := pods.Items[0].Object["status"].(map[string]interface{})["phase"]

				return phase.(string) == "Running"
			}, 60, 1).Should(Equal(true))

			By(cluster.clusterType + " " + cluster.clusterName +
				": showing the framework managedclusteraddon as available")
			Eventually(func() bool {
				addon := GetWithTimeout(
					clientDynamic, gvrManagedClusterAddOn, case1FrameworkDeploymentName, cluster.clusterName, true, 30,
				)

				return getAddonStatus(addon)
			}, 240, 1).Should(Equal(true))

			By(cluster.clusterType + " " + cluster.clusterName + ": deleting the managedclusteraddon")
			Kubectl("delete", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR)
			deploy = GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case1FrameworkDeploymentName, addonNamespace, false, 30,
			)
			Expect(deploy).To(BeNil())
		}
	})

	It("should revert edits to the ManifestWork by default", func() {
		for _, cluster := range managedClusterList {
			By(cluster.clusterType + " " + cluster.clusterName +
				": deploying the default framework managedclusteraddon")
			Kubectl("apply", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR)
			deploy := GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case1FrameworkDeploymentName, addonNamespace, true, 30,
			)
			Expect(deploy).NotTo(BeNil())

			By(cluster.clusterType + " " + cluster.clusterName +
				": getting the default number of items in the ManifestWork")
			defaultLength := 0
			Eventually(func() int {
				mw := GetWithTimeout(clientDynamic, gvrManifestWork, case1MWName, cluster.clusterName, true, 15)
				manifests, _, _ := unstructured.NestedSlice(mw.Object, "spec", "workload", "manifests")
				defaultLength = len(manifests)

				return defaultLength
			}, 60, 5).ShouldNot(Equal(0))

			By(cluster.clusterType + " " + cluster.clusterName + ": patching the ManifestWork to add an item")
			Kubectl("patch", "-n", cluster.clusterName, "manifestwork", case1MWName, "--type=json",
				"--patch-file="+case1MWPatch)

			By(cluster.clusterType + " " + cluster.clusterName + ": verifying the edit is reverted")
			Eventually(func() int {
				mw := GetWithTimeout(clientDynamic, gvrManifestWork, case1MWName, cluster.clusterName, true, 15)
				manifests, _, _ := unstructured.NestedSlice(mw.Object, "spec", "workload", "manifests")

				return len(manifests)
			}, 60, 5).Should(Equal(defaultLength))

			By(cluster.clusterType + " " + cluster.clusterName + ": deleting the managedclusteraddon")
			Kubectl("delete", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR)
			deploy = GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case1FrameworkDeploymentName, addonNamespace, false, 30,
			)
			Expect(deploy).To(BeNil())
		}
	})
	It("should preserve edits to the ManifestWork if paused by annotation", func() {
		for _, cluster := range managedClusterList {
			By(cluster.clusterType + " " + cluster.clusterName +
				": deploying the default framework managedclusteraddon")
			Kubectl("apply", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR)
			deploy := GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case1FrameworkDeploymentName, addonNamespace, true, 30,
			)
			Expect(deploy).NotTo(BeNil())

			By(cluster.clusterType + " " + cluster.clusterName +
				": annotating the managedclusteraddon with the pause annotation")
			Kubectl("annotate", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR, "policy-addon-pause=true")

			By(cluster.clusterType + " " + cluster.clusterName +
				": getting the default number of items in the ManifestWork")
			defaultLength := 0
			Eventually(func() int {
				mw := GetWithTimeout(clientDynamic, gvrManifestWork, case1MWName, cluster.clusterName, true, 15)
				manifests, _, _ := unstructured.NestedSlice(mw.Object, "spec", "workload", "manifests")
				defaultLength = len(manifests)

				return defaultLength
			}, 60, 5).ShouldNot(Equal(0))

			By(cluster.clusterType + " " + cluster.clusterName + ": patching the ManifestWork to add an item")
			Kubectl("patch", "-n", cluster.clusterName, "manifestwork", case1MWName, "--type=json",
				"--patch-file="+case1MWPatch)

			By(cluster.clusterType + " " + cluster.clusterName + ": verifying the edit is not reverted")
			Consistently(func() int {
				mw := GetWithTimeout(clientDynamic, gvrManifestWork, case1MWName, cluster.clusterName, true, 15)
				manifests, _, _ := unstructured.NestedSlice(mw.Object, "spec", "workload", "manifests")

				return len(manifests)
			}, 30, 5).Should(Equal(defaultLength + 1))

			By(cluster.clusterType + " " + cluster.clusterName + ": deleting the managedclusteraddon")
			Kubectl("delete", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR)
			deploy = GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case1FrameworkDeploymentName, addonNamespace, false, 30,
			)
			Expect(deploy).To(BeNil())
		}
	})
})
