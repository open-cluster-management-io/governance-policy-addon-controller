// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

const (
	case2ManagedClusterAddOnCR string = "../resources/config_policy_addon_cr.yaml"
	case2DeploymentName        string = "config-policy-controller"
	case2PodSelector           string = "app=config-policy-controller"
	case2OpenShiftClusterClaim string = "../resources/openshift_cluster_claim.yaml"
)

func verifyConfigPolicyDeployment(
	logPrefix string, client dynamic.Interface, clusterName, namespace string, clusterNum int,
) {
	By(logPrefix + "checking the number of containers in the deployment")

	deploy := GetWithTimeout(
		client, gvrDeployment, case2DeploymentName, namespace, true, 30,
	)
	Expect(deploy).NotTo(BeNil())

	Eventually(func() int {
		deploy = GetWithTimeout(
			client, gvrDeployment, case2DeploymentName, namespace, true, 30,
		)
		containers, _, _ := unstructured.NestedSlice(deploy.Object, "spec", "template", "spec", "containers")

		return len(containers)
	}, 60, 1).Should(Equal(1))

	if startupProbeInCluster(clusterNum) {
		By(logPrefix + "verifying all replicas in config-policy-controller deployment are available")
		Eventually(func() bool {
			deploy = GetWithTimeout(
				client, gvrDeployment, case2DeploymentName, namespace, true, 30,
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
		pods := ListWithTimeoutByNamespace(client, gvrPod, opts, namespace, 1, true, 30)

		phase, _, _ := unstructured.NestedString(pods.Items[0].Object, "status", "phase")

		return phase == "Running"
	}, 60, 1).Should(Equal(true))

	By(logPrefix + "showing the config-policy-controller managedclusteraddon as available")
	Eventually(func() bool {
		addon := GetWithTimeout(
			clientDynamic, gvrManagedClusterAddOn, case2DeploymentName, clusterName, true, 30,
		)

		return getAddonStatus(addon)
	}, 240, 1).Should(Equal(true))
}

var _ = Describe("Test config-policy-controller deployment", func() {
	It("should create the default config-policy-controller deployment on the managed cluster", func() {
		for i, cluster := range managedClusterList {
			logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "
			By(logPrefix + "deploying the default config-policy-controller managedclusteraddon")
			Kubectl("apply", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR)

			verifyConfigPolicyDeployment(logPrefix, cluster.clusterClient, cluster.clusterName, addonNamespace, i)

			By(logPrefix +
				"removing the config-policy-controller deployment when the ManagedClusterAddOn CR is removed")
			Kubectl("delete", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR)
			deploy := GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, false, 30,
			)
			Expect(deploy).To(BeNil())
		}
	})

	It("should create the default config-policy-controller deployment in hosted mode", Label("hosted-mode"), func() {
		for i, cluster := range managedClusterList[1:] {
			Expect(cluster.clusterType).To(Equal("managed"))

			cluster = managedClusterConfig{
				clusterClient: cluster.clusterClient,
				clusterName:   cluster.clusterName,
				clusterType:   cluster.clusterType,
				hostedOnHub:   true,
			}
			hubClusterConfig := managedClusterList[0]
			hubClient := hubClusterConfig.clusterClient
			installNamespace := fmt.Sprintf("%s-hosted", cluster.clusterName)
			logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "

			By(logPrefix + "creating the config-policy-controller-managed-kubeconfig secret")
			installNamespaceObject := unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Namespace",
				"metadata": map[string]interface{}{
					"name": installNamespace,
				},
			}}

			_, err := hubClient.Resource(gvrNamespace).Create(
				context.TODO(), &installNamespaceObject, metav1.CreateOptions{},
			)
			if !errors.IsAlreadyExists(err) {
				Expect(err).To(BeNil())
			}

			secret := unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata": map[string]interface{}{
					"name": "config-policy-controller-managed-kubeconfig",
				},
				"stringData": map[string]interface{}{
					"kubeconfig": string(hubKubeconfigInternal),
				},
			}}
			_, err = hubClient.Resource(gvrSecret).Namespace(installNamespace).Create(
				context.TODO(), &secret, metav1.CreateOptions{},
			)
			Expect(err).To(BeNil())

			By(logPrefix + "deploying the default config-policy-controller ManagedClusterAddOn in hosted mode")
			addon := unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "addon.open-cluster-management.io/v1alpha1",
				"kind":       "ManagedClusterAddOn",
				"metadata": map[string]interface{}{
					"name": "config-policy-controller",
					"annotations": map[string]interface{}{
						"addon.open-cluster-management.io/hosting-cluster-name": managedClusterList[0].clusterName,
					},
				},
				"spec": map[string]interface{}{
					"installNamespace": installNamespace,
				},
			}}
			_, err = hubClient.Resource(gvrManagedClusterAddOn).Namespace(cluster.clusterName).Create(
				context.TODO(), &addon, metav1.CreateOptions{},
			)
			Expect(err).To(BeNil())

			verifyConfigPolicyDeployment(logPrefix, hubClient, cluster.clusterName, installNamespace, i)

			By(logPrefix +
				"removing the config-policy-controller deployment when the ManagedClusterAddOn CR is removed")
			err = hubClient.Resource(gvrSecret).Namespace(installNamespace).Delete(
				context.TODO(), secret.GetName(), metav1.DeleteOptions{},
			)
			Expect(err).To(BeNil())

			err = clientDynamic.Resource(gvrManagedClusterAddOn).Namespace(cluster.clusterName).Delete(
				context.TODO(), addon.GetName(), metav1.DeleteOptions{},
			)
			Expect(err).To(BeNil())

			deploy := GetWithTimeout(
				hubClient, gvrDeployment, case2DeploymentName, installNamespace, false, 30,
			)
			Expect(deploy).To(BeNil())

			namespace := GetWithTimeout(hubClient, gvrNamespace, installNamespace, "", false, 30)
			Expect(namespace).To(BeNil())
		}
	})

	It("should create a config-policy-controller deployment with customizations", func() {
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

			By(
				logPrefix + "annotating the managedclusteraddon with the " + prometheusEnabledAnnotation +
					" annotation",
			)
			Kubectl(
				"annotate",
				"-n",
				cluster.clusterName,
				"-f",
				case2ManagedClusterAddOnCR,
				prometheusEnabledAnnotation,
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

			By(logPrefix + "verifying that the metrics ServiceMonitor exists")
			Eventually(func(g Gomega) {
				sm, err := cluster.clusterClient.Resource(gvrServiceMonitor).Namespace(addonNamespace).Get(
					context.TODO(), "ocm-config-policy-controller-metrics", metav1.GetOptions{},
				)
				g.Expect(err).To(BeNil())

				endpoints, _, _ := unstructured.NestedSlice(sm.Object, "spec", "endpoints")
				g.Expect(len(endpoints)).ToNot(Equal(0))
				g.Expect(endpoints[0].(map[string]interface{})["scheme"].(string)).To(Equal("http"))
			}, 60, 3).Should(Succeed())

			By(logPrefix + "verifying that the metrics Service exists")
			Eventually(func(g Gomega) {
				service, err := cluster.clusterClient.Resource(gvrService).Namespace(addonNamespace).Get(
					context.TODO(), "config-policy-controller-metrics", metav1.GetOptions{},
				)
				g.Expect(err).To(BeNil())

				ports, _, _ := unstructured.NestedSlice(service.Object, "spec", "ports")
				g.Expect(len(ports)).To(Equal(1))
				port := ports[0].(map[string]interface{})
				g.Expect(port["port"].(int64)).To(Equal(int64(8080)))
				g.Expect(port["targetPort"].(int64)).To(Equal(int64(8383)))
			}, 60, 3).Should(Succeed())

			By(logPrefix +
				"removing the config-policy-controller deployment when the ManagedClusterAddOn CR is removed")
			Kubectl("delete", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR)
			deploy = GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, false, 30,
			)
			Expect(deploy).To(BeNil())
		}
	})

	It("should create a config-policy-controller deployment with metrics monitoring on OpenShift clusters", func() {
		Expect(len(managedClusterList)).ToNot(Equal(0))
		hubClient := managedClusterList[0].clusterClient

		for i, cluster := range managedClusterList {
			logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "

			By(logPrefix + "creating the openshift-monitoring namespace")
			Kubectl(
				"create",
				"namespace",
				"openshift-monitoring",
				fmt.Sprintf("--kubeconfig=%s%d.kubeconfig", kubeconfigFilename, i+1),
			)

			By(logPrefix + "setting the product.open-cluster-management.io ClusterClaim to OpenShift")
			Kubectl(
				"apply",
				"-f",
				case2OpenShiftClusterClaim,
				fmt.Sprintf("--kubeconfig=%s%d.kubeconfig", kubeconfigFilename, i+1),
			)

			By(logPrefix + "waiting for the ClusterClaim to be in the ManagedCluster status")
			Eventually(func(g Gomega) {
				managedCluster, err := hubClient.Resource(gvrManagedCluster).Get(
					context.TODO(), cluster.clusterName, metav1.GetOptions{},
				)
				g.Expect(err).To(BeNil())

				clusterClaims, _, _ := unstructured.NestedSlice(managedCluster.Object, "status", "clusterClaims")
				g.Expect(len(clusterClaims)).ToNot(Equal(0))

				var claimValue string
				for _, clusterClaim := range clusterClaims {
					clusterClaim := clusterClaim.(map[string]interface{})
					if clusterClaim["name"].(string) == "product.open-cluster-management.io" {
						claimValue = clusterClaim["value"].(string)

						break
					}
				}

				g.Expect(claimValue).To(Equal("OpenShift"))
			}, 60, 1).Should(Succeed())

			// The status doesn't need to be checked on the deployment because the deployment requires a cert that
			// is auto-generated by OpenShift, which won't be present.
			By(logPrefix + "deploying the default config-policy-controller managedclusteraddon")
			Kubectl("apply", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR)
			deploy := GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, true, 30,
			)
			Expect(deploy).NotTo(BeNil())

			By(logPrefix + "verifying that the Deployment has the kube-rbac-proxy")
			containers, _, _ := unstructured.NestedSlice(deploy.Object, "spec", "template", "spec", "containers")
			Expect(len(containers)).To(Equal(2))
			Expect(containers[0].(map[string]interface{})["name"]).To(Equal("kube-rbac-proxy"))

			By(logPrefix + "verifying that the metrics ServiceMonitor exists")
			Eventually(func(g Gomega) {
				sm, err := cluster.clusterClient.Resource(gvrServiceMonitor).Namespace("openshift-monitoring").Get(
					context.TODO(), "ocm-config-policy-controller-metrics", metav1.GetOptions{},
				)
				g.Expect(err).To(BeNil())

				endpoints, _, _ := unstructured.NestedSlice(sm.Object, "spec", "endpoints")
				g.Expect(len(endpoints)).ToNot(Equal(0))
				g.Expect(endpoints[0].(map[string]interface{})["scheme"].(string)).To(Equal("https"))
			}, 120, 3).Should(Succeed())

			By(logPrefix + "verifying that the metrics Service exists")
			Eventually(func(g Gomega) {
				service, err := cluster.clusterClient.Resource(gvrService).Namespace(addonNamespace).Get(
					context.TODO(), "config-policy-controller-metrics", metav1.GetOptions{},
				)
				g.Expect(err).To(BeNil())

				ports, _, _ := unstructured.NestedSlice(service.Object, "spec", "ports")
				g.Expect(len(ports)).To(Equal(1))
				port := ports[0].(map[string]interface{})
				g.Expect(port["port"].(int64)).To(Equal(int64(8443)))
				g.Expect(port["targetPort"].(int64)).To(Equal(int64(8443)))
			}, 120, 3).Should(Succeed())

			By(logPrefix + "cleaning up")
			Kubectl(
				"delete",
				"-f",
				case2OpenShiftClusterClaim,
				fmt.Sprintf("--kubeconfig=%s%d.kubeconfig", kubeconfigFilename, i+1),
			)

			Kubectl("delete", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR)
			deploy = GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, false, 30,
			)
			Expect(deploy).To(BeNil())

			Kubectl(
				"delete",
				"namespace",
				"openshift-monitoring",
				fmt.Sprintf("--kubeconfig=%s%d.kubeconfig", kubeconfigFilename, i+1),
			)

			By(logPrefix + "waiting for the ClusterClaim to not be in the ManagedCluster status")
			Eventually(func(g Gomega) {
				managedCluster, err := hubClient.Resource(gvrManagedCluster).Get(
					context.TODO(), cluster.clusterName, metav1.GetOptions{},
				)
				g.Expect(err).To(BeNil())

				clusterClaims, _, _ := unstructured.NestedSlice(managedCluster.Object, "status", "clusterClaims")
				g.Expect(len(clusterClaims)).To(Equal(0))
			}, 60, 1).Should(Succeed())
		}
	})
})
