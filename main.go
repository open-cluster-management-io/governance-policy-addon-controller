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

	"github.com/JustinKuli/governance-policy-addon-controller/pkg/addon/helloworld_helm"
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

//+kubebuilder:rbac:groups=*,resources=*,verbs=*

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

	ctrlcmd := controllercmd.
		NewControllerCommandConfig(ctrlName, ctrlVersion, runController).
		NewCommandWithContext(context.TODO())
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

	expAgentAddon, err := helloworld_helm.GetAgentAddon(controllerContext)
	if err != nil {
		setupLog.Error(err, "unable to get experiment agent addon")
		os.Exit(1)
	}

	err = mgr.AddAgent(expAgentAddon)
	if err != nil {
		setupLog.Error(err, "unable to add experiment agent addon")
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
