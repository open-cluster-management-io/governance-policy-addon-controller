// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	case4ManagedClusterAddOnCR string = "../resources/cert_policy_addon_cr.yaml"
	case4DeploymentName        string = "cert-policy-controller"
	case4PodSelector           string = "app=cert-policy-controller"
)

var _ = Describe("Test cert-policy-controller deployment", func() {
	It("should create the cert-policy-controller deployment on the managed cluster", func() {
		for _, cluster := range managedClusterList {
			By(cluster.clusterType + " " + cluster.clusterName +
				": deploying the default cert-policy-controller managedclusteraddon")
			Kubectl("apply", "-n", cluster.clusterName, "-f", case4ManagedClusterAddOnCR)
			deploy := GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case4DeploymentName, addonNamespace, true, 30,
			)
			Expect(deploy).NotTo(BeNil())

			By(cluster.clusterType + " " + cluster.clusterName +
				": checking the number of containers in the deployment")
			Eventually(func() int {
				deploy = GetWithTimeout(
					cluster.clusterClient, gvrDeployment, case4DeploymentName, addonNamespace, true, 30,
				)
				spec := deploy.Object["spec"].(map[string]interface{})["template"].(map[string]interface{})["spec"]
				containers := spec.(map[string]interface{})["containers"]

				return len(containers.([]interface{}))
			}, 60, 1).Should(Equal(1))

			By(cluster.clusterType + " " + cluster.clusterName +
				": verifying all replicas in cert-policy-controller deployment are available")
			Eventually(func() bool {
				deploy = GetWithTimeout(
					cluster.clusterClient, gvrDeployment, case4DeploymentName, addonNamespace, true, 30,
				)
				status := deploy.Object["status"]
				replicas := status.(map[string]interface{})["replicas"]
				availableReplicas := status.(map[string]interface{})["availableReplicas"]

				return (availableReplicas != nil) && replicas.(int64) == availableReplicas.(int64)
			}, 240, 1).Should(Equal(true))

			By(cluster.clusterType + " " + cluster.clusterName +
				": verifying a running cert-policy-controller pod")
			Eventually(func() bool {
				opts := metav1.ListOptions{
					LabelSelector: case4PodSelector,
				}
				pods := ListWithTimeoutByNamespace(cluster.clusterClient, gvrPod, opts, addonNamespace, 1, true, 30)
				phase := pods.Items[0].Object["status"].(map[string]interface{})["phase"]

				return phase.(string) == "Running"
			}, 60, 1).Should(Equal(true))

			By(cluster.clusterType + " " + cluster.clusterName +
				": showing the cert-policy-controller managedclusteraddon as available")
			Eventually(func() bool {
				addon := GetWithTimeout(
					clientDynamic, gvrManagedClusterAddOn, case4DeploymentName, cluster.clusterName, true, 30,
				)

				return getAddonStatus(addon)
			}, 240, 1).Should(Equal(true))

			By(cluster.clusterType + " " + cluster.clusterName +
				": removing the cert-policy-controller deployment when the ManagedClusterAddOn CR is removed")
			Kubectl("delete", "-n", cluster.clusterName, "-f", case4ManagedClusterAddOnCR)
			deploy = GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case4DeploymentName, addonNamespace, false, 30,
			)
			Expect(deploy).To(BeNil())
		}
	})
})
