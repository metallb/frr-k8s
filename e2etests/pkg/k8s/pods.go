// SPDX-License-Identifier:Apache-2.0

package k8s

import (
	"context"
	"fmt"
	"slices"
	"time"

	"errors"

	"github.com/onsi/ginkgo/v2"
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

func RestartFRRK8sPods(cs clientset.Interface) error {
	pods, err := FRRK8sPods(cs)
	if err != nil {
		return err
	}
	oldNames := []string{}
	for _, p := range pods {
		oldNames = append(oldNames, p.Name)
		err := cs.CoreV1().Pods(FRRK8sNamespace).Delete(context.Background(), p.Name, metav1.DeleteOptions{})
		if err != nil {
			return err
		}
	}

	return wait.PollUntilContextTimeout(context.Background(), 10*time.Second,
		120*time.Second, false, func(context.Context) (bool, error) {
			npods, err := FRRK8sPods(cs)
			if err != nil {
				return false, err
			}
			if len(npods) != len(pods) {
				return false, nil
			}
			for _, p := range npods {
				if slices.Contains(oldNames, p.Name) {
					return false, nil
				}
				if !podIsRunningAndReady(p) {
					ginkgo.By(fmt.Sprintf("\t%v pod %s not ready", time.Now(), p.Name))
					return false, nil
				}
			}
			ginkgo.By(fmt.Sprintf("\tfrrk8s pod ARE ready"))
			return true, nil
		})
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
