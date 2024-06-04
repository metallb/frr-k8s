// SPDX-License-Identifier:Apache-2.0

package k8s

import (
	"context"

	"errors"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

// Nodes returns all nodes in the cluster.
func Nodes(cs clientset.Interface) ([]corev1.Node, error) {
	nodes, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, errors.Join(err, errors.New("failed to fetch frrk8s nodes"))
	}
	return nodes.Items, nil
}
