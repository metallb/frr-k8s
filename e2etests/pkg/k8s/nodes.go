// SPDX-License-Identifier:Apache-2.0

package k8s

import (
	"context"
	"fmt"

	"errors"

	e2ek8s "go.universe.tf/e2etest/pkg/k8s"

	"go.universe.tf/e2etest/pkg/ipfamily"
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

// NodeIPsForFamily returns the node IPs for the given IP family in the default
// VRF.
func NodeIPsForFamily(nodes []corev1.Node, family ipfamily.Family) ([]string, error) {
	return e2ek8s.NodeIPsForFamily(nodes, family, "")
}

// NodeIPForFamily returns the IP of a single node for the given IP family in
// the default VRF.
func NodeIPForFamily(node corev1.Node, family ipfamily.Family) (string, error) {
	ips, err := e2ek8s.NodeIPsForFamily([]corev1.Node{node}, family, "")
	if err != nil {
		return "", err
	}
	if len(ips) == 0 {
		return "", fmt.Errorf("no %s IP found for node %s", family, node.Name)
	}
	return ips[0], nil
}
