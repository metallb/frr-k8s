// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
	Scheme                       *runtime.Scheme
	Logger                       *logging.Logger
	Namespace                    string
	FRRK8sSelector               labels.Selector
	DefaultLogLevel              logging.Level
	FRROperatorConfigurationName string
}

// +kubebuilder:rbac:groups=frrk8s.metallb.io,resources=frrnodestates,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=frrk8s.metallb.io,resources=frroperatorconfigurations,verbs=get;list;watch

func (r *NodeStateCleaner) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	level.Info(r.Logger).Log("controller", "NodeStateCleaner", "start reconcile", req.String())
	defer level.Info(r.Logger).Log("controller", "NodeStateCleaner", "end reconcile", req.String())

	operatorConfig := frrk8sv1beta1.FRROperatorConfiguration{}
	err := r.Get(ctx, types.NamespacedName{Namespace: r.Namespace, Name: r.FRROperatorConfigurationName}, &operatorConfig)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	currentLogLevel, err := getLogLevel(ctx, r, r.Namespace, r.FRROperatorConfigurationName, r.DefaultLogLevel)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Dynamically set log level for the controller and log what the current log level is.
	r.Logger.SetLogLevel(currentLogLevel)
	level.Debug(r.Logger).Log("controller", "NodeStateCleaner", "log level controller", currentLogLevel)

	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(r.Namespace), client.MatchingLabelsSelector{Selector: r.FRRK8sSelector}); err != nil {
		level.Error(r.Logger).Log("controller", "NodeStateCleaner", "failed to list FRR-K8s pods", err)
		return ctrl.Result{}, err
	}

	nodesWithFRRPods := make(map[string]struct{})
	for _, pod := range pods.Items {
		nodesWithFRRPods[pod.Spec.NodeName] = struct{}{}
	}

	frrNodeStates := &frrk8sv1beta1.FRRNodeStateList{}
	if err := r.List(ctx, frrNodeStates); err != nil {
		level.Error(r.Logger).Log("controller", "NodeStateCleaner", "failed to list FRRNodeStates", err)
		return ctrl.Result{}, err
	}

	var errors []error
	for _, nodeState := range frrNodeStates.Items {
		if _, hasFRRPods := nodesWithFRRPods[nodeState.Name]; !hasFRRPods {
			level.Info(r.Logger).Log("controller", "NodeStateCleaner", "deleting FRRNodeState", "name", nodeState.Name, "reason", "no FRR pods on node")
			if err := r.Delete(ctx, &nodeState); err != nil {
				level.Error(r.Logger).Log("controller", "NodeStateCleaner", "failed to delete FRRNodeState", "name", nodeState.Name, "error", err)
				errors = append(errors, fmt.Errorf("failed to delete FRRNodeState %s: %w", nodeState.Name, err))
			}
		}
	}

	if len(errors) > 0 {
		level.Error(r.Logger).Log("controller", "NodeStateCleaner", "reconcile finished with errors", "error_count", len(errors))
		return ctrl.Result{}, utilerrors.NewAggregate(errors)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodeStateCleaner) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Watches(&frrk8sv1beta1.FRRNodeState{}, &handler.EnqueueRequestForObject{}).
		Watches(&frrk8sv1beta1.FRROperatorConfiguration{}, &handler.EnqueueRequestForObject{}).
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				_, isPod := e.Object.(*corev1.Pod)
				return !isPod
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				_, isFRROperatorConfiguration := e.ObjectNew.(*frrk8sv1beta1.FRROperatorConfiguration)
				return isFRROperatorConfiguration
			},
			GenericFunc: func(e event.GenericEvent) bool {
				_, isFRROperatorConfiguration := e.Object.(*frrk8sv1beta1.FRROperatorConfiguration)
				return isFRROperatorConfiguration
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				_, isPod := e.Object.(*corev1.Pod)
				_, isFRROperatorConfiguration := e.Object.(*frrk8sv1beta1.FRROperatorConfiguration)
				return isPod || isFRROperatorConfiguration
			},
		}).
		Complete(r)
}
