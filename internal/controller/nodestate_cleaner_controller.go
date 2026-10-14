// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-kit/log/level"
	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frr-k8s/internal/logging"
)

const podNodeNameField = "spec.nodeName"

// NodeStateCleaner removes FRRNodeState resources for nodes without an FRR-K8s pod.
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

	if req.Name == "" {
		return ctrl.Result{}, nil
	}

	pods := &corev1.PodList{}
	if err := r.List(ctx, pods,
		client.InNamespace(r.Namespace),
		client.MatchingLabelsSelector{Selector: r.FRRK8sSelector},
		client.MatchingFields{podNodeNameField: req.Name},
	); err != nil {
		level.Error(l).Log("controller", "NodeStateCleaner", "failed to list FRR-K8s pods", "node", req.Name, "error", err)
		return ctrl.Result{}, err
	}
	if len(pods.Items) != 0 {
		return ctrl.Result{}, nil
	}

	nodeState := &frrk8sv1beta1.FRRNodeState{}
	if err := r.Get(ctx, types.NamespacedName{Name: req.Name}, nodeState); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		level.Error(l).Log("controller", "NodeStateCleaner", "failed to get FRRNodeState", "name", req.Name, "error", err)
		return ctrl.Result{}, err
	}

	level.Info(l).Log("controller", "NodeStateCleaner", "deleting FRRNodeState", "name", nodeState.Name, "reason", "no FRR pods on node")
	if err := r.Delete(ctx, nodeState); err != nil {
		level.Error(l).Log("controller", "NodeStateCleaner", "failed to delete FRRNodeState", "name", nodeState.Name, "error", err)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodeStateCleaner) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &corev1.Pod{}, podNodeNameField, podNodeNameIndex); err != nil {
		return err
	}

	nodeStateCreated := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			_, isNodeState := e.Object.(*frrk8sv1beta1.FRRNodeState)
			return isNodeState
		},
		UpdateFunc:  func(event.UpdateEvent) bool { return false },
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
	podDeleted := predicate.Funcs{
		CreateFunc: func(event.CreateEvent) bool { return false },
		UpdateFunc: func(event.UpdateEvent) bool { return false },
		DeleteFunc: func(e event.DeleteEvent) bool {
			pod, isPod := e.Object.(*corev1.Pod)
			return isPod && r.FRRK8sSelector.Matches(labels.Set(pod.Labels))
		},
		GenericFunc: func(event.GenericEvent) bool { return false },
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&frrk8sv1beta1.FRRNodeState{}, builder.WithPredicates(nodeStateCreated)).
		Watches(&corev1.Pod{}, handler.EnqueueRequestsFromMapFunc(reconcileRequestForPodNode), builder.WithPredicates(podDeleted)).
		Complete(r)
}

// reconcileRequestForPodNode maps a scheduled pod to its node-state reconciliation request.
func reconcileRequestForPodNode(_ context.Context, obj client.Object) []reconcile.Request {
	nodeNames := podNodeNameIndex(obj)
	if len(nodeNames) == 0 {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: nodeNames[0]}}}
}

// podNodeNameIndex returns the scheduled node for the pod cache index.
func podNodeNameIndex(obj client.Object) []string {
	pod, ok := obj.(*corev1.Pod)
	if !ok || pod.Spec.NodeName == "" {
		return nil
	}
	return []string{pod.Spec.NodeName}
}
