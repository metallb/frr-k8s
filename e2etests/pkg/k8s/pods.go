// SPDX-License-Identifier:Apache-2.0

package k8s

import (
	"context"
	"fmt"
	"time"

	"errors"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
)

const (
	FRRK8sLabelSelector = "control-plane=frr-k8s"
	labelSelector       = "app=frr-k8s"
)

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

func RestartFRRK8sPodForNode(cs clientset.Interface, nodeName string) error {
	oldPod, err := podForNode(cs, nodeName)
	if err != nil {
		return err
	}
	if err := cs.CoreV1().Pods(FRRK8sNamespace).Delete(context.Background(), oldPod.Name, metav1.DeleteOptions{}); err != nil {
		return err
	}

	return wait.PollUntilContextTimeout(context.Background(), 10*time.Second,
		120*time.Second, false, func(context.Context) (bool, error) {
			newPod, err := podForNode(cs, nodeName)
			if err != nil {
				return false, err
			}

			if newPod.Name == oldPod.Name {
				return false, fmt.Errorf("pod was not deleted")
			}

			if !podIsRunningAndReady(newPod) {
				return false, nil
			}

			return true, nil
		})
}

func podForNode(cs clientset.Interface, nodeName string) (*corev1.Pod, error) {
	pods, err := FRRK8sPods(cs)
	if err != nil {
		return nil, err
	}
	for _, p := range pods {
		if p.Spec.NodeName == nodeName {
			return p, nil
		}
	}
	return nil, fmt.Errorf("no frr-k8s pod found on node %s", nodeName)
}

func podIsRunningAndReady(pod *v1.Pod) bool {
	if pod.Status.Phase != v1.PodRunning {
		return false
	}

	for _, containerStatus := range pod.Status.ContainerStatuses {
		if !containerStatus.Ready {
			return false
		}
	}

	return true
}
