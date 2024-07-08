// SPDX-License-Identifier:Apache-2.0

package k8s

import (
	"context"

	"errors"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

const labelSelector = "app=frr-k8s"

// FRRK8sPods returns the set of pods related to FRR-K8s.
func FRRK8sPods(cs clientset.Interface) ([]*corev1.Pod, error) {
	pods, err := cs.CoreV1().Pods(FRRK8sNamespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, errors.Join(err, errors.New("failed to fetch frrk8s pods"))
	}
	if len(pods.Items) == 0 {
		return nil, errors.New("No frrk8s pods found")
	}
	res := make([]*corev1.Pod, 0)
	for _, item := range pods.Items {
		i := item
		res = append(res, &i)
	}
	return res, nil
}
