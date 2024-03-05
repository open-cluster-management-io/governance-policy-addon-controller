/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	k8sdepwatches "github.com/stolostron/kubernetes-dependency-watches/client"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	utilflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"

	"open-cluster-management.io/governance-policy-addon-controller/pkg/addon/configpolicy"
	"open-cluster-management.io/governance-policy-addon-controller/pkg/addon/policyframework"
	"open-cluster-management.io/governance-policy-addon-controller/pkg/controllers/complianceapi"
)

//+kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=get;create
//+kubebuilder:rbac:groups=certificates.k8s.io,resources=certificatesigningrequests;certificatesigningrequests/approval,verbs=get;list;watch;create;update
//+kubebuilder:rbac:groups=certificates.k8s.io,resources=signers,verbs=approve
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=clustermanagementaddons,verbs=get;list;watch

// RBAC below will need to be updated if/when new policy controllers are added.

//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=create
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;patch;update,resourceNames=governance-policy-framework;config-policy-controller

//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=create
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;update;patch;delete,resourceNames="open-cluster-management:policy-framework-hub";"open-cluster-management:config-policy-controller-hub";"open-cluster-management:compliance-history-api-recorder"
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=create
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;update;patch;delete,resourceNames="open-cluster-management:policy-framework-hub";"open-cluster-management:config-policy-controller-hub";"open-cluster-management:compliance-history-api-recorder"
//+kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=create
//+kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;update;patch;delete,resourceNames="open-cluster-management-compliance-history-api-recorder"
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=create
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;update;patch;delete;watch;list,resourceNames="open-cluster-management-compliance-history-api-recorder"

// Cannot limit based on resourceNames because the name is dynamic in hosted mode.
//+kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworks,verbs=create;delete;get;list;patch;update;watch

//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons,verbs=create
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons,verbs=get;list;watch;update
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons,verbs=delete,resourceNames=config-policy-controller;governance-policy-framework
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons/finalizers,verbs=update,resourceNames=config-policy-controller;governance-policy-framework
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons/status,verbs=update;patch,resourceNames=config-policy-controller;governance-policy-framework
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=clustermanagementaddons/status,verbs=update;patch,resourceNames=config-policy-controller;governance-policy-framework

//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=clustermanagementaddons/finalizers,verbs=update,resourceNames=config-policy-controller;governance-policy-framework
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=addondeploymentconfigs,verbs=get;list;watch

// Permissions required for policy-framework
// (see https://kubernetes.io/docs/reference/access-authn-authz/rbac/#privilege-escalation-prevention-and-bootstrapping)

//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=policies,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=policies/finalizers,verbs=update
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=policies/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=core,resources=secrets,resourceNames=policy-encryption-key;governance-policy-database,verbs=get;list;watch
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=clusterclaims,resourceNames=id.k8s.io,verbs=get
//+kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=create
//+kubebuilder:rbac:groups=route.openshift.io,resources=routes,resourceNames=governance-history-api,verbs=get;list;watch;update;delete
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;get;list;patch;update;watch
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups=config.openshift.io,resources=infrastructures,verbs=get;list;watch

var ctrlVersion = version.Info{}

const (
	ctrlName = "governance-policy-addon-controller"
)

