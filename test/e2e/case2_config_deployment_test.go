// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	case2ManagedClusterAddOnCR string = "../resources/config_policy_addon_cr.yaml"
	case2ConfigDeploymentName  string = "config-policy-controller"
	case2ConfigPodSelector     string = "app=config-policy-controller"
)

var _ = Describe("Test config policy controller deployment", func() {
	It("should create the default config policy controller deployment on the managed cluster", func() {
		Kubectl("apply", "-f", case2ManagedClusterAddOnCR)
		deploy := GetWithTimeout(clientDynamic, gvrDeployment, case2ConfigDeploymentName, addonNamespace, true, 30)
		Expect(deploy).NotTo(BeNil())

		By("checking the number of containers in the deployment")
		Eventually(func() int {
			deploy := GetWithTimeout(clientDynamic, gvrDeployment, case2ConfigDeploymentName, addonNamespace, true, 30)
			template := deploy.Object["spec"].(map[string]interface{})["template"].(map[string]interface{})
			containers := template["spec"].(map[string]interface{})["containers"].([]interface{})
			return len(containers)
		}, 60, 1).Should(Equal(1))
	})
	It("should have a running config policy controller pod", func() {
		Eventually(func() bool {
			opts := metav1.ListOptions{
				LabelSelector: case2ConfigPodSelector,
			}
			pods := ListWithTimeoutByNamespace(clientDynamic, gvrPod, opts, addonNamespace, 1, true, 30)
			status := pods.Items[0].Object["status"].(map[string]interface{})
			phase := status["phase"].(string)
			return phase == "Running"
		}, 60, 1).Should(Equal(true))
	})
	It("should remove the config policy controller deployment when the ManagedClusterAddOn CR is removed", func() {
		Kubectl("delete", "-f", case2ManagedClusterAddOnCR)
		deploy := GetWithTimeout(clientDynamic, gvrDeployment, case2ConfigDeploymentName, addonNamespace, false, 30)
		Expect(deploy).To(BeNil())
	})
})
