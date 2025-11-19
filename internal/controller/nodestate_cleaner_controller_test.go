// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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
		pods           []*corev1.Pod
		frrNodeStates  []frrk8sv1beta1.FRRNodeState
		expectedDelete bool
	}{
		{
			name: "FRR-K8s pod exists on node, FRRNodeState should not be deleted",
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "frr-k8s-pod",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app.kubernetes.io/component": "frr-k8s",
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "node1",
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				},
			},
			frrNodeStates: []frrk8sv1beta1.FRRNodeState{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
					},
				},
			},
			expectedDelete: false,
		},
		{
			name: "no FRR-K8s pods on node, FRRNodeState should be deleted",
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-pod",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app.kubernetes.io/component": "other-component",
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "node1",
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				},
			},
			frrNodeStates: []frrk8sv1beta1.FRRNodeState{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
					},
				},
			},
			expectedDelete: true,
		},
		{
			name: "FRR-K8s pod on different node, FRRNodeState should be deleted",
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "frr-k8s-pod",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app.kubernetes.io/component": "frr-k8s",
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "node2",
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				},
			},
			frrNodeStates: []frrk8sv1beta1.FRRNodeState{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
					},
				},
			},
			expectedDelete: true,
		},
		{
			name: "multiple FRR-K8s pods on different nodes",
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "frr-k8s-pod-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app.kubernetes.io/component": "frr-k8s",
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "node1",
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "frr-k8s-pod-2",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app.kubernetes.io/component": "frr-k8s",
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "node2",
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				},
			},
			frrNodeStates: []frrk8sv1beta1.FRRNodeState{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node2",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node3",
					},
				},
			},
			expectedDelete: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := createTestClient(t)

			for _, pod := range tt.pods {
				if err := client.Create(context.Background(), pod); err != nil {
					t.Fatalf("failed to create pod: %v", err)
				}
			}

			for _, nodeState := range tt.frrNodeStates {
				if err := client.Create(context.Background(), &nodeState); err != nil {
					t.Fatalf("failed to create FRRNodeState: %v", err)
				}
			}

			reconciler := &NodeStateCleaner{
				Client:    client,
				Logger:    log.NewNopLogger(),
				Namespace: "test-namespace",
				FRRK8sSelector: labels.SelectorFromSet(map[string]string{
					"app.kubernetes.io/component": "frr-k8s",
				}),
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

				hasFRRPods := false
				for _, pod := range tt.pods {
					if pod.Spec.NodeName == nodeState.Name && pod.Labels["app.kubernetes.io/component"] == "frr-k8s" {
						hasFRRPods = true
						break
					}
				}

				if !hasFRRPods && err == nil {
					t.Errorf("FRRNodeState for node %s should have been deleted", nodeState.Name)
				}

				if hasFRRPods && err != nil {
					t.Errorf("FRRNodeState for node %s should not have been deleted: %v", nodeState.Name, err)
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
