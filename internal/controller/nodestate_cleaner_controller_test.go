// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/go-kit/log"
	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNodeStateCleaner_Reconcile(t *testing.T) {
	tests := []struct {
		name           string
		pod            *corev1.Pod
		frrNodeStates  []frrk8sv1beta1.FRRNodeState
		expectedDelete bool
	}{
		{
			name: "pod exists, FRRNodeState should not be deleted",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					Labels: map[string]string{
						"app": "frr-k8s",
					},
				},
			},
			frrNodeStates: []frrk8sv1beta1.FRRNodeState{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Annotations: map[string]string{
							PodNameAnnotation: "test-pod",
						},
					},
				},
			},
			expectedDelete: false,
		},
		{
			name: "pod doesn't exist, FRRNodeState should be deleted",
			pod:  nil,
			frrNodeStates: []frrk8sv1beta1.FRRNodeState{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Annotations: map[string]string{
							PodNameAnnotation: "test-pod",
						},
					},
				},
			},
			expectedDelete: true,
		},
		{
			name: "FRRNodeState without pod annotation, no action",
			pod:  nil,
			frrNodeStates: []frrk8sv1beta1.FRRNodeState{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
					},
				},
			},
			expectedDelete: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := createTestClient(t)

			if tt.pod != nil {
				if err := client.Create(context.Background(), tt.pod); err != nil {
					t.Fatalf("failed to create pod: %v", err)
				}
			}

			for _, nodeState := range tt.frrNodeStates {
				if err := client.Create(context.Background(), &nodeState); err != nil {
					t.Fatalf("failed to create FRRNodeState: %v", err)
				}
			}

			reconciler := &NodeStateCleaner{
				Client: client,
				Logger: log.NewNopLogger(),
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
			}

			if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
				t.Fatalf("reconcile failed: %v", err)
			}

			for _, nodeState := range tt.frrNodeStates {
				var retrieved frrk8sv1beta1.FRRNodeState
				err := client.Get(context.Background(), types.NamespacedName{Name: nodeState.Name}, &retrieved)

				if tt.expectedDelete && err == nil {
					t.Error("FRRNodeState should have been deleted")
				}

				if !tt.expectedDelete && err != nil {
					t.Errorf("FRRNodeState should not have been deleted: %v", err)
				}
			}
		})
	}
}

func createTestClient(t *testing.T) clientWithScheme {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 to scheme: %v", err)
	}
	if err := frrk8sv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add frrk8sv1beta1 to scheme: %v", err)
	}
	return clientWithScheme{Client: fake.NewClientBuilder().WithScheme(scheme).Build(), scheme: scheme}
}

type clientWithScheme struct {
	client.Client
	scheme *runtime.Scheme
}

func (c clientWithScheme) Scheme() *runtime.Scheme {
	return c.scheme
}
