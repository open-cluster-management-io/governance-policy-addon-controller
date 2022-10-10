// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	case1ManagedClusterAddOnCR string = "../resources/framework_addon_cr.yaml"
	case1hubAnnotationMCAOCR   string = "../resources/framework_hub_annotation_addon_cr.yaml"
	case1hubValuesMCAOCR       string = "../resources/framework_hub_values_addon_cr.yaml"
	case1DeploymentName        string = "governance-policy-framework"
	case1PodSelector           string = "app=governance-policy-framework"
	case1MWName                string = "addon-governance-policy-framework-deploy"
	case1MWPatch               string = "../resources/manifestwork_add_patch.json"
)

var _ = Describe("Test framework deployment", func() {
	It("should create the default framework deployment on separate managed clusters", func() {
		for i, cluster := range managedClusterList[1:] {
			Expect(cluster.clusterType).To(Equal("managed"))

			logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "
			By(logPrefix + "deploying the default framework managedclusteraddon")
			Kubectl("apply", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR)
			deploy := GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case1DeploymentName, addonNamespace, true, 60,
			)
			Expect(deploy).NotTo(BeNil())

			checkContainersAndAvailability(cluster, i+1)

			expectedArgs := []string{"--cluster-namespace=" + cluster.clusterName}

			if cluster.clusterType == "hub" {
				expectedArgs = append(expectedArgs, "--disable-spec-sync=true")
			}

			checkArgs(cluster, expectedArgs...)

			By(logPrefix + "removing the framework deployment when the ManagedClusterAddOn CR is removed")
			Kubectl("delete", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR)
			deploy = GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case1DeploymentName, addonNamespace, false, 30,
			)
			Expect(deploy).To(BeNil())
		}
	})

	It("should create the default framework deployment in hosted mode", Label("hosted-mode"), func() {
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

			By(logPrefix + "deploying the default framework ManagedClusterAddOn in hosted mode")
			addon := unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "addon.open-cluster-management.io/v1alpha1",
				"kind":       "ManagedClusterAddOn",
				"metadata": map[string]interface{}{
					"name": "governance-policy-framework",
					"annotations": map[string]interface{}{
						"addon.open-cluster-management.io/hosting-cluster-name": hubClusterConfig.clusterName,
					},
				},
				"spec": map[string]interface{}{
					"installNamespace": installNamespace,
				},
			}}
			_, err := hubClient.Resource(gvrManagedClusterAddOn).Namespace(cluster.clusterName).Create(
				context.TODO(), &addon, metav1.CreateOptions{},
			)
			Expect(err).To(BeNil())

			checkContainersAndAvailability(cluster, i+1)

			checkArgs(
				cluster,
				"--cluster-namespace="+installNamespace,
				"--cluster-namespace-on-hub="+cluster.clusterName,
			)

			By(logPrefix + "removing the framework deployment when the ManagedClusterAddOn CR is removed")
			err = hubClient.Resource(gvrManagedClusterAddOn).Namespace(cluster.clusterName).Delete(
				context.TODO(), addon.GetName(), metav1.DeleteOptions{},
			)
			Expect(err).To(BeNil())

			deploy := GetWithTimeout(hubClient, gvrDeployment, case1DeploymentName, installNamespace, false, 30)
			Expect(deploy).To(BeNil())

			namespace := GetWithTimeout(hubClient, gvrNamespace, installNamespace, "", false, 30)
			Expect(namespace).To(BeNil())
		}
	})

	It("should create a framework deployment with custom logging levels", func() {
		for i, cluster := range managedClusterList {
			logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "
			By(logPrefix + "deploying the default framework managedclusteraddon")
			if cluster.clusterType == "hub" {
				Kubectl("apply", "-n", cluster.clusterName, "-f", case1hubAnnotationMCAOCR)
			} else {
				Kubectl("apply", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR)
			}

			deploy := GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case1DeploymentName, addonNamespace, true, 60,
			)
			Expect(deploy).NotTo(BeNil())

			checkContainersAndAvailability(cluster, i)

			By(logPrefix + "annotating the managedclusteraddon with the " + loggingLevelAnnotation + " annotation")
			Kubectl("annotate", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR, loggingLevelAnnotation)

			checkArgs(cluster, "--log-encoder=console", "--log-level=8", "--v=6")

			By(logPrefix + "removing the framework deployment when the ManagedClusterAddOn CR is removed")
			Kubectl("delete", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR)
			deploy = GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case1DeploymentName, addonNamespace, false, 30,
			)
			Expect(deploy).To(BeNil())
		}
	})

	It("should use the onManagedClusterHub value set in helm values annotation", func() {
		cluster := managedClusterList[0]
		Expect(cluster.clusterType).To(Equal("hub"))

		logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "

		By(logPrefix + "deploying the annotated framework managedclusteraddon")
		Kubectl("apply", "-n", cluster.clusterName, "-f", case1hubValuesMCAOCR)
		deploy := GetWithTimeout(
			cluster.clusterClient, gvrDeployment, case1DeploymentName, addonNamespace, true, 60,
		)
		Expect(deploy).NotTo(BeNil())

		checkContainersAndAvailability(cluster, 0)

		// Adding this annotation and later verifying the cluster namespace is not removed checks
		// that the helm values annotation and the logging level annotation are stackable.
		By(logPrefix + "annotating the managedclusteraddon with the " + loggingLevelAnnotation + " annotation")
		Kubectl("annotate", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR, loggingLevelAnnotation)

		checkArgs(cluster, "--log-encoder=console", "--log-level=8", "--v=6")

		By(logPrefix + "deleting the managedclusteraddon")
		Kubectl("delete", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR)
		deploy = GetWithTimeout(
			cluster.clusterClient, gvrDeployment, case1DeploymentName, addonNamespace, false, 30,
		)
		Expect(deploy).To(BeNil())

		By(logPrefix + "checking the managed cluster namespace is not deleted after addon removed")
		Consistently(func() *unstructured.Unstructured {
			return GetWithTimeoutClusterResource(cluster.clusterClient, gvrNamespace, cluster.clusterName, true, 15)
		}, 30, 5).Should(Not(BeNil()))
	})

	It("should use the onManagedClusterHub value set in the custom annotation", func() {
		cluster := managedClusterList[0]
		Expect(cluster.clusterType).To(Equal("hub"))

		logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "

		By(logPrefix + "deploying the annotated framework managedclusteraddon")
		Kubectl("apply", "-n", cluster.clusterName, "-f", case1hubAnnotationMCAOCR)
		deploy := GetWithTimeout(
			cluster.clusterClient, gvrDeployment, case1DeploymentName, addonNamespace, true, 60,
		)
		Expect(deploy).NotTo(BeNil())

		checkContainersAndAvailability(cluster, 0)

		// Adding this annotation and later verifying the cluster namespace is not removed checks
		// that the multiclusterhub annotation and the logging level annotation are stackable.
		By(logPrefix + "annotating the managedclusteraddon with the " + loggingLevelAnnotation + " annotation")
		Kubectl("annotate", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR, loggingLevelAnnotation)

		checkArgs(cluster, "--log-encoder=console", "--log-level=8", "--v=6")

		By(logPrefix + "deleting the managedclusteraddon")
		Kubectl("delete", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR)
		deploy = GetWithTimeout(
			cluster.clusterClient, gvrDeployment, case1DeploymentName, addonNamespace, false, 30,
		)
		Expect(deploy).To(BeNil())

		By(logPrefix + "checking the managed cluster namespace is not deleted after addon removed")
		Consistently(func() *unstructured.Unstructured {
			return GetWithTimeoutClusterResource(cluster.clusterClient, gvrNamespace, cluster.clusterName, true, 15)
		}, 30, 5).Should(Not(BeNil()))
	})

	It("should revert edits to the ManifestWork by default", func() {
		for _, cluster := range managedClusterList {
			logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "
			By(logPrefix + "deploying the default framework managedclusteraddon")
			if cluster.clusterType == "hub" {
				Kubectl("apply", "-n", cluster.clusterName, "-f", case1hubAnnotationMCAOCR)
			} else {
				Kubectl("apply", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR)
			}
			deploy := GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case1DeploymentName, addonNamespace, true, 60,
			)
			Expect(deploy).NotTo(BeNil())

			By(logPrefix + "getting the default number of items in the ManifestWork")
			defaultLength := 0
			Eventually(func() int {
				mw := GetWithTimeout(clientDynamic, gvrManifestWork, case1MWName, cluster.clusterName, true, 15)
				manifests, _, _ := unstructured.NestedSlice(mw.Object, "spec", "workload", "manifests")
				defaultLength = len(manifests)

				return defaultLength
			}, 60, 5).ShouldNot(Equal(0))

			By(logPrefix + "patching the ManifestWork to add an item")
			Kubectl("patch", "-n", cluster.clusterName, "manifestwork", case1MWName, "--type=json",
				"--patch-file="+case1MWPatch)

			By(logPrefix + "verifying the edit is reverted")
			Eventually(func() int {
				mw := GetWithTimeout(clientDynamic, gvrManifestWork, case1MWName, cluster.clusterName, true, 15)
				manifests, _, _ := unstructured.NestedSlice(mw.Object, "spec", "workload", "manifests")

				return len(manifests)
			}, 60, 5).Should(Equal(defaultLength))

			By(logPrefix + "deleting the managedclusteraddon")
			Kubectl("delete", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR)
			deploy = GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case1DeploymentName, addonNamespace, false, 30,
			)
			Expect(deploy).To(BeNil())
		}
	})

	It("should preserve edits to the ManifestWork if paused by annotation", func() {
		for _, cluster := range managedClusterList {
			logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "
			By(logPrefix + "deploying the default framework managedclusteraddon")
			if cluster.clusterType == "hub" {
				Kubectl("apply", "-n", cluster.clusterName, "-f", case1hubAnnotationMCAOCR)
			} else {
				Kubectl("apply", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR)
			}
			deploy := GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case1DeploymentName, addonNamespace, true, 60,
			)
			Expect(deploy).NotTo(BeNil())

			By(logPrefix + "annotating the managedclusteraddon with the pause annotation")
			Kubectl("annotate", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR, "policy-addon-pause=true")

			By(logPrefix + "getting the default number of items in the ManifestWork")
			defaultLength := 0
			Eventually(func() int {
				mw := GetWithTimeout(clientDynamic, gvrManifestWork, case1MWName, cluster.clusterName, true, 15)
				manifests, _, _ := unstructured.NestedSlice(mw.Object, "spec", "workload", "manifests")
				defaultLength = len(manifests)

				return defaultLength
			}, 60, 5).ShouldNot(Equal(0))

			By(logPrefix + "patching the ManifestWork to add an item")
			Kubectl("patch", "-n", cluster.clusterName, "manifestwork", case1MWName, "--type=json",
				"--patch-file="+case1MWPatch)

			By(logPrefix + "verifying the edit is not reverted")
			Consistently(func() int {
				mw := GetWithTimeout(clientDynamic, gvrManifestWork, case1MWName, cluster.clusterName, true, 15)
				manifests, _, _ := unstructured.NestedSlice(mw.Object, "spec", "workload", "manifests")

				return len(manifests)
			}, 30, 5).Should(Equal(defaultLength + 1))

			By(logPrefix + "deleting the managedclusteraddon")
			Kubectl("delete", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR)
			deploy = GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case1DeploymentName, addonNamespace, false, 30,
			)
			Expect(deploy).To(BeNil())
		}
	})

	It("should manage the cluster namespace on managed clusters", func() {
		for _, cluster := range managedClusterList[1:] {
			Expect(cluster.clusterType).To(Equal("managed"))

			logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "
			By(logPrefix + "checking the managed cluster namespace does not exist before addon created")
			GetWithTimeoutClusterResource(cluster.clusterClient, gvrNamespace, cluster.clusterName, false, 15)

			By(logPrefix + "deploying the default framework managedclusteraddon")
			Kubectl("apply", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR)
			deploy := GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case1DeploymentName, addonNamespace, true, 60,
			)
			Expect(deploy).NotTo(BeNil())

			By(logPrefix + "checking the managed cluster namespace exists after addon created")
			GetWithTimeoutClusterResource(cluster.clusterClient, gvrNamespace, cluster.clusterName, true, 15)

			By(logPrefix + "deleting the managedclusteraddon")
			Kubectl("delete", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR)
			deploy = GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case1DeploymentName, addonNamespace, false, 30,
			)
			Expect(deploy).To(BeNil())

			By(logPrefix + "checking the managed cluster namespace is deleted after addon removed")
			GetWithTimeoutClusterResource(cluster.clusterClient, gvrNamespace, cluster.clusterName, false, 15)
		}
	})

	It("should deploy with startupProbes or initialDelaySeconds depending on version", func() {
		for i, cluster := range managedClusterList[1:] {
			Expect(cluster.clusterType).To(Equal("managed"))

			logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "
			By(logPrefix + "deploying the default framework managedclusteraddon")
			Kubectl("apply", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR)
			deploy := GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case1DeploymentName, addonNamespace, true, 60,
			)
			Expect(deploy).NotTo(BeNil())

			containers, found, err := unstructured.NestedSlice(deploy.Object, "spec", "template", "spec", "containers")
			Expect(err).To(BeNil())
			Expect(found).To(BeTrue())

			container, ok := containers[0].(map[string]interface{})
			Expect(ok).To(BeTrue())

			if startupProbeInCluster(i) {
				By(logPrefix + "checking for startupProbe on kubernetes 1.20 or higher")
				_, found, err = unstructured.NestedMap(container, "startupProbe")
				Expect(err).To(BeNil())
				Expect(found).To(BeTrue())
			} else {
				By(logPrefix + "checking for initialDelaySeconds on kubernetes 1.19 or lower")
				_, found, err = unstructured.NestedInt64(container, "livenessProbe", "initialDelaySeconds")
				Expect(err).To(BeNil())
				Expect(found).To(BeTrue())
			}

			By(logPrefix + "deleting the managedclusteraddon")
			Kubectl("delete", "-n", cluster.clusterName, "-f", case1ManagedClusterAddOnCR)
			deploy = GetWithTimeout(
				cluster.clusterClient, gvrDeployment, case1DeploymentName, addonNamespace, false, 30,
			)
			Expect(deploy).To(BeNil())
		}
	})
})

