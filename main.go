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
	"runtime"
	"strconv"
	"sync"

	"github.com/go-logr/zapr"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stolostron/go-log-utils/zaputil"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/version"
	utilflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	ctrl "sigs.k8s.io/controller-runtime"

	"open-cluster-management.io/governance-policy-addon-controller/pkg/addon/configpolicy"
	"open-cluster-management.io/governance-policy-addon-controller/pkg/addon/policyframework"
	"open-cluster-management.io/governance-policy-addon-controller/pkg/addon/standalonetemplating"
)

//+kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=get;create
//+kubebuilder:rbac:groups=certificates.k8s.io,resources=certificatesigningrequests;certificatesigningrequests/approval,verbs=get;list;watch;create;update
//+kubebuilder:rbac:groups=certificates.k8s.io,resources=signers,verbs=approve
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=clustermanagementaddons,verbs=get;list;watch

// RBAC below will need to be updated if/when new policy controllers are added.

//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=create
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;patch;update,resourceNames=governance-policy-framework;config-policy-controller;governance-standalone-hub-templating

//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=create
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;update;patch;delete,resourceNames="open-cluster-management:policy-framework-hub";"open-cluster-management:config-policy-controller-hub";"open-cluster-management:governance-standalone-hub-templating"
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=create
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;update;patch;delete,resourceNames="open-cluster-management:policy-framework-hub";"open-cluster-management:config-policy-controller-hub";"open-cluster-management:governance-standalone-hub-templating"

// Cannot limit based on resourceNames because the name is dynamic in hosted mode.
//+kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworks,verbs=create;delete;get;list;patch;update;watch

//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons,verbs=create
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons,verbs=get;list;watch;update
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons,verbs=delete,resourceNames=config-policy-controller;governance-policy-framework;governance-standalone-hub-templating
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons/finalizers,verbs=update,resourceNames=config-policy-controller;governance-policy-framework;governance-standalone-hub-templating
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons/status,verbs=update;patch,resourceNames=config-policy-controller;governance-policy-framework;governance-standalone-hub-templating
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=clustermanagementaddons/status,verbs=update;patch,resourceNames=config-policy-controller;governance-policy-framework;governance-standalone-hub-templating

//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=clustermanagementaddons/finalizers,verbs=update,resourceNames=config-policy-controller;governance-policy-framework;governance-standalone-hub-templating
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

var (
	ctrlVersion = version.Info{}
	log         = ctrl.Log.WithName("setup")
	// Set up unified log level and encoding flags
	zflags = zaputil.FlagConfig{
		LevelName:   "log-level",
		EncoderName: "log-encoder",
	}
)

const (
	ctrlName = "governance-policy-addon-controller"
)

func main() {
	// Bind command line flags to the various cmd/log configurations
	zflags.Bind(flag.CommandLine)
	klog.InitFlags(flag.CommandLine)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.CommandLine.SetNormalizeFunc(utilflag.WordSepNormalizeFunc)

	// Set up an initial logger for klog, since flags are parsed inside of the Cobra Execute()
	// NOTE: This will be set again using go-log-utils in setupLogging()
	klogConfig := zap.NewProductionConfig()
	klogConfig.Encoding = "console"
	klogConfig.DisableStacktrace = true
	klogConfig.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	klogZap, err := klogConfig.Build()
	if err != nil {
		panic(fmt.Sprintf("Failed to build zap logger for klog: %v", err))
	}

	klog.SetLogger(zapr.NewLogger(klogZap).WithName("klog"))

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
	setupLogging()

	log.Info("Starting "+ctrlName, "GoVersion", runtime.Version(), "GOOS", runtime.GOOS, "GOARCH", runtime.GOARCH)

	mgr, err := addonmanager.New(controllerContext.KubeConfig)
	if err != nil {
		log.Error(err, "unable to create new addon manager")
		os.Exit(1)
	}

	agentFuncs := []func(context.Context, addonmanager.AddonManager, *controllercmd.ControllerContext) error{
		policyframework.GetAndAddAgent,
		configpolicy.GetAndAddAgent,
		standalonetemplating.GetAndAddAgent,
	}

	wg := sync.WaitGroup{}

	for _, f := range agentFuncs {
		err := f(ctx, mgr, controllerContext)
		if err != nil {
			log.Error(err, "unable to get or add agent addon")
			os.Exit(1)
		}
	}

	wg.Add(1)

	go func() {
		defer wg.Done()

		err = mgr.Start(ctx)
		if err != nil {
			log.Error(err, "problem starting manager")
			os.Exit(1)
		}

		// mgr.Start is not blocking so wait on the context to finish
		<-ctx.Done()
	}()

	wg.Wait()

	return nil
}

func setupLogging() {
	// Build controller-runtime logger
	ctrlZap, err := zflags.BuildForCtrl()
	if err != nil {
		panic(fmt.Sprintf("Failed to build zap logger for controller: %v", err))
	}

	// Bind the controller-runtime logger
	ctrl.SetLogger(zapr.NewLogger(ctrlZap))

	// Configure klog logger
	// (This is a fragment from the go-log-utils BuildForKlog() because the
	// SkipLineEnding setting there removes newlines that must be preserved
	// here)
	klogConfig := zflags.GetConfig()

	klogV := flag.CommandLine.Lookup("v")
	if klogV != nil {
		var klogLevel int

		klogLevel, err = strconv.Atoi(klogV.Value.String())
		if err != nil {
			klogConfig.Level = zap.NewAtomicLevelAt(zapcore.Level(int8(-1 * klogLevel)))
		}
	}

	// Handle errors from building the klog configuration
	if klogV == nil || err != nil {
		log.Info("Failed to parse 'v' flag in flagset for klog--verbosity will not be configurable for klog.")
	}

	// Build the klog logger
	klogZap, err := klogConfig.Build()
	if err != nil {
		log.Error(err, "Failed to build zap logger for klog, those logs will not go through zap")
	} else {
		klog.SetLogger(zapr.NewLogger(klogZap).WithName("klog"))
	}
}
