// SPDX-License-Identifier:Apache-2.0

package k8s

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		return nil, fmt.Errorf("Expected exactly one frr-k8s daemonset, got %d", len(daemonSets.Items))
	}
	return &daemonSets.Items[0], nil
}
