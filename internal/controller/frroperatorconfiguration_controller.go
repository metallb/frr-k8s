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

package controller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/go-kit/log/level"
	frrk8sv1beta2 "github.com/metallb/frr-k8s/api/v1beta2"
	"github.com/metallb/frr-k8s/internal/logging"
)

// FRROperatorConfigurationReconciler reconciles a FRROperatorConfiguration object.
type FRROperatorConfigurationReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	Logger          *logging.DynamicLvlLogger
	DumpResources   bool
	NodeName        string
	Namespace       string
	DefaultLogLevel string
}

// +kubebuilder:rbac:groups=frrk8s.metallb.io,resources=frroperatorconfigurations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=frrk8s.metallb.io,resources=frroperatorconfigurations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=frrk8s.metallb.io,resources=frroperatorconfigurations/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=validatingwebhookconfigurations,verbs=get;list;watch
// +kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=validatingwebhookconfigurations,resourceNames="frr-k8s-validating-webhook-configuration",verbs=update
// +kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=mutatingwebhookconfigurations,verbs=get;list;watch
// +kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=mutatingwebhookconfigurations,resourceNames="frr-k8s-mutating-webhook-configuration",verbs=update

func (r *FRROperatorConfigurationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	level.Info(r.Logger).Log("controller", "FRROperatorConfigurationReconciler", "start reconcile", req.String())
	defer level.Info(r.Logger).Log("controller", "FRROperatorConfigurationReconciler", "end reconcile", req.String())

	configs := frrk8sv1beta2.FRROperatorConfigurationList{}
	err := r.List(ctx, &configs, client.InNamespace(r.Namespace))
	if err != nil {
		return ctrl.Result{}, err
	}

	config := frrk8sv1beta2.FRROperatorConfiguration{}
	logLevelController := r.DefaultLogLevel
	switch len(configs.Items) {
	case 0:
		// Set back to defaults as config is gone.
	case 1:
		config = configs.Items[0]
		if config.Spec.LogLevel != "" {
			logLevelController = config.Spec.LogLevel
		}
	default:
		return ctrl.Result{},
			fmt.Errorf("more than a single configuration object found in namespace %s, objects: %+v",
				r.Namespace, configs.Items)
	}

	// Set log level for the controller.
	if err = r.Logger.SetLogLevel(logLevelController); err != nil {
		return ctrl.Result{}, err
	}
	level.Info(r.Logger).Log("controller", "FRROperatorConfigurationReconciler", "log level controller", logLevelController)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FRROperatorConfigurationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&frrk8sv1beta2.FRROperatorConfiguration{}).
		WithEventFilter(predicate.NewPredicateFuncs(func(object client.Object) bool {
			return object.GetNamespace() == r.Namespace
		})).
		Complete(r)
}