func checkContainersAndAvailability(cluster managedClusterConfig, clusterIdx int) {
	logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "
	client := cluster.clusterClient
	namespace := addonNamespace

	if cluster.hostedOnHub {
		client = managedClusterList[0].clusterClient
		namespace = fmt.Sprintf("%s-hosted", cluster.clusterName)
	}

	if startupProbeInCluster(clusterIdx) {
		By(logPrefix + "verifying all replicas in framework deployment are available")
		Eventually(func() bool {
			deploy := GetWithTimeout(
				client, gvrDeployment, case1DeploymentName, namespace, true, 60,
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

	By(logPrefix + "verifying one framework pod is running")
	Eventually(func() bool {
		opts := metav1.ListOptions{
			LabelSelector: case1PodSelector,
		}
		pods := ListWithTimeoutByNamespace(client, gvrPod, opts, namespace, 1, true, 30)

		phase, _, _ := unstructured.NestedString(pods.Items[0].Object, "status", "phase")

		return phase == "Running"
	}, 60, 1).Should(Equal(true))

	By(logPrefix + "showing the framework managedclusteraddon as available")
	Eventually(func() bool {
		addon := GetWithTimeout(
			clientDynamic, gvrManagedClusterAddOn, case1DeploymentName, cluster.clusterName, true, 30,
		)

		return getAddonStatus(addon)
	}, 240, 1).Should(Equal(true))
}

func checkArgs(cluster managedClusterConfig, desiredArgs ...string) {
	logPrefix := cluster.clusterType + " " + cluster.clusterName + ": "

	client := cluster.clusterClient
	namespace := addonNamespace

	if cluster.hostedOnHub {
		client = managedClusterList[0].clusterClient
		namespace = fmt.Sprintf("%s-hosted", cluster.clusterName)
	}

	By(logPrefix + "verifying one framework pod is running and has the desired args")
	Eventually(func(g Gomega) error {
		opts := metav1.ListOptions{
			LabelSelector: case1PodSelector,
		}
		pods := ListWithTimeoutByNamespace(client, gvrPod, opts, namespace, 1, true, 30)
		podObj := pods.Items[0].Object

		phase, found, err := unstructured.NestedString(podObj, "status", "phase")
		if err != nil || !found || phase != "Running" {
			return fmt.Errorf("pod phase is not running; found=%v; err=%w", found, err)
		}

		containerList, found, err := unstructured.NestedSlice(podObj, "spec", "containers")
		if err != nil || !found {
			return fmt.Errorf("could not get container list; found=%v; err=%w", found, err)
		}

		if len(containerList) != 1 {
			return fmt.Errorf("the container list had more than 1 entry; containerList=%v", containerList)
		}

		container := containerList[0]

		containerObj, ok := container.(map[string]interface{})
		if !ok {
			return fmt.Errorf("could not convert container to map; container=%v", container)
		}

		argList, found, err := unstructured.NestedStringSlice(containerObj, "args")
		if err != nil || !found {
			return fmt.Errorf("could not get container args; found=%v; err=%w", found, err)
		}

		g.Expect(argList).To(ContainElements(desiredArgs))

		return nil
	}, 120, 1).Should(BeNil())
}

func startupProbeInCluster(clusterIdx int) bool {
	versionJSON := Kubectl(
		"version",
		"-o=json",
		fmt.Sprintf("--kubeconfig=%s%d.kubeconfig", kubeconfigFilename, clusterIdx+1),
	)

	version := struct {
		ServerVersion struct {
			Minor int `json:"minor,string"`
		} `json:"serverVersion"`
	}{}

	if err := json.Unmarshal([]byte(versionJSON), &version); err != nil {
		// Deliberately panic and fail if the output didn't match what we expected
		p := fmt.Sprintf("error: %v, versionJSON: %v", err, versionJSON)
		panic(p)
	}

	return version.ServerVersion.Minor >= 20
}
