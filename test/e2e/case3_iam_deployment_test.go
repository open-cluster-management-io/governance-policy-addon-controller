// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	case3ManagedClusterAddOnCR string = "../resources/iam_policy_addon_cr.yaml"
	case3DeploymentName        string = "iam-policy-controller"
)

var _ = Describe("Test iam policy controller deployment", func() {
	It("should create the iam-policy-controller deployment on the managed cluster", func() {
		Kubectl("apply", "-f", case3ManagedClusterAddOnCR)
		deploy := GetWithTimeout(clientDynamic, gvrDeployment, case3DeploymentName, addonNamespace, true, 30)
		Expect(deploy).NotTo(BeNil())
	})
	It("should have all replicas in iam-policy-controller deployment available", func() {
		Eventually(func() bool {
			deploy := GetWithTimeout(clientDynamic, gvrDeployment, case3DeploymentName, addonNamespace, true, 30)
			status := deploy.Object["status"]
			replicas := status.(map[string]interface{})["replicas"]
			availableReplicas := status.(map[string]interface{})["availableReplicas"]

			return (availableReplicas != nil) && replicas.(int64) == availableReplicas.(int64)
		}, 240, 1).Should(Equal(true))
	})
	It("should delete the iam-policy-controller deployment when the ManagedClusterAddOn CR is removed", func() {
		Kubectl("delete", "-f", case3ManagedClusterAddOnCR)
		deploy := GetWithTimeout(clientDynamic, gvrDeployment, case3DeploymentName, addonNamespace, false, 30)
		Expect(deploy).To(BeNil())
	})
})
