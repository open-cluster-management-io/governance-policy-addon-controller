// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// Kubectl executes kubectl commands
func Kubectl(args ...string) string {
	// Inject the kubeconfig to ensure we're pointing to the hub if none is provided
	skipKubeconfig := false

	for _, arg := range args {
		if strings.HasPrefix(arg, "--kubeconfig=") {
			skipKubeconfig = true

			break
		}
	}

	if !skipKubeconfig {
		args = append(args, "--kubeconfig="+kubeconfigFilename+"1_e2e")
	}

	cmd := exec.Command("kubectl", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		// in case of failure, print command output (including error)
		GinkgoWriter.Printf("output\n======\n%s\n", stdout.String())
		GinkgoWriter.Printf("error\n======\n%s\n", stderr.String())
		Fail(fmt.Sprintf("Error: %v", err), 1)
	}

	return stdout.String()
}

// GetWithTimeout keeps polling to get the namespaced object for timeout seconds until wantFound is
// met (true for found, false for not found)
func GetWithTimeout(
	ctx context.Context,
	client dynamic.Interface,
	gvr schema.GroupVersionResource,
	name, namespace string,
	wantFound bool,
	timeout int,
) *unstructured.Unstructured {
	GinkgoHelper()

	if timeout < 1 {
		timeout = 1
	}
	var obj *unstructured.Unstructured

	Eventually(func() error {
		var err error
		namespace := client.Resource(gvr).Namespace(namespace)
		obj, err = namespace.Get(ctx, name, metav1.GetOptions{})
		if wantFound && err != nil {
			return err
		}
		if !wantFound && err == nil {
			return fmt.Errorf("expected to return IsNotFound error")
		}
		if !wantFound && err != nil && !errors.IsNotFound(err) {
			return err
		}

		return nil
	}, timeout, 1).ShouldNot(HaveOccurred())

	if wantFound {
		return obj
	}

	return nil
}

// GetWithTimeoutClusterResource keeps polling to get the cluster-scoped object for timeout seconds
// until wantFound is met (true for found, false for not found)
func GetWithTimeoutClusterResource(
	ctx context.Context,
	client dynamic.Interface,
	gvr schema.GroupVersionResource,
	name string,
	wantFound bool,
	timeout int,
) *unstructured.Unstructured {
	GinkgoHelper()

	if timeout < 1 {
		timeout = 1
	}
	var obj *unstructured.Unstructured

	Eventually(func() error {
		var err error
		res := client.Resource(gvr)
		obj, err = res.Get(ctx, name, metav1.GetOptions{})
		if wantFound && err != nil {
			return err
		}
		if !wantFound && err == nil {
			return fmt.Errorf("expected to return IsNotFound error")
		}
		if !wantFound && err != nil && !errors.IsNotFound(err) {
			return err
		}

		return nil
	}, timeout, 1).ShouldNot(HaveOccurred())

	if wantFound {
		return obj
	}

	return nil
}

// ListWithTimeoutByNamespace keeps polling to list the object for timeout seconds until wantFound is met
// (true for found, false for not found)
func ListWithTimeoutByNamespace(
	ctx context.Context,
	clientHubDynamic dynamic.Interface,
	gvr schema.GroupVersionResource,
	opts metav1.ListOptions,
	ns string,
	size int,
	wantFound bool,
	timeout int,
) *unstructured.UnstructuredList {
	GinkgoHelper()

	if timeout < 1 {
		timeout = 1
	}

	var list *unstructured.UnstructuredList

	Eventually(func(g Gomega) {
		var err error
		list, err = clientHubDynamic.Resource(gvr).Namespace(ns).List(ctx, opts)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(list.Items).To(HaveLen(size))
	}, timeout, 1).Should(Succeed())

	if wantFound {
		return list
	}

	return nil
}

func getAddonStatus(addon *unstructured.Unstructured) bool {
	conditions, found, err := unstructured.NestedSlice(addon.Object, "status", "conditions")
	if err != nil {
		panic(err)
	}

	if !found {
		return false
	}

	for _, item := range conditions {
		if condition, ok := item.(map[string]interface{}); !ok {
			panic(fmt.Errorf("failed to parse .status.condition[]: %+v", item))
		} else if condition["type"] == "Available" {
			return condition["status"] == "True"
		}
	}

	return false
}

func debugCollection(podSelector string) {
	namespaceSuffix := []string{""}

	if slices.Contains(CurrentSpecReport().Labels(), "hosted-mode") {
		namespaceSuffix = append(namespaceSuffix, "-hosted")
	}

	By("Recording debug logs")

	output := "===\n"

	for i, cluster := range managedClusterList {
		targetKubeconfig := fmt.Sprintf("--kubeconfig=%s%d_e2e", kubeconfigFilename, i+1)
		targetCluster := cluster.clusterName
		clusterNs := []string{cluster.clusterName}

		if cluster.clusterName == "cluster1" {
			for _, cluster := range managedClusterList[1:] {
				clusterNs = append(clusterNs, cluster.clusterName)
			}

			output += "::group::Cluster cluster1: Addon objects"
			output += Kubectl("get", "clustermanagementaddons", "-o=yaml", targetKubeconfig)
			output += "---\n"
			output += Kubectl("get", "managedclusteraddons", "-A", "-o=yaml", targetKubeconfig)
			output += "---\n"
			output += Kubectl("get", "addondeploymentconfigs", "-A", "-o=yaml", targetKubeconfig)
			output += "---\n"
			output += Kubectl("get", "manifestwork", "-A", "-o=yaml", targetKubeconfig)
			output += "::endgroup::\n"
		}

		for _, namespace := range clusterNs {
			for _, suffix := range namespaceSuffix {
				namespace += suffix
				output += fmt.Sprintf("::group::Cluster %s: All objects in namespace %s:\n", targetCluster, namespace)
				output += Kubectl("get", "all", "-n", namespace, targetKubeconfig)
				output += "::endgroup::\n"
				output += fmt.Sprintf(
					"::group::Cluster %s: Pod logs for label %s in namespace %s:\n",
					targetCluster, podSelector, namespace,
				)
				output += Kubectl("describe", "pod", "-n", namespace, "-l", podSelector, targetKubeconfig)
				output += Kubectl("logs", "-n", namespace, "-l", podSelector, "--ignore-errors", targetKubeconfig)
				output += "::endgroup::\n"
			}
		}

		output += fmt.Sprintf("::group::Cluster %s: All objects in namespace %s:\n", targetCluster, addonNamespace)
		output += Kubectl("get", "all", "-n", addonNamespace, targetKubeconfig)
		output += "::endgroup::\n"
		output += fmt.Sprintf("::group::Cluster %s: Pod logs for label %s in namespace %s for cluster %s:\n",
			targetCluster, podSelector, addonNamespace, cluster.clusterName)
		output += Kubectl(
			"describe", "pod", "-n", addonNamespace, "-l", podSelector, targetKubeconfig,
		)
		output += Kubectl(
			"logs", "-n", addonNamespace, "-l", podSelector, "--ignore-errors", targetKubeconfig,
		)
		output += "::endgroup::\n"
	}

	GinkgoWriter.Print(output)
}
