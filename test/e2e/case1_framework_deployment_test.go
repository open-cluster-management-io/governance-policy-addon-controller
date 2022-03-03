// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	case1ManagedClusterAddOnCR   string = "../resources/framework_addon_cr.yaml"
	case1FrameworkDeploymentName string = "governance-policy-framework"
	case1FrameworkPodSelector    string = "app=governance-policy-framework"
)

var _ = Describe("Test framework deployment", func() {
	It("should create the default framework deployment on the managed cluster", func() {
		Kubectl("apply", "-f", case1ManagedClusterAddOnCR)
		deploy := GetWithTimeout(clientDynamic, gvrDeployment, case1FrameworkDeploymentName, addonNamespace, true, 30)
		Expect(deploy).NotTo(BeNil())

		By("checking the number of containers in the deployment")
		Eventually(func() int {
			deploy := GetWithTimeout(clientDynamic, gvrDeployment,
				case1FrameworkDeploymentName, addonNamespace, true, 30)
			spec := deploy.Object["spec"].(map[string]interface{})["template"].(map[string]interface{})["spec"]
			containers := spec.(map[string]interface{})["containers"]

			return len(containers.([]interface{}))
		}, 60, 1).Should(Equal(3))
	})
	It("should have a framework pod that is running", func() {
		Eventually(func() bool {
			opts := metav1.ListOptions{
				LabelSelector: case1FrameworkPodSelector,
			}
			pods := ListWithTimeoutByNamespace(clientDynamic, gvrPod, opts, addonNamespace, 1, true, 30)
			phase := pods.Items[0].Object["status"].(map[string]interface{})["phase"]

			return phase.(string) == "Running"
		}, 60, 1).Should(Equal(true))
	})
	It("should show the framework managedclusteraddon as available", func() {
		Eventually(func() bool {
			addon := GetWithTimeout(
				clientDynamic, gvrManagedClusterAddOn, case1FrameworkDeploymentName, "cluster1", true, 30,
			)

			return getAddonStatus(addon)
		}, 240, 1).Should(Equal(true))
	})
	It("should remove the framework deployment when the ManagedClusterAddOn CR is removed", func() {
		Kubectl("delete", "-f", case1ManagedClusterAddOnCR)
		deploy := GetWithTimeout(clientDynamic, gvrDeployment, case1FrameworkDeploymentName, addonNamespace, false, 30)
		Expect(deploy).To(BeNil())
	})
	It("should deploy with 2 containers if onManagedClusterHub is set in helm values annotation", func() {
		By("deploying the default framework managedclusteraddon")
		Kubectl("apply", "-f", case1ManagedClusterAddOnCR)
		deploy := GetWithTimeout(clientDynamic, gvrDeployment, case1FrameworkDeploymentName, addonNamespace, true, 30)
		Expect(deploy).NotTo(BeNil())

		By("annotating the framework managedclusteraddon with helm values")
		Kubectl("annotate", "-f", case1ManagedClusterAddOnCR,
			"addon.open-cluster-management.io/values={\"onMulticlusterHub\":true}")

		Eventually(func() int {
			deploy := GetWithTimeout(clientDynamic, gvrDeployment,
				case1FrameworkDeploymentName, addonNamespace, true, 30)
			spec := deploy.Object["spec"].(map[string]interface{})["template"].(map[string]interface{})["spec"]
			containers := spec.(map[string]interface{})["containers"]

			return len(containers.([]interface{}))
		}, 60, 1).Should(Equal(2))

		By("showing the framework managedclusteraddon as available")
		Eventually(func() bool {
			addon := GetWithTimeout(
				clientDynamic, gvrManagedClusterAddOn, case1FrameworkDeploymentName, "cluster1", true, 30,
			)

			return getAddonStatus(addon)
		}, 240, 1).Should(Equal(true))

		By("deleting the managedclusteraddon")
		Kubectl("delete", "-f", case1ManagedClusterAddOnCR)
		deploy = GetWithTimeout(clientDynamic, gvrDeployment, case1FrameworkDeploymentName, addonNamespace, false, 30)
		Expect(deploy).To(BeNil())
	})
	It("should deploy with 2 containers if onManagedClusterHub is set in the custom annotation", func() {
		By("deploying the default framework managedclusteraddon")
		Kubectl("apply", "-f", case1ManagedClusterAddOnCR)
		deploy := GetWithTimeout(clientDynamic, gvrDeployment, case1FrameworkDeploymentName, addonNamespace, true, 30)
		Expect(deploy).NotTo(BeNil())

		By("annotating the framework managedclusteraddon with helm values")
		Kubectl("annotate", "-f", case1ManagedClusterAddOnCR,
			"addon.open-cluster-management.io/on-multicluster-hub=true")

		Eventually(func() int {
			deploy := GetWithTimeout(clientDynamic, gvrDeployment,
				case1FrameworkDeploymentName, addonNamespace, true, 30)
			spec := deploy.Object["spec"].(map[string]interface{})["template"].(map[string]interface{})["spec"]
			containers := spec.(map[string]interface{})["containers"]

			return len(containers.([]interface{}))
		}, 60, 1).Should(Equal(2))

		By("showing the framework managedclusteraddon as available")
		Eventually(func() bool {
			addon := GetWithTimeout(
				clientDynamic, gvrManagedClusterAddOn, case1FrameworkDeploymentName, "cluster1", true, 30,
			)

			return getAddonStatus(addon)
		}, 240, 1).Should(Equal(true))

		By("deleting the managedclusteraddon")
		Kubectl("delete", "-f", case1ManagedClusterAddOnCR)
		deploy = GetWithTimeout(clientDynamic, gvrDeployment, case1FrameworkDeploymentName, addonNamespace, false, 30)
		Expect(deploy).To(BeNil())
	})
})
