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
	"flag"
	"fmt"
	"os"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	frrk8sv1beta1 "github.com/metallb/frrk8s/api/v1beta1"
	"github.com/metallb/frrk8s/internal/controller"
	"github.com/metallb/frrk8s/internal/frr"
	"github.com/metallb/frrk8s/internal/logging"
	"github.com/metallb/frrk8s/internal/version"
	"github.com/open-policy-agent/cert-controller/pkg/rotator"
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

func main() {
	var (
		metricsAddr         string
		probeAddr           string
		logLevel            string
		nodeName            string
		namespace           string
		disableCertRotation bool
		certDir             string
		certServiceName     string
		webhookMode         string
		pprofAddr           string
		healthCheckMode     string
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", "127.0.0.1:7572", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", "127.0.0.1:8081", "The address the probe endpoint binds to.")
	flag.StringVar(&logLevel, "log-level", "info", fmt.Sprintf("log level. must be one of: [%s]", logging.Levels.String()))
	flag.StringVar(&nodeName, "node-name", "", "The node this daemon is running on.")
	flag.StringVar(&namespace, "namespace", "", "The namespace this daemon is deployed in")
	flag.StringVar(&webhookMode, "webhook-mode", "disabled", "webhook mode: can be disabled or onlywebhook if we want the controller to act as webhook endpoint only")
	flag.BoolVar(&disableCertRotation, "disable-cert-rotation", false, "disable automatic generation and rotation of webhook TLS certificates/keys")
	flag.StringVar(&certDir, "cert-dir", "/tmp/k8s-webhook-server/serving-certs", "The directory where certs are stored")
	flag.StringVar(&certServiceName, "cert-service-name", "frr-k8s-webhook-service", "The service name used to generate the TLS cert's hostname")
	flag.StringVar(&pprofAddr, "pprof-bind-address", "", "The address the pprof endpoints bind to.")
	flag.StringVar(&healthCheckMode, "healthcheck-mode", "enabled", "healthcheck mode: can be disabled or enabled")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	logger, err := logging.Init(logLevel)
	if err != nil {
		fmt.Printf("failed to initialize logging: %s\n", err)
		os.Exit(1)
	}

	namespaceSelector := cache.ByObject{
		Field: fields.ParseSelectorOrDie(fmt.Sprintf("metadata.namespace=%s", namespace)),
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: probeAddr,
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				&corev1.Secret{}: namespaceSelector,
			},
		},
		WebhookServer: webhook.NewServer(
			webhook.Options{
				Port: 9443,
			},
		),
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		PprofBindAddress: pprofAddr,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	ctx := ctrl.SetupSignalHandler()

	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	enableWebhook := webhookMode == "onlywebhook"
	startListeners := make(chan struct{})
	if enableWebhook && !disableCertRotation {
		setupLog.Info("Starting certs generator")
		err = setupCertRotation(startListeners, mgr, logger, namespace, certDir, certServiceName)
		if err != nil {
			setupLog.Error(err, "unable to set up cert rotator")
			os.Exit(1)
		}
	} else {
		close(startListeners)
	}

	go func() {
		<-startListeners

		if enableWebhook {
			setupLog.Info("Starting webhooks")
			err := setupWebhook(mgr, namespace, logger)
			if err != nil {
				setupLog.Error(err, "unable to create", "webhooks")
				os.Exit(1)
			}
			return // We currently support only a onlywebhook mode
		}

		setupLog.Info("Starting controllers")
		reloadStatusChan := make(chan event.GenericEvent)
		reloadStatus := func() {
			reloadStatusChan <- controller.NewStateEvent()
		}
		reloadHealthChan := make(chan event.GenericEvent)
		reloadHealth := func() {
			reloadHealthChan <- controller.NewHealthCheckEvent()
		}

		onFRRReload := reloadStatus
		if healthCheckMode == "enabled" {
			onFRRReload = func() { reloadStatus(); reloadHealth() }
		}
		frrInstance := frr.NewFRR(ctx, onFRRReload, logger)
		hostName, err := os.Hostname()
		if err != nil {
			setupLog.Error(err, "unable to get hostname")
			os.Exit(1)
		}
		configReconciler := &controller.FRRConfigurationReconciler{
			Client:       mgr.GetClient(),
			Scheme:       mgr.GetScheme(),
			FRRHandler:   frrInstance,
			FRRLogLevel:  logging.LevelToFRR(logging.Level(logLevel)),
			Hostname:     hostName,
			Logger:       logger,
			NodeName:     nodeName,
			ReloadStatus: reloadStatus,
			HealthUpdate: reloadHealthChan,
		}
		if err = configReconciler.SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "FRRConfiguration")
			os.Exit(1)
		}

		if healthCheckMode == "enabled" {
			err = configReconciler.SetupHealthCheck(mgr.GetAPIReader(), ctx, 3, 30*time.Second, 5*time.Minute)
			if err != nil {
				setupLog.Error(err, "unable to setup healthcheck", "controller", "FRRConfiguration")
				os.Exit(1)
			}
		}

		if err = (&controller.FRRStateReconciler{
			Client:           mgr.GetClient(),
			Scheme:           mgr.GetScheme(),
			FRRStatus:        frrInstance,
			Logger:           logger,
			NodeName:         nodeName,
			Update:           reloadStatusChan,
			ConversionResult: configReconciler,
			HealthResult:     configReconciler,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "FRRStatus")
			os.Exit(1)
		}
	}()

	setupLog.Info("starting frr-k8s", "version", version.String())
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

const (
	caName         = "cert"
	caOrganization = "frrk8s"
)

var (
	webhookName       = "frr-k8s-validating-webhook-configuration"
	webhookSecretName = "frr-k8s-webhook-server-cert" //#nosec G101
)

func setupCertRotation(notifyFinished chan struct{}, mgr manager.Manager, logger log.Logger,
	namespace, certDir, certServiceName string) error {
	webhooks := []rotator.WebhookInfo{
		{
			Name: webhookName,
			Type: rotator.Validating,
		},
	}

	level.Info(logger).Log("op", "startup", "action", "setting up cert rotation")
	err := rotator.AddRotator(mgr, &rotator.CertRotator{
		SecretKey: types.NamespacedName{
			Namespace: namespace,
			Name:      webhookSecretName,
		},
		CertDir:        certDir,
		CAName:         caName,
		CAOrganization: caOrganization,
		DNSName:        fmt.Sprintf("%s.%s.svc", certServiceName, namespace),
		IsReady:        notifyFinished,
		Webhooks:       webhooks,
		FieldOwner:     "frr-k8s",
	})
	if err != nil {
		level.Error(logger).Log("error", err, "unable to set up", "cert rotation")
		return err
	}
	return nil
}

func setupWebhook(mgr manager.Manager, namespace string, logger log.Logger) error {
	level.Info(logger).Log("op", "startup", "action", "webhooks enabled")

	frrk8sv1beta1.Namespace = namespace
	frrk8sv1beta1.Logger = logger
	frrk8sv1beta1.WebhookClient = mgr.GetAPIReader()
	frrk8sv1beta1.Validate = controller.Validate

	if err := (&frrk8sv1beta1.FRRConfiguration{}).SetupWebhookWithManager(mgr); err != nil {
		level.Error(logger).Log("op", "startup", "error", err, "msg", "unable to create webhook", "webhook", "FRRConfigurations")
		return err
	}

	return nil
}
