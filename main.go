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
	"errors"
	"flag"
	"fmt"
	"os"
	"sync"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	k8sdepwatches "github.com/stolostron/kubernetes-dependency-watches/client"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/dynamic"
	utilflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"

	"open-cluster-management.io/governance-policy-addon-controller/pkg/addon/configpolicy"
	"open-cluster-management.io/governance-policy-addon-controller/pkg/addon/policyframework"
	"open-cluster-management.io/governance-policy-addon-controller/pkg/controllers/componentfinalizer"
)

//+kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=get;create
//+kubebuilder:rbac:groups=certificates.k8s.io,resources=certificatesigningrequests;certificatesigningrequests/approval,verbs=get;list;watch;create;update
//+kubebuilder:rbac:groups=certificates.k8s.io,resources=signers,verbs=approve
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=clustermanagementaddons,verbs=get;list;watch

//+kubebuilder:rbac:groups=operator.open-cluster-management.io,resources=internalhubcomponents,verbs=get;list;watch;update,resourceNames=grc
//+kubebuilder:rbac:groups=operator.open-cluster-management.io,resources=internalhubcomponents/finalizers,verbs=update,resourceNames=grc

// RBAC below will need to be updated if/when new policy controllers are added.

//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=create
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;patch;update,resourceNames=governance-policy-framework;config-policy-controller

//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=create
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;update;patch;delete,resourceNames="open-cluster-management:policy-framework-hub";"open-cluster-management:config-policy-controller-hub"
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=create
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;update;patch;delete,resourceNames="open-cluster-management:policy-framework-hub";"open-cluster-management:config-policy-controller-hub"

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
//+kubebuilder:rbac:groups=core,resources=secrets,resourceNames=policy-encryption-key,verbs=get;list;watch
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=clusterclaims,resourceNames=id.k8s.io,verbs=get
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

	ctrlconfig := controllercmd.NewControllerCommandConfig(ctrlName, ctrlVersion, runController, clock.RealClock{})
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
	wg := sync.WaitGroup{}

	reconciler := componentfinalizer.Reconciler{
		DynamicClient: dynamic.NewForConfigOrDie(controllerContext.KubeConfig),
		ManagedClusterAddOnNames: []string{
			"config-policy-controller",
			"governance-policy-framework",
		},
		InternalHubComponentNamespace: controllerContext.OperatorNamespace, // usually 'open-cluster-management'
	}

	dynamicWatcher, err := k8sdepwatches.New(
		controllerContext.KubeConfig, &reconciler, &k8sdepwatches.Options{EnableCache: true})
	if err != nil {
		klog.Error(err, " - failed to start the InternalHubComponent finalizer reconciler")
		os.Exit(1)
	}

	reconciler.DynamicWatcher = dynamicWatcher

	go func() {
		err := dynamicWatcher.Start(ctx)
		if err != nil {
			klog.Error(err, " - unable to start the dynamic watcher for the IHC finalizer reconciler")
			os.Exit(1)
		}

		wg.Done()
	}()

	klog.Info("Waiting for the dynamic watcher to start")
	<-dynamicWatcher.Started()

	if err := reconciler.WatchResources(); err != nil {
		klog.Error(err, " - finalizer reconciler unable to watch resources")

		if errors.Is(err, k8sdepwatches.ErrNoVersionedResource) {
			klog.Info("InternalHubComponent CRD not found, that resources will not be watched")
		} else {
			os.Exit(1)
		}
	}

	mgr, err := addonmanager.New(controllerContext.KubeConfig)
	if err != nil {
		klog.Error(err, " - unable to create new addon manager")
		os.Exit(1)
	}

	agentFuncs := []func(context.Context, addonmanager.AddonManager, *controllercmd.ControllerContext) error{
		policyframework.GetAndAddAgent,
		configpolicy.GetAndAddAgent,
	}

	for _, f := range agentFuncs {
		err := f(ctx, mgr, controllerContext)
		if err != nil {
			klog.Error(err, " - unable to get or add agent addon")
			os.Exit(1)
		}
	}

	wg.Add(1)

	go func() {
		defer wg.Done()

		err = mgr.Start(ctx)
		if err != nil {
			klog.Error(err, " - problem starting manager")
			os.Exit(1)
		}

		// mgr.Start is not blocking so wait on the context to finish
		<-ctx.Done()
	}()

	wg.Wait()

	return nil
}
