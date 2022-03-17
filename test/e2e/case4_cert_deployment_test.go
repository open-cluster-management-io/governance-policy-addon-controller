// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	case4ManagedClusterAddOnCR string = "../resources/cert_policy_addon_cr.yaml"
	case4DeploymentName        string = "cert-policy-controller"
	case4PodSelector           string = "app=cert-policy-controller"
)

var _ = Describe("Test cert-policy-controller deployment", func() {
	It("should create the cert-policy-controller deployment on the managed cluster", func() {
		for _, cluster := range managedClusterList {
			logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "
			By(logPrefix + "deploying the default cert-policy-controller managedclusteraddon")
			Kubectl("apply", "-n", cluster.clusterName, "-f", case4ManagedClusterAddOnCR)
			deploy := GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case4DeploymentName, addonNamespace, true, 30,
			)
			Expect(deploy).NotTo(BeNil())

			By(logPrefix + "checking the number of containers in the deployment")
			Eventually(func() int {
				deploy = GetWithTimeout(
					cluster.clusterClient, gvrDeployment, case4DeploymentName, addonNamespace, true, 30,
				)
				spec := deploy.Object["spec"].(map[string]interface{})["template"].(map[string]interface{})["spec"]
				containers := spec.(map[string]interface{})["containers"]

				return len(containers.([]interface{}))
			}, 60, 1).Should(Equal(1))

			By(logPrefix + "verifying all replicas in cert-policy-controller deployment are available")
			Eventually(func() bool {
				deploy = GetWithTimeout(
					cluster.clusterClient, gvrDeployment, case4DeploymentName, addonNamespace, true, 30,
				)
				status := deploy.Object["status"]
				replicas := status.(map[string]interface{})["replicas"]
				availableReplicas := status.(map[string]interface{})["availableReplicas"]

				return (availableReplicas != nil) && replicas.(int64) == availableReplicas.(int64)
			}, 240, 1).Should(Equal(true))

			By(logPrefix + "verifying a running cert-policy-controller pod")
			Eventually(func() bool {
				opts := metav1.ListOptions{
					LabelSelector: case4PodSelector,
				}
				pods := ListWithTimeoutByNamespace(cluster.clusterClient, gvrPod, opts, addonNamespace, 1, true, 30)
				phase := pods.Items[0].Object["status"].(map[string]interface{})["phase"]

				return phase.(string) == "Running"
			}, 60, 1).Should(Equal(true))

			By(logPrefix + "showing the cert-policy-controller managedclusteraddon as available")
			Eventually(func() bool {
				addon := GetWithTimeout(
					clientDynamic, gvrManagedClusterAddOn, case4DeploymentName, cluster.clusterName, true, 30,
				)

				return getAddonStatus(addon)
			}, 240, 1).Should(Equal(true))

			By(logPrefix + "removing the cert-policy-controller deployment when the ManagedClusterAddOn CR is removed")
			Kubectl("delete", "-n", cluster.clusterName, "-f", case4ManagedClusterAddOnCR)
			deploy = GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case4DeploymentName, addonNamespace, false, 30,
			)
			Expect(deploy).To(BeNil())
		}
	})

	It("should create a cert-policy-controller deployment with custom logging levels", func() {
		for _, cluster := range managedClusterList {
			logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "
			By(logPrefix + "deploying the default cert-policy-controller managedclusteraddon")
			Kubectl("apply", "-n", cluster.clusterName, "-f", case4ManagedClusterAddOnCR)
			deploy := GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case4DeploymentName, addonNamespace, true, 30,
			)
			Expect(deploy).NotTo(BeNil())

			By(logPrefix + "showing the cert-policy-controller managedclusteraddon as available")
			Eventually(func() bool {
				addon := GetWithTimeout(
					clientDynamic, gvrManagedClusterAddOn, case4DeploymentName, cluster.clusterName, true, 30,
				)

				return getAddonStatus(addon)
			}, 240, 1).Should(Equal(true))

			By(logPrefix + "annotating the managedclusteraddon with the " + loggingLevelAnnotation + " annotation")
			Kubectl("annotate", "-n", cluster.clusterName, "-f", case4ManagedClusterAddOnCR, loggingLevelAnnotation)

			By(logPrefix + "verifying a new cert-policy-controller pod is deployed")
			opts := metav1.ListOptions{
				LabelSelector: case4PodSelector,
			}
			_ = ListWithTimeoutByNamespace(cluster.clusterClient, gvrPod, opts, addonNamespace, 2, true, 30)

			By(logPrefix + "verifying the pod has been deployed with a new logging level")
			pods := ListWithTimeoutByNamespace(cluster.clusterClient, gvrPod, opts, addonNamespace, 1, true, 60)
			phase := pods.Items[0].Object["status"].(map[string]interface{})["phase"]

			Expect(phase.(string)).To(Equal("Running"))
			containerList, _, err := unstructured.NestedSlice(pods.Items[0].Object, "spec", "containers")
			if err != nil {
				panic(err)
			}
			for _, container := range containerList {
				if containerObj, ok := container.(map[string]interface{}); ok {
					if Expect(containerObj).To(HaveKey("name")) && containerObj["name"] != case2DeploymentName {
						continue
					}
					if Expect(containerObj).To(HaveKey("args")) {
						args := containerObj["args"]
						Expect(args).To(ContainElement("--log-encoder=console"))
						Expect(args).To(ContainElement("--log-level=8"))
						Expect(args).To(ContainElement("--v=6"))
					}
				} else {
					panic(fmt.Errorf("containerObj type assertion failed"))
				}
			}

			By(logPrefix + "removing the cert-policy-controller deployment when the ManagedClusterAddOn CR is removed")
			Kubectl("delete", "-n", cluster.clusterName, "-f", case4ManagedClusterAddOnCR)
			deploy = GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case4DeploymentName, addonNamespace, false, 30,
			)
			Expect(deploy).To(BeNil())
		}
	})
})