func main() {
	pflag.CommandLine.SetNormalizeFunc(utilflag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)

	logs.InitLogs()
	defer logs.FlushLogs()

	cmd := &cobra.Command{
		Use:   ctrlName,
		Short: "Governance policy addon controller for Open Cluster Management",
		Run: func(cmd *cobra.Command, args []string) {
			if err := cmd.Help(); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
			}
			os.Exit(1)
		},
	}

	ctrlconfig := controllercmd.NewControllerCommandConfig(ctrlName, ctrlVersion, runController)
	ctrlconfig.DisableServing = true

	ctrlcmd := ctrlconfig.NewCommandWithContext(context.TODO())
	ctrlcmd.Use = "controller"
	ctrlcmd.Short = "Start the addon controller"

	cmd.AddCommand(ctrlcmd)

	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func runController(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	mgr, err := addonmanager.New(controllerContext.KubeConfig)
	if err != nil {
		klog.Error(err, "unable to create new addon manager")
		os.Exit(1)
	}

	agentFuncs := []func(context.Context, addonmanager.AddonManager, *controllercmd.ControllerContext) error{
		policyframework.GetAndAddAgent,
		configpolicy.GetAndAddAgent,
	}

	wg := sync.WaitGroup{}

	runSecretReconciler, err := isOpenShift(controllerContext.KubeConfig)
	if err != nil {
		klog.Error(err, "Failed to detect if this is running on OpenShift. Assuming it's not.")
	}

	// The Compliance API DB Secret reconciler is responsible for creating/deleting OpenShift routes for the compliance
	// history API. If it's not OpenShift, then don't run this.
	if runSecretReconciler {
		klog.Info("Starting the compliance events database secret reconciler")

		controllerNamespace := getControllerNamespace()

		dynamicClient := dynamic.NewForConfigOrDie(controllerContext.KubeConfig)
		reconciler := complianceapi.ComplianceDBSecretReconciler{DynamicClient: dynamicClient}

		dynamicWatcher, err := k8sdepwatches.New(
			controllerContext.KubeConfig, &reconciler, &k8sdepwatches.Options{
				EnableCache: true,
				ObjectCacheOptions: k8sdepwatches.ObjectCacheOptions{
					// Cache the GVKToGVR for 24 hours since we are using it for stable things like determining if
					// this is an OpenShift cluster by seeing if the cluster has a Route CRD or querying for a Secret.
					GVKToGVRCacheTTL:           24 * time.Hour,
					MissingAPIResourceCacheTTL: 24 * time.Hour,
				},
			},
		)
		if err != nil {
			klog.Error(
				err, "Failed to instantiate the dynamic watcher for the compliance events database secret reconciler",
			)
			os.Exit(1)
		}

		reconciler.DynamicWatcher = dynamicWatcher

		wg.Add(1)

		go func() {
			err := dynamicWatcher.Start(ctx)
			if err != nil {
				klog.Error(
					err, "Unable to start the dynamic watcher for the compliance events database secret reconciler",
				)
				os.Exit(1)
			}

			wg.Done()
		}()

		klog.Info("Waiting for the dynamic watcher to start")
		<-dynamicWatcher.Started()

		watcherSecret := k8sdepwatches.ObjectIdentifier{
			Version:   "v1",
			Kind:      "Secret",
			Namespace: controllerNamespace,
			Name:      complianceapi.DBSecretName,
		}
		if err := dynamicWatcher.AddWatcher(watcherSecret, watcherSecret); err != nil {
			klog.Error(err, "Unable to start the compliance events database secret watcher")
			os.Exit(1)
		}

		route := k8sdepwatches.ObjectIdentifier{
			Group:     "route.openshift.io",
			Kind:      "Route",
			Version:   "v1",
			Namespace: controllerNamespace,
			Name:      complianceapi.RouteName,
		}
		if err := dynamicWatcher.AddWatcher(watcherSecret, route); err != nil {
			klog.Error(err, "Unable to start the compliance events database secret watcher")
			os.Exit(1)
		}
	} else {
		klog.Info("Not running on OpenShift so not starting the compliance events database secret reconciler")
	}

	for _, f := range agentFuncs {
		err := f(ctx, mgr, controllerContext)
		if err != nil {
			klog.Error(err, "unable to get or add agent addon")
			os.Exit(1)
		}
	}

	wg.Add(1)

	go func() {
		err = mgr.Start(ctx)
		if err != nil {
			klog.Error(err, "problem starting manager")
			os.Exit(1)
		}

		wg.Done()
	}()

	wg.Wait()

	return nil
}

// getControllerNamespace returns the namespace the controller is running in. It defaults to open-cluster-management.
func getControllerNamespace() string {
	nsBytes, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return "open-cluster-management"
	}

	ns := strings.TrimSpace(string(nsBytes))

	return ns
}

func isOpenShift(kubeconfig *rest.Config) (bool, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(kubeconfig)
	if err != nil {
		return false, err
	}

	_, err = discoveryClient.ServerResourcesForGroupVersion(complianceapi.RouteGVR.GroupVersion().String())
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}
