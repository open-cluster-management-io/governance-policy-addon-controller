// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	case2ManagedClusterAddOnCR string = "../resources/config_policy_addon_cr.yaml"
	case2ConfigDeploymentName  string = "config-policy-controller"
	case2ConfigPodSelector     string = "app=config-policy-controller"
)

var _ = Describe("Test config-policy-controller deployment", func() {
	It("should create the default config-policy-controller deployment on the managed cluster", func() {
		for _, cluster := range managedClusterList {
			By(cluster.clusterType + " " + cluster.clusterName +
				": deploying the default config-policy-controller managedclusteraddon")
			Kubectl("apply", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR)
			deploy := GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case2ConfigDeploymentName, addonNamespace, true, 30,
			)
			Expect(deploy).NotTo(BeNil())

			By(cluster.clusterType + " " + cluster.clusterName +
				": checking the number of containers in the deployment")
			Eventually(func() int {
				deploy = GetWithTimeout(
					cluster.clusterClient, gvrDeployment, case2ConfigDeploymentName, addonNamespace, true, 30,
				)
				spec := deploy.Object["spec"].(map[string]interface{})["template"].(map[string]interface{})["spec"]
				containers := spec.(map[string]interface{})["containers"]

				return len(containers.([]interface{}))
			}, 60, 1).Should(Equal(1))

			By(cluster.clusterType + " " + cluster.clusterName +
				": verifying all replicas in config-policy-controller deployment are available")
			Eventually(func() bool {
				deploy = GetWithTimeout(
					cluster.clusterClient, gvrDeployment, case2ConfigDeploymentName, addonNamespace, true, 30,
				)
				status := deploy.Object["status"]
				replicas := status.(map[string]interface{})["replicas"]
				availableReplicas := status.(map[string]interface{})["availableReplicas"]

				return (availableReplicas != nil) && replicas.(int64) == availableReplicas.(int64)
			}, 240, 1).Should(Equal(true))

			By(cluster.clusterType + " " + cluster.clusterName +
				": verifying a running config-policy-controller pod")
			Eventually(func() bool {
				opts := metav1.ListOptions{
					LabelSelector: case2ConfigPodSelector,
				}
				pods := ListWithTimeoutByNamespace(cluster.clusterClient, gvrPod, opts, addonNamespace, 1, true, 30)
				phase := pods.Items[0].Object["status"].(map[string]interface{})["phase"]

				return phase.(string) == "Running"
			}, 60, 1).Should(Equal(true))

			By(cluster.clusterType + " " + cluster.clusterName +
				": showing the config-policy-controller managedclusteraddon as available")
			Eventually(func() bool {
				addon := GetWithTimeout(
					clientDynamic, gvrManagedClusterAddOn, case2ConfigDeploymentName, cluster.clusterName, true, 30,
				)

				return getAddonStatus(addon)
			}, 240, 1).Should(Equal(true))

			By(cluster.clusterType + " " + cluster.clusterName +
				": removing the config-policy-controller deployment when the ManagedClusterAddOn CR is removed")
			Kubectl("delete", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR)
			deploy = GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case2ConfigDeploymentName, addonNamespace, false, 30,
			)
			Expect(deploy).To(BeNil())
		}
	})
})
