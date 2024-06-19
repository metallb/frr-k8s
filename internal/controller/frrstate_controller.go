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
	"reflect"
	"regexp"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frr-k8s/internal/frr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:skip

var passwordRegex = regexp.MustCompile(`password.*`)

// FRRStateReconciler reconciles the FRRStatus object.
type FRRStateReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	Update           chan event.GenericEvent
	Logger           log.Logger
	NodeName         string
	FRRStatus        frr.StatusFetcher
	ConversionResult ConversionResultFetcher
}

type stateEvent struct {
	metav1.TypeMeta
	metav1.ObjectMeta
}

func (evt *stateEvent) DeepCopyObject() runtime.Object {
	res := new(stateEvent)
	res.Name = evt.Name
	res.Namespace = evt.Namespace
	return res
}

func NewStateEvent() event.GenericEvent {
	evt := stateEvent{}
	evt.Name = "stateUpdate"
	evt.Namespace = "metallbreload"
	return event.GenericEvent{Object: &evt}
}

type ConversionResultFetcher interface {
	ConversionResult() string
}

// +kubebuilder:rbac:groups=frrk8s.metallb.io,resources=frrnodestates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=frrk8s.metallb.io,resources=frrnodestates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

func (r *FRRStateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	level.Info(r.Logger).Log("controller", "FRRStateReconciler", "start reconcile", req.NamespacedName.String())
	defer level.Info(r.Logger).Log("controller", "FRRStateReconciler", "end reconcile", req.NamespacedName.String())

	state := &frrk8sv1beta1.FRRNodeState{}

	err := r.Client.Get(ctx, types.NamespacedName{Name: r.NodeName}, state)
	if k8serrors.IsNotFound(err) {
		state.Name = r.NodeName
		err = r.Client.Create(ctx, state)
	}
	if err != nil {
		level.Error(r.Logger).Log("controller", "FRRStateReconciler", "failed to get", err)
		return ctrl.Result{}, err
	}
	frrStatus := r.FRRStatus.GetStatus()

	newStatus := frrk8sv1beta1.FRRNodeStateStatus{
		RunningConfig:        cleanPasswords(frrStatus.Current),
		LastReloadResult:     cleanPasswords(frrStatus.LastReloadResult),
		LastConversionResult: r.ConversionResult.ConversionResult(),
	}
	if reflect.DeepEqual(state.Status, newStatus) { // Do nothing
		return ctrl.Result{}, nil
	}

	state.Status = newStatus
	err = r.Client.Status().Update(ctx, state)
	if err != nil {
		level.Error(r.Logger).Log("controller", "FRRStateReconciler", "failed to update", err)
		return ctrl.Result{}, err
	}
	level.Debug(r.Logger).Log("controller", "FRRStateReconciler", "updated nodestate", dumpResource(state))

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FRRStateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	p := predicate.NewPredicateFuncs(func(o client.Object) bool {
		state, ok := o.(*frrk8sv1beta1.FRRNodeState)
		if !ok {
			return true
		}
		if state.Name != r.NodeName {
			return false
		}
		return true
	})
	return ctrl.NewControllerManagedBy(mgr).
		For(&frrk8sv1beta1.FRRNodeState{}).
		WatchesRawSource(source.Channel(r.Update, &handler.EnqueueRequestForObject{})).
		WithEventFilter(p).
		Complete(r)
}

func cleanPasswords(toClean string) string {
	cleaned := passwordRegex.ReplaceAllString(toClean, "password <retracted>")
	return cleaned
}
