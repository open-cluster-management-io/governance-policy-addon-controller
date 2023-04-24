// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

const (
	case2ManagedClusterAddOnName  string = "config-policy-controller"
	case2ManagedClusterAddOnCR    string = "../resources/config_policy_addon_cr.yaml"
	case2ClusterManagementAddOnCR string = "../resources/config_policy_clustermanagementaddon.yaml"
	case2DeploymentName           string = "config-policy-controller"
	case2PodSelector              string = "app=config-policy-controller"
	case2OpenShiftClusterClaim    string = "../resources/openshift_cluster_claim.yaml"
)

func verifyConfigPolicyDeployment(
	logPrefix string, client dynamic.Interface, clusterName, namespace string, clusterNum int,
) {
	By(logPrefix + "checking the number of containers in the deployment")

	deploy := GetWithTimeout(
		client, gvrDeployment, case2DeploymentName, namespace, true, 60,
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
			Kubectl("delete", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR, "--timeout=30s")
			deploy := GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, false, 180,
			)
			Expect(deploy).To(BeNil())
		}
	})

	It("should create a config-policy-controller deployment with node selector on the managed cluster", func() {
		By("Creating the AddOnDeploymentConfig")
		Kubectl("apply", "-f", addOnDeplomentConfigCR)
		By("Creating the config-policy-controller ClusterManagementAddOn to use the AddOnDeploymentConfig")
		Kubectl("apply", "-f", case2ClusterManagementAddOnCR)

		for i, cluster := range managedClusterList {
			logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "
			By(logPrefix + "deploying the default config-policy-controller managedclusteraddon")
			Kubectl("apply", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR)

			verifyConfigPolicyDeployment(logPrefix, cluster.clusterClient, cluster.clusterName, addonNamespace, i)

			By(logPrefix + "verifying the nodeSelector")
			deploy := GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, true, 30,
			)

			nodeSelector, _, _ := unstructured.NestedStringMap(
				deploy.Object, "spec", "template", "spec", "nodeSelector",
			)
			Expect(nodeSelector).To(Equal(map[string]string{"kubernetes.io/os": "linux"}))

			By(logPrefix + "verifying the tolerations")
			tolerations, _, _ := unstructured.NestedSlice(deploy.Object, "spec", "template", "spec", "tolerations")
			Expect(tolerations).To(HaveLen(1))
			expected := map[string]interface{}{
				"key":      "dedicated",
				"operator": "Equal",
				"value":    "something-else",
				"effect":   "NoSchedule",
			}
			Expect(tolerations[0]).To(Equal(expected))

			By(logPrefix +
				"removing the config-policy-controller deployment when the ManagedClusterAddOn CR is removed")
			Kubectl("delete", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR, "--timeout=30s")
			deploy = GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, false, 180,
			)
			Expect(deploy).To(BeNil())
		}

		By("Deleting the AddOnDeploymentConfig")
		Kubectl("delete", "-f", addOnDeplomentConfigCR, "--timeout=15s")
		By("Deleting the config-policy-controller ClusterManagementAddOn to use the AddOnDeploymentConfig")
		Kubectl("delete", "-f", case2ClusterManagementAddOnCR, "--timeout=15s")
	})

	It("should create the default config-policy-controller deployment in hosted mode", Label("hosted-mode"), func() {
		for _, cluster := range managedClusterList[1:] {
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

			setupClusterSecretForHostedMode(
				logPrefix, hubClient, "config-policy-controller-managed-kubeconfig",
				string(hubKubeconfigInternal), installNamespace)

			installAddonInHostedMode(
				logPrefix, hubClient, case2ManagedClusterAddOnName,
				cluster.clusterName, managedClusterList[0].clusterName, installNamespace)

			verifyConfigPolicyDeployment(logPrefix, hubClient, cluster.clusterName, installNamespace, 0)

			ctx, cancel := context.WithTimeout(context.TODO(), 15*time.Second)
			defer cancel()

			By(logPrefix +
				"removing the config-policy-controller deployment when the ManagedClusterAddOn CR is removed")
			err := clientDynamic.Resource(gvrManagedClusterAddOn).Namespace(cluster.clusterName).Delete(
				ctx, case2ManagedClusterAddOnName, metav1.DeleteOptions{},
			)
			Expect(err).To(BeNil())

			deploy := GetWithTimeout(
				hubClient, gvrDeployment, case2DeploymentName, installNamespace, false, 180,
			)
			Expect(deploy).To(BeNil())

			namespace := GetWithTimeout(hubClient, gvrNamespace, installNamespace, "", false, 30)
			Expect(namespace).To(BeNil())
		}
	})

	It("should create the default config-policy-controller deployment in hosted mode in klusterlet agent namespace",
		Label("hosted-mode"), func() {
			By("Creating the AddOnDeploymentConfig")
			Kubectl("apply", "-f", addOnDeplomentConfigWithCustomVarsCR)
			By("Creating the config-policy-controller ClusterManagementAddOn to use the AddOnDeploymentConfig")
			Kubectl("apply", "-f", case2ClusterManagementAddOnCR)

			for _, cluster := range managedClusterList[1:] {
				Expect(cluster.clusterType).To(Equal("managed"))

				cluster = managedClusterConfig{
					clusterClient: cluster.clusterClient,
					clusterName:   cluster.clusterName,
					clusterType:   cluster.clusterType,
					hostedOnHub:   true,
				}
				hubClusterConfig := managedClusterList[0]
				hubClient := hubClusterConfig.clusterClient
				installNamespace := fmt.Sprintf("klusterlet-%s", cluster.clusterName)
				logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "

				setupClusterSecretForHostedMode(
					logPrefix, hubClient, "external-managed-kubeconfig",
					string(hubKubeconfigInternal), installNamespace)

				installAddonInHostedMode(
					logPrefix, hubClient, case2ManagedClusterAddOnName,
					cluster.clusterName, managedClusterList[0].clusterName, installNamespace)

				verifyConfigPolicyDeployment(logPrefix, hubClient, cluster.clusterName, installNamespace, 0)

				ctx, cancel := context.WithTimeout(context.TODO(), 15*time.Second)
				defer cancel()

				By(logPrefix + "Removing the ManagedClusterAddOn CR")
				err := clientDynamic.Resource(gvrManagedClusterAddOn).Namespace(cluster.clusterName).Delete(
					ctx, case2ManagedClusterAddOnName, metav1.DeleteOptions{},
				)
				Expect(err).To(BeNil())

				By(logPrefix +
					"Verifying controller deployment is removed when the ManagedClusterAddOn CR is removed")

				deploy := GetWithTimeout(
					hubClient, gvrDeployment, case2DeploymentName, installNamespace, false, 180,
				)
				Expect(deploy).To(BeNil())

				By(logPrefix + "Verifying install namespace is not removed when the ManagedClusterAddOn CR is removed")
				namespace := GetWithTimeout(hubClient, gvrNamespace, installNamespace, "", true, 30)
				Expect(namespace).NotTo(BeNil())

				ctxSec, cancelSec := context.WithTimeout(context.TODO(), 15*time.Second)
				defer cancelSec()

				By(logPrefix + "cleaning up  the hosting cluster secret")
				err = hubClient.Resource(gvrSecret).Namespace(installNamespace).Delete(
					ctxSec, "external-managed-kubeconfig", metav1.DeleteOptions{},
				)
				Expect(err).To(BeNil())

				ctxNS, cancelNS := context.WithTimeout(context.TODO(), 15*time.Second)
				defer cancelNS()

				By(logPrefix + "Cleaning up the install namespace")
				err = hubClient.Resource(gvrNamespace).Delete(
					ctxNS, installNamespace, metav1.DeleteOptions{},
				)
				Expect(err).To(BeNil())

				namespace = GetWithTimeout(hubClient, gvrNamespace, installNamespace, "", false, 30)
				Expect(namespace).To(BeNil())
			}
			By("Deleting the AddOnDeploymentConfig")
			Kubectl("delete", "-f", addOnDeplomentConfigWithCustomVarsCR, "--timeout=15s")
			By("Deleting the config-policy-controller ClusterManagementAddOn to use the AddOnDeploymentConfig")
			Kubectl("delete", "-f", case2ClusterManagementAddOnCR, "--timeout=15s")
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

			By(logPrefix + "annotating the managedclusteraddon with the " + prometheusEnabledAnnotation + " annotation")
			Kubectl(
				"annotate",
				"-n",
				cluster.clusterName,
				"-f",
				case2ManagedClusterAddOnCR,
				prometheusEnabledAnnotation,
			)

			By(logPrefix + "annotating the managedclusteraddon with the " + clientQPSAnnotation + " annotation")
			Kubectl("annotate", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR, clientQPSAnnotation)

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
						g.Expect(args).To(ContainElement("--client-max-qps=50"))
						g.Expect(args).To(ContainElement("--leader-elect=false"))
					}
				}
			}, 180, 10).Should(Succeed())

			By(logPrefix + "verifying that the metrics ServiceMonitor exists")
			Eventually(func(g Gomega) {
				sm, err := cluster.clusterClient.Resource(gvrServiceMonitor).Namespace(addonNamespace).Get(
					context.TODO(), "ocm-config-policy-controller-"+addonNamespace+"-metrics", metav1.GetOptions{},
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
			Kubectl("delete", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR, "--timeout=30s")
			deploy = GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, false, 180,
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
					context.TODO(), "ocm-config-policy-controller-"+addonNamespace+"-metrics", metav1.GetOptions{},
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
				"--timeout=15s",
			)

			Kubectl("delete", "-n", cluster.clusterName, "-f", case2ManagedClusterAddOnCR, "--timeout=30s")
			deploy = GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case2DeploymentName, addonNamespace, false, 30,
			)
			Expect(deploy).To(BeNil())

			Kubectl(
				"delete",
				"namespace",
				"openshift-monitoring",
				fmt.Sprintf("--kubeconfig=%s%d.kubeconfig", kubeconfigFilename, i+1),
				"--timeout=15s",
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

func setupClusterSecretForHostedMode(
	logPrefix string, client dynamic.Interface, secretName, secretData, installNamespace string,
) {
	By(logPrefix + "creating  secret " + secretName + " in namespace " + installNamespace)

	installNamespaceObject := unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]interface{}{
			"name": installNamespace,
		},
	}}

	_, err := client.Resource(gvrNamespace).Create(
		context.TODO(), &installNamespaceObject, metav1.CreateOptions{},
	)
	if !errors.IsAlreadyExists(err) {
		Expect(err).To(BeNil())
	}

	secret := unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]interface{}{
			"name": secretName,
		},
		"stringData": map[string]interface{}{
			"kubeconfig": secretData,
		},
	}}
	_, err = client.Resource(gvrSecret).Namespace(installNamespace).Create(
		context.TODO(), &secret, metav1.CreateOptions{},
	)
	Expect(err).To(BeNil())
}
