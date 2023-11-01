// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

type healthcheck func() error

type healthChecker struct {
	check    healthcheck
	attempts int
}

type hcEvent struct {
	metav1.TypeMeta
	metav1.ObjectMeta
}

func (evt *hcEvent) DeepCopyObject() runtime.Object {
	res := new(hcEvent)
	res.Name = evt.Name
	res.Namespace = evt.Namespace
	return res
}

func NewHealthCheckEvent() event.GenericEvent {
	evt := hcEvent{}
	evt.Name = "healthcheck"
	return event.GenericEvent{Object: &evt}
}

func (r *healthChecker) Run() error {
	var err error
	for i := 0; i < r.attempts; i++ {
		err = r.check()
		if err == nil {
			break
		}
	}

	return err
}

// returns a healthcheck that checks if the apiserver is reachable.
func healthcheckForAPIServer(r client.Reader, ctx context.Context, timeout time.Duration, node string) healthcheck {
	return func() error {
		thisNode := &corev1.Node{}
		deadlineCtx, deadlineCancel := context.WithTimeout(ctx, timeout)
		defer deadlineCancel()

		return r.Get(deadlineCtx, types.NamespacedName{Name: node}, thisNode)
	}
}
