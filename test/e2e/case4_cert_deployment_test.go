// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	case4ManagedClusterAddOnCR string = "../resources/cert_policy_addon_cr.yaml"
	case4DeploymentName        string = "cert-policy-controller"
)

var _ = Describe("Test cert policy controller deployment", func() {
	It("should create the cert-policy-controller deployment on the managed cluster", func() {
		Kubectl("apply", "-f", case4ManagedClusterAddOnCR)
		deploy := GetWithTimeout(clientDynamic, gvrDeployment, case4DeploymentName, addonNamespace, true, 30)
		Expect(deploy).NotTo(BeNil())

	})
	It("should have all replicas in cert-policy-controller deployment available", func() {
		Eventually(func() bool {
			deploy := GetWithTimeout(clientDynamic, gvrDeployment, case4DeploymentName, addonNamespace, true, 30)
			status := deploy.Object["status"].(map[string]interface{})

			return (status["availableReplicas"] != nil) && status["replicas"].(int64) == status["availableReplicas"].(int64)

		}, 240, 1).Should(Equal(true))
	})
	It("should delete the cert-policy-controller deployment when the ManagedClusterAddOn CR is removed", func() {
		Kubectl("delete", "-f", case4ManagedClusterAddOnCR)
		deploy := GetWithTimeout(clientDynamic, gvrDeployment, case4DeploymentName, addonNamespace, false, 30)
		Expect(deploy).To(BeNil())
	})
})
