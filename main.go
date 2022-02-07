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

	"github.com/JustinKuli/governance-policy-addon-controller/pkg/addon/certpolicy"
	"github.com/JustinKuli/governance-policy-addon-controller/pkg/addon/configpolicy"
	"github.com/JustinKuli/governance-policy-addon-controller/pkg/addon/iampolicy"
	"github.com/JustinKuli/governance-policy-addon-controller/pkg/addon/policyframework"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	utilflag "k8s.io/component-base/cli/flag"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/component-base/logs"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"

	ctrl "sigs.k8s.io/controller-runtime"
	//+kubebuilder:scaffold:imports
)

//+kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=get;create
//+kubebuilder:rbac:groups=certificates.k8s.io,resources=certificatesigningrequests;certificatesigningrequests/approval,verbs=get;list;watch;create;update
//+kubebuilder:rbac:groups=certificates.k8s.io,resources=signers,verbs=approve
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=clustermanagementaddons,verbs=get;list;watch

// RBAC below will need to be updated if/when new policy controllers are added.

//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=create
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;patch;update,resourceNames=policy-framework;config-policy-controller;iam-policy-controller;cert-policy-controller

//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=create
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;update;patch;delete,resourceNames=policy-framework;config-policy-controller;iam-policy-controller;cert-policy-controller
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=create
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;update;patch;delete,resourceNames="open-cluster-management:policy-framework";"open-cluster-management:config-policy-controller";"open-cluster-management:iam-policy-controller";"open-cluster-management:cert-policy-controller"

//+kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworks,verbs=create
//+kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworks,verbs=get;list;watch
//+kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworks,verbs=update;patch;delete,resourceNames=addon-config-policy-controller-deploy;addon-governance-policy-framework-deploy;addon-iam-policy-controller-deploy;addon-cert-policy-controller-deploy

//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons,verbs=create
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons,verbs=get;list;watch;update
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons/finalizers,verbs=update,resourceNames=config-policy-controller;governance-policy-framework;iam-policy-controller;cert-policy-controller
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons/status,verbs=update;patch,resourceNames=config-policy-controller;governance-policy-framework;iam-policy-controller;cert-policy-controller

// Permissions required for policy-framework
// (see https://kubernetes.io/docs/reference/access-authn-authz/rbac/#privilege-escalation-prevention-and-bootstrapping)

//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=policies,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=policies/finalizers,verbs=update
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=policies/status,verbs=get;patch;update

var (
	setupLog    = ctrl.Log.WithName("setup")
	ctrlVersion = version.Info{}
)

const (
	ctrlName = "governance-policy-addon-controller"
)

func main() {
	pflag.CommandLine.SetNormalizeFunc(utilflag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)

	logs.InitLogs()
	defer logs.FlushLogs()

	cmd := &cobra.Command{
		Use:   "addon",
		Short: "helloworldhelm example addon",
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
		setupLog.Error(err, "unable to create new addon manager")
		os.Exit(1)
	}

	frameworkAgentAddon, err := policyframework.GetAgentAddon(controllerContext)
	if err != nil {
		setupLog.Error(err, "unable to get policy framework agent addon")
		os.Exit(1)
	}

	err = mgr.AddAgent(frameworkAgentAddon)
	if err != nil {
		setupLog.Error(err, "unable to add policy framework agent addon")
		os.Exit(1)
	}

	configAgentAddon, err := configpolicy.GetAgentAddon(controllerContext)
	if err != nil {
		setupLog.Error(err, "unable to get config policy agent addon")
		os.Exit(1)
	}

	err = mgr.AddAgent(configAgentAddon)
	if err != nil {
		setupLog.Error(err, "unable to add config policy agent addon")
		os.Exit(1)
	}

	iamAgentAddon, err := iampolicy.GetAgentAddon(controllerContext)
	if err != nil {
		setupLog.Error(err, "unable to get iam policy agent addon")
		os.Exit(1)
	}

	err = mgr.AddAgent(iamAgentAddon)
	if err != nil {
		setupLog.Error(err, "unable to add iam policy agent addon")
		os.Exit(1)
	}

	certAgentAddon, err := certpolicy.GetAgentAddon(controllerContext)
	if err != nil {
		setupLog.Error(err, "unable to get cert policy agent addon")
		os.Exit(1)
	}

	err = mgr.AddAgent(certAgentAddon)
	if err != nil {
		setupLog.Error(err, "unable to add cert policy agent addon")
		os.Exit(1)
	}

	err = mgr.Start(ctx)
	if err != nil {
		setupLog.Error(err, "problem starting manager")
		os.Exit(1)
	}

	<-ctx.Done()

	return nil
}
