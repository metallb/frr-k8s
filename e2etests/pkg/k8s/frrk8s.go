// SPDX-License-Identifier:Apache-2.0

package k8s

import (
	"context"
	"errors"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
)

func FRRK8sDaemonSet(cs clientset.Interface) (*appsv1.DaemonSet, error) {
	daemonSets, err := cs.AppsV1().DaemonSets(FRRK8sNamespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/component=frr-k8s",
	})
	if err != nil {
		return nil, err
	}
	if len(daemonSets.Items) != 1 {
		return nil, fmt.Errorf("expected exactly one frr-k8s daemonset, got %d", len(daemonSets.Items))
	}
	frrK8sDaemonSet := &daemonSets.Items[0]

	err = wait.PollUntilContextTimeout(context.Background(), 5*time.Second, 2*time.Minute, true,
		func(ctx context.Context) (bool, error) {
			ds, err := cs.AppsV1().DaemonSets(FRRK8sNamespace).Get(ctx, frrK8sDaemonSet.Name, metav1.GetOptions{})
			if err != nil {
				return false, err
			}

			if ds.Status.NumberReady == ds.Status.DesiredNumberScheduled && ds.Status.NumberUnavailable == 0 {
				frrK8sDaemonSet = ds
				return true, nil
			}
			return false, nil
		})

	if err != nil {
		return nil, fmt.Errorf("timed out waiting for frr-k8s daemonset to be ready: %w", err)
	}
	return frrK8sDaemonSet, nil
}

// frrK8SStatusCleanerPods returns the set of pods related to FRR-K8s StatusCleaner / the webhook-server.
func FRRK8SStatusCleanerPods(cs clientset.Interface, namespace string) ([]*corev1.Pod, error) {
	pods, err := cs.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: FRRK8sStatusCleanerApp,
	})
	if err != nil {
		return nil, errors.Join(err, errors.New("Failed to fetch frr-k8s pods"))
	}
	if len(pods.Items) == 0 {
		return nil, errors.New("No frr-k8s pods found")
	}
	res := make([]*corev1.Pod, 0)
	for _, item := range pods.Items {
		i := item
		res = append(res, &i)
	}
	return res, nil
}
