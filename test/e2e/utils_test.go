// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
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
	client dynamic.Interface,
	gvr schema.GroupVersionResource,
	name, namespace string,
	wantFound bool,
	timeout int,
) *unstructured.Unstructured {
	if timeout < 1 {
		timeout = 1
	}
	var obj *unstructured.Unstructured

	EventuallyWithOffset(1, func() error {
		var err error
		namespace := client.Resource(gvr).Namespace(namespace)
		obj, err = namespace.Get(context.TODO(), name, metav1.GetOptions{})
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
	client dynamic.Interface,
	gvr schema.GroupVersionResource,
	name string,
	wantFound bool,
	timeout int,
) *unstructured.Unstructured {
	if timeout < 1 {
		timeout = 1
	}
	var obj *unstructured.Unstructured

	EventuallyWithOffset(1, func() error {
		var err error
		res := client.Resource(gvr)
		obj, err = res.Get(context.TODO(), name, metav1.GetOptions{})
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
	clientHubDynamic dynamic.Interface,
	gvr schema.GroupVersionResource,
	opts metav1.ListOptions,
	ns string,
	size int,
	wantFound bool,
	timeout int,
) *unstructured.UnstructuredList {
	if timeout < 1 {
		timeout = 1
	}

	var list *unstructured.UnstructuredList

	EventuallyWithOffset(1, func() error {
		var err error
		list, err = clientHubDynamic.Resource(gvr).Namespace(ns).List(context.TODO(), opts)
		if err != nil {
			return err
		}

		if len(list.Items) != size {
			return fmt.Errorf("list size doesn't match, expected %d actual %d", size, len(list.Items))
		}

		return nil
	}, timeout, 1).ShouldNot(HaveOccurred())

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
