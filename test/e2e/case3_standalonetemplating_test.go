// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	case3ManagedClusterAddOnCR           string = "../resources/standalonetemplating_addon_cr.yaml"
	case3ClusterManagementAddOnDefaultCR string = "../resources/standalonetemplating_clustermanagementaddon.yaml"
	case3SecretName                      string = "governance-standalone-hub-templating-info"
)

var _ = Describe("Test config-policy-controller deployment with standalone templating", Ordered, func() {
	BeforeAll(func() {
		By("Deploying the default config-policy-controller and governance-standalone-hub-templating " +
			"ClusterManagementAddons to the hub cluster")
		Kubectl("apply", "-f", case2ClusterManagementAddOnCRDefault)
		Kubectl("apply", "-f", case3ClusterManagementAddOnDefaultCR)
	})

	AfterAll(func() {
		By("Deleting the default config-policy-controller and governance-standalone-hub-templating " +
			"ClusterManagementAddons from the hub cluster")
		Kubectl("delete", "-f", case2ClusterManagementAddOnCRDefault)
		Kubectl("delete", "-f", case3ClusterManagementAddOnDefaultCR)

		By("Deleting the default config-policy-controller and governance-standalone-hub-templating " +
			"ManagedClusterAddons on each cluster")
		for _, cluster := range managedClusterList {
			Kubectl("delete", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR, "--ignore-not-found=true")
			Kubectl("delete", "-n", cluster.clusterName, "-f", case3ManagedClusterAddOnCR, "--ignore-not-found=true")
		}
	})

	It("should not have hub templating enabled when the standalone-templating addon does not exist", func() {
		for _, cluster := range managedClusterList {
			logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "
			By(logPrefix + "deploying the default config-policy-controller managedclusteraddon")
			Kubectl("apply", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR)

			By(logPrefix + "verifying the standalone-hub-templates arg is not set")

			deploy := GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, true, 60,
			)
			Expect(deploy).NotTo(BeNil())

			Eventually(func(g Gomega) []string {
				deploy = GetWithTimeout(
					cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, true, 30,
				)
				containers, _, _ := unstructured.NestedSlice(deploy.Object, "spec", "template", "spec", "containers")
				g.Expect(containers).Should(HaveLen(1))

				cont, ok := containers[0].(map[string]any)
				g.Expect(ok).To(BeTrue())

				args, _, _ := unstructured.NestedStringSlice(cont, "args")

				return args
			}, 60, 1).ShouldNot(ContainElement(ContainSubstring("standalone-hub-templates")))
		}
	})

	It("should have hub templating enabled after the standalone-templating addon is created", func() {
		for _, cluster := range managedClusterList {
			logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "
			By(logPrefix + "deploying the default governance-standalone-hub-templating managedclusteraddon")
			Kubectl("apply", "-n", cluster.clusterName, "-f", case3ManagedClusterAddOnCR)

			By(logPrefix + "verifying the standalone-hub-templates arg is set")

			deploy := GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, true, 60,
			)
			Expect(deploy).NotTo(BeNil())

			Eventually(func(g Gomega) []string {
				deploy = GetWithTimeout(
					cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, true, 30,
				)
				containers, _, _ := unstructured.NestedSlice(deploy.Object, "spec", "template", "spec", "containers")
				g.Expect(containers).Should(HaveLen(1))

				cont, ok := containers[0].(map[string]any)
				g.Expect(ok).To(BeTrue())

				args, _, _ := unstructured.NestedStringSlice(cont, "args")

				return args
			}, 60, 1).Should(ContainElement(ContainSubstring("standalone-hub-templates")))

			By(logPrefix + "verifying the " + case3SecretName + " secret was created")

			secret := GetWithTimeout(
				cluster.clusterClient, gvrSecret, case3SecretName, addonNamespace, true, 30,
			)
			Expect(secret).NotTo(BeNil())
		}
	})
})
