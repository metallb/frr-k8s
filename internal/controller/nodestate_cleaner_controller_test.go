// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"context"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frr-k8s/internal/logging"
)

const testFRRPodLabel = "app.kubernetes.io/component"

var _ = Describe("NodeStateCleaner controller", func() {
	const (
		staleNodeName  = "nodestate-cleaner-stale"
		podNodeName    = "nodestate-cleaner-pod"
		podName        = "nodestate-cleaner-frr-pod"
		controllerWait = 5 * time.Second
	)

	newCleanerManager := func() (context.CancelFunc, error) {
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:  scheme.Scheme,
			Metrics: metricsserver.Options{BindAddress: "0"},
			// The suite already registers an FRRNodeState controller in another
			// manager; controller names are tracked across the entire process.
			Controller: config.Controller{SkipNameValidation: ptr.To(true)},
		})
		if err != nil {
			return nil, err
		}
		cleaner := &NodeStateCleaner{
			Client:         mgr.GetClient(),
			Scheme:         mgr.GetScheme(),
			Namespace:      testNamespace,
			FRRK8sSelector: labels.SelectorFromSet(map[string]string{testFRRPodLabel: "frr-k8s"}),
		}
		if err := cleaner.SetupWithManager(mgr); err != nil {
			return nil, err
		}
		managerContext, cancel := context.WithCancel(context.Background())
		stopped := make(chan struct{})
		go func() {
			defer GinkgoRecover()
			defer close(stopped)
			Expect(mgr.Start(managerContext)).To(Succeed())
		}()
		return func() {
			cancel()
			Eventually(stopped, controllerWait).Should(BeClosed())
		}, nil
	}

	AfterEach(func() {
		for _, name := range []string{staleNodeName, podNodeName} {
			state := &frrk8sv1beta1.FRRNodeState{ObjectMeta: metav1.ObjectMeta{Name: name}}
			Expect(client.IgnoreNotFound(k8sClient.Delete(context.Background(), state))).To(Succeed())
		}
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: podName}}
		Expect(client.IgnoreNotFound(k8sClient.Delete(context.Background(), pod, client.GracePeriodSeconds(0)))).To(Succeed())
	})

	It("removes a stale node state from the initial cache list", func() {
		state := &frrk8sv1beta1.FRRNodeState{ObjectMeta: metav1.ObjectMeta{Name: staleNodeName}}
		Expect(k8sClient.Create(context.Background(), state)).To(Succeed())

		cancel, err := newCleanerManager()
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cancel)

		Eventually(func() bool {
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: staleNodeName}, &frrk8sv1beta1.FRRNodeState{})
			return k8serrors.IsNotFound(err)
		}, controllerWait).Should(BeTrue())
	})

	It("removes a node state after the last matching FRR pod is deleted", func() {
		pod := testPod(podName, podNodeName, "frr-k8s")
		pod.Namespace = testNamespace
		pod.Spec.Containers = []corev1.Container{{Name: "frr", Image: "frr-k8s:test"}}
		Expect(k8sClient.Create(context.Background(), pod)).To(Succeed())
		state := &frrk8sv1beta1.FRRNodeState{ObjectMeta: metav1.ObjectMeta{Name: podNodeName}}
		Expect(k8sClient.Create(context.Background(), state)).To(Succeed())

		cancel, err := newCleanerManager()
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cancel)
		Consistently(func() error {
			return k8sClient.Get(context.Background(), client.ObjectKey{Name: podNodeName}, &frrk8sv1beta1.FRRNodeState{})
		}, time.Second).Should(Succeed())

		Expect(k8sClient.Delete(context.Background(), pod, client.GracePeriodSeconds(0))).To(Succeed())
		Eventually(func() bool {
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: podNodeName}, &frrk8sv1beta1.FRRNodeState{})
			return k8serrors.IsNotFound(err)
		}, controllerWait).Should(BeTrue())
	})
})

func TestNodeStateCleanerReconcile(t *testing.T) {
	tests := []struct {
		name      string
		request   string
		pods      []*corev1.Pod
		nodeState []string
		deleted   []string
	}{
		{
			name:      "matching FRR pod keeps node state",
			request:   "node1",
			pods:      []*corev1.Pod{testPod("frr-node1", "node1", "frr-k8s")},
			nodeState: []string{"node1"},
		},
		{
			name:      "missing FRR pod deletes node state",
			request:   "node1",
			nodeState: []string{"node1"},
			deleted:   []string{"node1"},
		},
		{
			name:      "FRR pod on another node does not protect node state",
			request:   "node1",
			pods:      []*corev1.Pod{testPod("frr-node2", "node2", "frr-k8s")},
			nodeState: []string{"node1", "node2"},
			deleted:   []string{"node1"},
		},
		{
			name:      "non matching pod does not protect node state",
			request:   "node1",
			pods:      []*corev1.Pod{testPod("other-node1", "node1", "other")},
			nodeState: []string{"node1"},
			deleted:   []string{"node1"},
		},
		{
			name:      "reconcile only checks requested node",
			request:   "node1",
			nodeState: []string{"node1", "node2", "node3"},
			deleted:   []string{"node1"},
		},
		{
			name:    "missing node state is a no-op",
			request: "node1",
		},
	}

	if err := logging.InitWithWriter(os.Stdout); err != nil {
		t.Fatalf("building logger failed: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := make([]client.Object, 0, len(tt.pods)+len(tt.nodeState))
			for _, pod := range tt.pods {
				objects = append(objects, pod)
			}
			for _, name := range tt.nodeState {
				objects = append(objects, &frrk8sv1beta1.FRRNodeState{ObjectMeta: metav1.ObjectMeta{Name: name}})
			}
			k8sClient := createNodeStateCleanerTestClient(t, objects...)
			reconciler := &NodeStateCleaner{
				Client:         k8sClient,
				Namespace:      "test-namespace",
				FRRK8sSelector: labels.SelectorFromSet(map[string]string{testFRRPodLabel: "frr-k8s"}),
			}
			if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: tt.request}}); err != nil {
				t.Fatalf("reconcile failed: %v", err)
			}
			deleted := make(map[string]struct{}, len(tt.deleted))
			for _, name := range tt.deleted {
				deleted[name] = struct{}{}
			}
			for _, name := range tt.nodeState {
				state := &frrk8sv1beta1.FRRNodeState{}
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: name}, state)
				_, wantDeleted := deleted[name]
				if wantDeleted && !k8serrors.IsNotFound(err) {
					t.Errorf("FRRNodeState %s was not deleted: %v", name, err)
				}
				if !wantDeleted && err != nil {
					t.Errorf("FRRNodeState %s should remain: %v", name, err)
				}
			}
		})
	}
}

func testPod(name, nodeName, component string) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test-namespace", Labels: map[string]string{testFRRPodLabel: component}}, Spec: corev1.PodSpec{NodeName: nodeName}}
}

func createNodeStateCleanerTestClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 to scheme: %v", err)
	}
	if err := frrk8sv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add frrk8sv1beta1 to scheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithIndex(&corev1.Pod{}, podNodeNameField, podNodeNameIndex).Build()
}
