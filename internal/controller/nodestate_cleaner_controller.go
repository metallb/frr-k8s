// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
)

const (
	// PodNameAnnotation is the annotation key used to store the pod name in FRRNodeState.
	PodNameAnnotation = "frrk8s.metallb.io/pod-name"
)

// NodeStateCleaner reconciles Pod objects to clean up FRRNodeState resources.
type NodeStateCleaner struct {
	client.Client
	Scheme *runtime.Scheme
	Logger log.Logger
}

// +kubebuilder:rbac:groups=frrk8s.metallb.io,resources=frrnodestates,verbs=get;list;watch;delete

func (r *NodeStateCleaner) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	level.Info(r.Logger).Log("controller", "NodeStateCleaner", "start reconcile", req.String())
	defer level.Info(r.Logger).Log("controller", "NodeStateCleaner", "end reconcile", req.String())

	frrNodeStates := &frrk8sv1beta1.FRRNodeStateList{}
	if err := r.Client.List(ctx, frrNodeStates); err != nil {
		level.Error(r.Logger).Log("controller", "NodeStateCleaner", "failed to list FRRNodeStates", err)
		return ctrl.Result{}, err
	}

	var errors []error
	for _, nodeState := range frrNodeStates.Items {
		podName, exists := nodeState.Annotations[PodNameAnnotation]
		if !exists {
			level.Info(r.Logger).Log("controller", "NodeStateCleaner", "FRRNodeState has no pod name annotation", "name", nodeState.Name)
			continue
		}
		if podName == "" {
			level.Info(r.Logger).Log("controller", "NodeStateCleaner", "FRRNodeState has empty pod name annotation", "name", nodeState.Name)
			continue
		}

		pod := &corev1.Pod{}
		err := r.Client.Get(ctx, types.NamespacedName{Name: podName, Namespace: req.Namespace}, pod)
		if k8serrors.IsNotFound(err) {
			level.Info(r.Logger).Log("controller", "NodeStateCleaner", "deleting FRRNodeState", "name", nodeState.Name, "pod", podName)
			if err := r.Client.Delete(ctx, &nodeState); err != nil {
				level.Error(r.Logger).Log("controller", "NodeStateCleaner", "failed to delete FRRNodeState", "name", nodeState.Name, "error", err)
				errors = append(errors, fmt.Errorf("failed to delete FRRNodeState %s: %w", nodeState.Name, err))
			}
			continue
		}
		if err != nil {
			level.Error(r.Logger).Log("controller", "NodeStateCleaner", "failed to get pod", "pod", podName, "error", err)
			errors = append(errors, fmt.Errorf("failed to get pod %s: %w", podName, err))
		}
	}

	if len(errors) > 0 {
		level.Error(r.Logger).Log("controller", "NodeStateCleaner", "reconcile finished with errors", "error_count", len(errors))
		return ctrl.Result{}, fmt.Errorf("reconcile finished with %d errors", len(errors))
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodeStateCleaner) SetupWithManager(mgr ctrl.Manager) error {
	startupChan := make(chan event.GenericEvent, 1)
	// Trigger initial reconciliation, as pod might have
	// been deleted while we are down.
	go func() {
		startupChan <- NewStartupEvent()
	}()

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		WatchesRawSource(source.Channel(startupChan, &handler.EnqueueRequestForObject{})).
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return false
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				return false
			},
			GenericFunc: func(e event.GenericEvent) bool {
				return false
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return true
			},
		}).
		Complete(r)
}

type startupEvent struct {
	metav1.TypeMeta
	metav1.ObjectMeta
}

func (evt *startupEvent) DeepCopyObject() runtime.Object {
	res := new(startupEvent)
	res.Name = evt.Name
	res.Namespace = evt.Namespace
	return res
}

func NewStartupEvent() event.GenericEvent {
	evt := startupEvent{}
	evt.Name = "startup"
	evt.Namespace = "frrk8s"
	return event.GenericEvent{Object: &evt}
}
