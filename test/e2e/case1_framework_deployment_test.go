// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"
	"fmt"
	"os/exec"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

const (
	case1ManagedClusterAddOnCR   string = "../resources/framework_addon_cr.yaml"
	case1FrameworkDeploymentName string = "governance-policy-framework"
)

var _ = Describe("Test framework deployment", func() {
	It("should create the default framework deployment on the managed cluster", func() {
		Kubectl("apply", "-f", case1ManagedClusterAddOnCR)
		deploy := GetWithTimeout(clientDynamic, gvrDeployment, case1FrameworkDeploymentName, addonNamespace, true, 30)
		Expect(deploy).NotTo(BeNil())

		By("checking the number of containers in the deployment")
		Eventually(func() int {
			deploy := GetWithTimeout(clientDynamic, gvrDeployment, case1FrameworkDeploymentName, addonNamespace, true, 30)
			template := deploy.Object["spec"].(map[string]interface{})["template"].(map[string]interface{})
			containers := template["spec"].(map[string]interface{})["containers"].([]interface{})
			return len(containers)
		}, 60, 1).Should(Equal(3))
	})
	It("should remove the framework deployment when the ManagedClusterAddOn CR is removed", func() {
		Kubectl("delete", "-f", case1ManagedClusterAddOnCR)
		deploy := GetWithTimeout(clientDynamic, gvrDeployment, case1FrameworkDeploymentName, addonNamespace, false, 30)
		Expect(deploy).To(BeNil())
	})
	It("should deploy with 2 containers if onManagedClusterHub is set in helm values annotation", func() {
		By("deploying the default framework managedclusteraddon")
		Kubectl("apply", "-f", case1ManagedClusterAddOnCR)
		deploy := GetWithTimeout(clientDynamic, gvrDeployment, case1FrameworkDeploymentName, addonNamespace, true, 30)
		Expect(deploy).NotTo(BeNil())

		By("annotating the framework managedclusteraddon with helm values")
		Kubectl("annotate", "-f", case1ManagedClusterAddOnCR, "addon.open-cluster-management.io/values={\"onMulticlusterHub\":true}")

		Eventually(func() int {
			deploy := GetWithTimeout(clientDynamic, gvrDeployment, case1FrameworkDeploymentName, addonNamespace, true, 30)
			template := deploy.Object["spec"].(map[string]interface{})["template"].(map[string]interface{})
			containers := template["spec"].(map[string]interface{})["containers"].([]interface{})
			return len(containers)
		}, 60, 1).Should(Equal(2))
	})
})

// Kubectl executes kubectl commands
func Kubectl(args ...string) {
	cmd := exec.Command("kubectl", args...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		// in case of failure, print command output (including error)
		//nolint:forbidigo
		fmt.Printf("%s\n", output)
		Fail(fmt.Sprintf("Error: %v", err))
	}
}

// GetWithTimeout keeps polling to get the object for timeout seconds until wantFound is met
// (true for found, false for not found)
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

	Eventually(func() error {
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
	}, timeout, 1).Should(BeNil())

	if wantFound {
		return obj
	}

	return nil
}
