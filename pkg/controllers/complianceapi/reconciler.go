package complianceapi

import (
	"context"
	"errors"

	k8sdepwatches "github.com/stolostron/kubernetes-dependency-watches/client"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	ServiceName string = "governance-policy-compliance-history-api"
	// The Route name needs to be relatively short since there is a 63 character limit on DNS names.
	RouteName    string = "governance-history-api"
	DBSecretName string = "governance-policy-database"
)

var RouteGVR = schema.GroupVersionResource{Group: "route.openshift.io", Version: "v1", Resource: "routes"}

type ComplianceDBSecretReconciler struct {
	DynamicWatcher k8sdepwatches.DynamicWatcher
	DynamicClient  *dynamic.DynamicClient
}

// Reconcile watches the governance-policy-database secret in the controller namespace. On changes it'll handle
// the Kubernetes objects to expose the compliance history API.
func (r *ComplianceDBSecretReconciler) Reconcile(
	ctx context.Context, watcher k8sdepwatches.ObjectIdentifier,
) (ctrl.Result, error) {
	// Everything that is watched is in the same namespace
	ns := watcher.Namespace

	secret, err := r.DynamicWatcher.GetFromCache(
		schema.GroupVersionKind{Version: "v1", Kind: "Secret"}, ns, DBSecretName,
	)
	if err != nil && !errors.Is(err, k8sdepwatches.ErrNoCacheEntry) {
		return ctrl.Result{}, nil
	}

	if secret == nil {
		klog.V(2).Infof(
			"The Secret %s is not present. Verifying that the Route of %s is deleted.", DBSecretName, RouteName,
		)

		err = r.DynamicClient.Resource(RouteGVR).Namespace(ns).Delete(ctx, RouteName, metav1.DeleteOptions{})
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return ctrl.Result{}, nil
			}

			klog.Errorf("Failed to delete the compliance history API Route of %s: %v", RouteName, err)

			return ctrl.Result{}, err
		}

		klog.Infof("Deleted the compliance history API Route of %s", RouteName)

		return ctrl.Result{}, nil
	}

	klog.V(2).Infof(
		"The Secret %s is present. Verifying that the compliance history API Route of %s is present.",
		DBSecretName, RouteName,
	)

	route := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "route.openshift.io/v1",
			"kind":       "Route",
			"metadata": map[string]interface{}{
				"name":      RouteName,
				"namespace": ns,
			},
			"spec": map[string]interface{}{
				"port": map[string]interface{}{
					"targetPort": "compliance-history-api",
				},
				"tls": map[string]interface{}{
					"insecureEdgeTerminationPolicy": "Redirect",
					"termination":                   "reencrypt",
				},
				"to": map[string]interface{}{
					"kind": "Service",
					"name": ServiceName,
				},
			},
		},
	}

	_, err = r.DynamicClient.Resource(RouteGVR).Namespace(ns).Create(ctx, route, metav1.CreateOptions{})
	if err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			klog.Errorf("Failed to create the compliance history API Route of %s: %v", RouteName, err)

			return ctrl.Result{}, err
		}

		klog.V(2).Infof("The compliance history API Route of %s already exists", RouteName)
	} else {
		klog.Infof("Created the compliance history API Route of %s", RouteName)
	}

	return ctrl.Result{}, nil
}
