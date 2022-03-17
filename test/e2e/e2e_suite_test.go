// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	addonNamespace         string = "open-cluster-management-agent-addon"
	kubeconfigFilename     string = "../../policy-addon-ctrl"
	loggingLevelAnnotation string = "log-level=8"
)

var (
	gvrDeployment          schema.GroupVersionResource
	gvrPod                 schema.GroupVersionResource
	gvrManagedClusterAddOn schema.GroupVersionResource
	gvrManagedCluster      schema.GroupVersionResource
	gvrManifestWork        schema.GroupVersionResource
	managedClusterList     []managedClusterConfig
	clientDynamic          dynamic.Interface
)

type managedClusterConfig struct {
	clusterName   string
	clusterClient dynamic.Interface
	clusterType   string
}

func TestE2e(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "governance policy addon controller e2e Suite")
}

var _ = BeforeSuite(func() {
	gvrDeployment = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	gvrPod = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	gvrManagedClusterAddOn = schema.GroupVersionResource{
		Group: "addon.open-cluster-management.io", Version: "v1alpha1", Resource: "managedclusteraddons",
	}
	gvrManagedCluster = schema.GroupVersionResource{
		Group: "cluster.open-cluster-management.io", Version: "v1", Resource: "managedclusters",
	}
	gvrManifestWork = schema.GroupVersionResource{
		Group: "work.open-cluster-management.io", Version: "v1", Resource: "manifestworks",
	}
	clientDynamic = NewKubeClientDynamic("", kubeconfigFilename+"1.kubeconfig", "")
	managedClusterList = getManagedClusters(clientDynamic)
})

func getManagedClusters(client dynamic.Interface) []managedClusterConfig {
	clusterObjs, err := client.Resource(gvrManagedCluster).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err)
	}

	var clusters []managedClusterConfig

	for i, cluster := range clusterObjs.Items {
		clusterName, _, err := unstructured.NestedString(cluster.Object, "metadata", "name")
		if err != nil {
			panic(err)
		}

		clusterClient := NewKubeClientDynamic("", fmt.Sprintf("%s%d.kubeconfig", kubeconfigFilename, i+1), "")

		var clusterType string
		if i == 0 {
			clusterType = "hub"
		} else {
			clusterType = "managed"
		}

		newCluster := managedClusterConfig{
			clusterName,
			clusterClient,
			clusterType,
		}
		clusters = append(clusters, newCluster)
	}

	return clusters
}

func NewKubeClientDynamic(url, kubeconfig, context string) dynamic.Interface {
	config, err := LoadConfig(url, kubeconfig, context)
	if err != nil {
		panic(err)
	}

	clientset, err := dynamic.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	return clientset
}

func LoadConfig(url, kubeconfig, context string) (*rest.Config, error) {
	if kubeconfig == "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	}

	// If we have an explicit indication of where the kubernetes config lives, read that.
	if kubeconfig != "" {
		if context == "" {
			return clientcmd.BuildConfigFromFlags(url, kubeconfig)
		}

		return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig},
			&clientcmd.ConfigOverrides{
				CurrentContext: context,
			}).ClientConfig()
	}

	// If not, try the in-cluster config.
	if c, err := rest.InClusterConfig(); err == nil {
		return c, nil
	}

	// If no in-cluster config, try the default location in the user's home directory.
	if usr, err := user.Current(); err == nil {
		if c, err := clientcmd.BuildConfigFromFlags("", filepath.Join(usr.HomeDir, ".kube", "config")); err == nil {
			return c, nil
		}
	}

	return nil, fmt.Errorf("could not create a valid kubeconfig")
}
