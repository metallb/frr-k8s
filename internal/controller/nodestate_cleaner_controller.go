// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/go-kit/log/level"
	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frr-k8s/internal/logging"
)

// NodeStateCleaner reconciles Pod objects to clean up FRRNodeState resources.
type NodeStateCleaner struct {
	client.Client
	Scheme         *runtime.Scheme
	Namespace      string
	FRRK8sSelector labels.Selector
}

// +kubebuilder:rbac:groups=frrk8s.metallb.io,resources=frrnodestates,verbs=get;list;watch;delete

func (r *NodeStateCleaner) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := logging.GetLogger()
	level.Info(l).Log("controller", "NodeStateCleaner", "start reconcile", req.String())
	defer level.Info(l).Log("controller", "NodeStateCleaner", "end reconcile", req.String())
	level.Debug(l).Log("controller", "NodeStateCleaner", "log level controller", "debug")

	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(r.Namespace), client.MatchingLabelsSelector{Selector: r.FRRK8sSelector}); err != nil {
		level.Error(l).Log("controller", "NodeStateCleaner", "failed to list FRR-K8s pods", err)
		return ctrl.Result{}, err
	}

	nodesWithFRRPods := make(map[string]struct{})
	for _, pod := range pods.Items {
		nodesWithFRRPods[pod.Spec.NodeName] = struct{}{}
	}

	frrNodeStates := &frrk8sv1beta1.FRRNodeStateList{}
	if err := r.List(ctx, frrNodeStates); err != nil {
		level.Error(l).Log("controller", "NodeStateCleaner", "failed to list FRRNodeStates", err)
		return ctrl.Result{}, err
	}

	var errors []error
	for _, nodeState := range frrNodeStates.Items {
		if _, hasFRRPods := nodesWithFRRPods[nodeState.Name]; !hasFRRPods {
			level.Info(l).Log("controller", "NodeStateCleaner", "deleting FRRNodeState", "name", nodeState.Name, "reason", "no FRR pods on node")
			if err := r.Delete(ctx, &nodeState); err != nil {
				level.Error(l).Log("controller", "NodeStateCleaner", "failed to delete FRRNodeState", "name", nodeState.Name, "error", err)
				errors = append(errors, fmt.Errorf("failed to delete FRRNodeState %s: %w", nodeState.Name, err))
			}
		}
	}

	if len(errors) > 0 {
		level.Error(l).Log("controller", "NodeStateCleaner", "reconcile finished with errors", "error_count", len(errors))
		return ctrl.Result{}, utilerrors.NewAggregate(errors)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodeStateCleaner) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Watches(&frrk8sv1beta1.FRRNodeState{}, &handler.EnqueueRequestForObject{}).
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				_, isPod := e.Object.(*corev1.Pod)
				return !isPod
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				return false
			},
			GenericFunc: func(e event.GenericEvent) bool {
				return false
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				_, isPod := e.Object.(*corev1.Pod)
				return isPod
			},
		}).
		Complete(r)
}
