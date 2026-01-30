/*
Copyright 2026.

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

package controller

import (
	"context"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/go-kit/log/level"
	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frr-k8s/internal/logging"
)

// frrK8sConfigurationName is the name of the FRRK8sConfiguration CR to watch.
const frrK8sConfigurationName = "config"

// FRRK8sConfigurationReconciler reconciles a FRRK8sConfiguration object.
type FRRK8sConfigurationReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	Namespace       string
	DefaultLogLevel logging.Level
}

// +kubebuilder:rbac:groups=frrk8s.metallb.io,resources=frrk8sconfigurations,verbs=get;list;watch

func (r *FRRK8sConfigurationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := logging.GetLogger()
	level.Info(l).Log("controller", "FRRK8sConfigurationReconciler", "start reconcile", req.String())
	defer level.Info(l).Log("controller", "FRRK8sConfigurationReconciler", "end reconcile", req.String())

	currentLogLevel, err := getLogLevel(ctx, r, r.Namespace, r.DefaultLogLevel)
	if err != nil {
		return ctrl.Result{}, err
	}
	l.SetLogLevel(currentLogLevel)
	level.Debug(l).Log("controller", "FRRK8sConfigurationReconciler", "log level controller", currentLogLevel)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FRRK8sConfigurationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&frrk8sv1beta1.FRRK8sConfiguration{}).
		Complete(r)
}

// getLogLevel extracts the log level from an FRRK8sConfiguration resource.
// It attempts to parse the LogLevel field from the configuration spec and returns the parsed log level.
// If the resource cannot be found or if the field is empty, it falls back to the provided defaultLogLevel.
// If any other error occurs on r.Get, or if parsing fails, it returns logging.LevelFallback and an error.
func getLogLevel(ctx context.Context, r client.Reader, namespace string, defaultLogLevel logging.Level) (logging.Level, error) {
	config := frrk8sv1beta1.FRRK8sConfiguration{}
	err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: frrK8sConfigurationName}, &config)
	if k8serrors.IsNotFound(err) {
		return defaultLogLevel, nil
	}
	if err != nil {
		return logging.LevelFallback, err
	}
	if config.Spec.LogLevel == "" {
		return defaultLogLevel, nil
	}
	return logging.ParseLevel(config.Spec.LogLevel)
}
