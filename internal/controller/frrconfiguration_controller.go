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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	frrk8sv1beta1 "github.com/metallb/frrk8s/api/v1beta1"
	"github.com/metallb/frrk8s/internal/frr"
)

// FRRConfigurationReconciler reconciles a FRRConfiguration object.
type FRRConfigurationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	FRR    *frr.FRR
	Logger log.Logger
}

// +kubebuilder:rbac:groups=frrk8s.metallb.io,resources=frrconfigurations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=frrk8s.metallb.io,resources=frrconfigurations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=frrk8s.metallb.io,resources=frrconfigurations/finalizers,verbs=update

func (r *FRRConfigurationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	level.Info(r.Logger).Log("controller", "FRRConfigurationReconciler", "start reconcile", req.NamespacedName.String())
	defer level.Info(r.Logger).Log("controller", "FRRConfigurationReconciler", "end reconcile", req.NamespacedName.String())

	configs := frrk8sv1beta1.FRRConfigurationList{}
	err := r.Client.List(ctx, &configs)
	if err != nil {
		return ctrl.Result{}, err
	}
	if len(configs.Items) > 1 { // TODO implement merging
		return ctrl.Result{}, nil
	}

	if len(configs.Items) == 0 {
		config := frrk8sv1beta1.FRRConfiguration{}
		if err := r.FRR.ApplyConfig(config); err != nil {
			level.Error(r.Logger).Log("controller", "FRRConfigurationReconciler", "failed to apply the empty config", req.NamespacedName.String(), "error", err)
		}
		return ctrl.Result{}, nil
	}
	if err := r.FRR.ApplyConfig(configs.Items[0]); err != nil {
		level.Error(r.Logger).Log("controller", "FRRConfigurationReconciler", "failed to apply the config", req.NamespacedName.String(), "error", err)
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FRRConfigurationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&frrk8sv1beta1.FRRConfiguration{}).
		Complete(r)
}
