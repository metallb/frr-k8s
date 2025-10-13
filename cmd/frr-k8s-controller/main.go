/*
Copyright 2023.

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
	"net"
	"os"
	"strings"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/go-kit/log"
	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frr-k8s/internal/controller"
	"github.com/metallb/frr-k8s/internal/frr"
	"github.com/metallb/frr-k8s/internal/logging"
	"github.com/metallb/frr-k8s/internal/version"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(frrk8sv1beta1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

type params struct {
	metricsAddr      string
	logLevel         string
	nodeName         string
	namespace        string
	podName          string
	pprofAddr        string
	alwaysBlockCIDRs string
}

func main() {
	params := params{}

	flag.StringVar(&params.metricsAddr, "metrics-bind-address", "127.0.0.1:7572", "The address the metric endpoint binds to.")
	flag.StringVar(&params.logLevel, "log-level", "info", fmt.Sprintf("log level. must be one of: [%s]", logging.Levels.String()))
	flag.StringVar(&params.nodeName, "node-name", "", "The node this daemon is running on.")
	flag.StringVar(&params.namespace, "namespace", "", "The namespace this daemon is deployed in")
	flag.StringVar(&params.podName, "pod-name", "", "The name of the pod this process is running in")
	flag.StringVar(&params.pprofAddr, "pprof-bind-address", "", "The address the pprof endpoints bind to.")
	flag.StringVar(&params.alwaysBlockCIDRs, "always-block", "", "a list of comma separated cidrs we need to always block")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	logger, err := logging.Init(params.logLevel)
	if err != nil {
		fmt.Printf("failed to initialize logging: %s\n", err)
		os.Exit(1)
	}

	namespaceSelector := cache.ByObject{
		Field: fields.ParseSelectorOrDie(fmt.Sprintf("metadata.namespace=%s", params.namespace)),
	}

	options := ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: "", // we use the metrics endpoint for healthchecks
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				&corev1.Secret{}:                  namespaceSelector,
				&corev1.Pod{}:                     namespaceSelector,
				&frrk8sv1beta1.FRRConfiguration{}: namespaceSelector,
			},
		},
		Metrics: metricsserver.Options{
			BindAddress: params.metricsAddr,
		},
		PprofBindAddress: params.pprofAddr,
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), options)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	ctx := ctrl.SetupSignalHandler()

	//+kubebuilder:scaffold:builder

	startFRRControllers(ctx, mgr, params, logger)

	setupLog.Info("starting frr-k8s", "version", version.String(), "params", fmt.Sprintf("%+v", params))
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func startFRRControllers(ctx context.Context, mgr manager.Manager, params params, logger log.Logger) {
	setupLog.Info("Starting controllers")
	reloadStatusChan := make(chan event.GenericEvent)
	reloadStatus := func() {
		reloadStatusChan <- controller.NewStateEvent()
	}
	frrInstance := frr.NewFRR(ctx, reloadStatus, logger, logging.Level(params.logLevel))

	alwaysBlock, err := parseCIDRs(params.alwaysBlockCIDRs)
	if err != nil {
		setupLog.Error(err, "failed to parse the always-block parameter", "always-block", params.alwaysBlockCIDRs)
		os.Exit(1)
	}

	dumpResources := params.logLevel == logging.LevelDebug || params.logLevel == logging.LevelAll
	configReconciler := &controller.FRRConfigurationReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		FRRHandler:       frrInstance,
		Logger:           logger,
		NodeName:         params.nodeName,
		ReloadStatus:     reloadStatus,
		AlwaysBlockCIDRS: alwaysBlock,
		DumpResources:    dumpResources,
	}
	if err = configReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "FRRConfiguration")
		os.Exit(1)
	}

	if err = (&controller.FRRStateReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		FRRStatus:        frrInstance,
		Logger:           logger,
		NodeName:         params.nodeName,
		Update:           reloadStatusChan,
		ConversionResult: configReconciler,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "FRRStatus")
		os.Exit(1)
	}
}

func parseCIDRs(cidrs string) ([]net.IPNet, error) {
	if cidrs == "" {
		return nil, nil
	}

	elems := strings.Split(cidrs, ",")
	res := make([]net.IPNet, 0, len(elems))
	for _, e := range elems {
		trimmed := strings.Trim(e, " ")
		_, cidr, err := net.ParseCIDR(trimmed)
		if err != nil {
			return nil, fmt.Errorf("failed to parse cidr %s: %w", e, err)
		}
		res = append(res, *cidr)
	}
	return res, nil
}
