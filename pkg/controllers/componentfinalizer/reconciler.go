package componentfinalizer

import (
	"context"

	k8sdepwatches "github.com/stolostron/kubernetes-dependency-watches/client"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
)

type Reconciler struct {
	DynamicWatcher                k8sdepwatches.DynamicWatcher
	DynamicClient                 *dynamic.DynamicClient
	ManagedClusterAddOnNames      []string
	InternalHubComponentNamespace string
}

const policyFinalizer = "policy.open-cluster-management.io/mcao-cleanup"

var (
	ihcGVK = schema.GroupVersionKind{
		Group:   "operator.open-cluster-management.io",
		Version: "v1",
		Kind:    "InternalHubComponent",
	}
	ihcGVR = schema.GroupVersionResource{
		Group:    "operator.open-cluster-management.io",
		Version:  "v1",
		Resource: "internalhubcomponents",
	}
)

func (r *Reconciler) Reconcile(
	ctx context.Context, ihc k8sdepwatches.ObjectIdentifier,
) (ctrl.Result, error) {
	invalid := ihc.Group != "operator.open-cluster-management.io" ||
		ihc.Kind != "InternalHubComponent" ||
		ihc.Name != "grc"

	if invalid {
		klog.Infof("Invalid input to ComponentFinalizerReconciler: %v\n", ihc)

		return ctrl.Result{}, nil
	}

	ihcUnstruct, err := r.DynamicWatcher.GetFromCache(ihcGVK, r.InternalHubComponentNamespace, "grc")
	if err != nil {
		return ctrl.Result{}, err
	}

	if ihcUnstruct == nil {
		return ctrl.Result{}, nil
	}

	finalizers := ihcUnstruct.GetFinalizers()

	finalizerIdx := -1

	for i, finalizer := range finalizers {
		if finalizer == policyFinalizer {
			finalizerIdx = i

			break
		}
	}

	// Note: this actually includes the IHC, but it will basically be ignored
	mcaos, err := r.DynamicWatcher.ListWatchedFromCache(ihc)
	if err != nil {
		return ctrl.Result{}, err
	}

	anyMatchedMCAOs := false

mcaoLoop:
	for _, mcao := range mcaos {
		for _, name := range r.ManagedClusterAddOnNames {
			if name == mcao.GetName() {
				anyMatchedMCAOs = true

				break mcaoLoop
			}
		}
	}

	ihcClient := r.DynamicClient.Resource(ihcGVR).Namespace(r.InternalHubComponentNamespace)

	if anyMatchedMCAOs && finalizerIdx == -1 {
		// Add the finalizer
		ihcUnstruct.SetFinalizers(append(finalizers, policyFinalizer))

		if _, err := ihcClient.Update(ctx, ihcUnstruct, v1.UpdateOptions{}); err != nil {
			return ctrl.Result{}, err
		}
	} else if !anyMatchedMCAOs && finalizerIdx != -1 {
		// Remove the finalizer because there are no more matched MCAOs
		ihcUnstruct.SetFinalizers(append(finalizers[:finalizerIdx], finalizers[finalizerIdx+1:]...))

		if _, err := ihcClient.Update(ctx, ihcUnstruct, v1.UpdateOptions{}); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *Reconciler) WatchResources() error {
	ihc := k8sdepwatches.ObjectIdentifier{
		Group:     "operator.open-cluster-management.io",
		Version:   "v1",
		Kind:      "InternalHubComponent",
		Namespace: r.InternalHubComponentNamespace, // usually 'open-cluster-management'
		Name:      "grc",
	}

	toWatch := make([]k8sdepwatches.ObjectIdentifier, 0, len(r.ManagedClusterAddOnNames)+1)

	toWatch = append(toWatch, ihc)

	for _, name := range r.ManagedClusterAddOnNames {
		toWatch = append(toWatch, k8sdepwatches.ObjectIdentifier{
			Group:     "addon.open-cluster-management.io",
			Version:   "v1alpha1",
			Kind:      "ManagedClusterAddOn",
			Namespace: "", // All namespaces
			Name:      name,
			Selector:  labels.Everything().String(),
		})
	}

	return r.DynamicWatcher.AddOrUpdateWatcher(ihc, toWatch...)
}
