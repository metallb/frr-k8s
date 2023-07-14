// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"fmt"
	"time"

	"github.com/metallb/frrk8stests/pkg/routes"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.universe.tf/e2etest/pkg/frr"
	frrcontainer "go.universe.tf/e2etest/pkg/frr/container"
	"go.universe.tf/e2etest/pkg/ipfamily"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/kubernetes/test/e2e/framework"
)

func ValidateFRRNotPeeredWithNodes(nodes []corev1.Node, c *frrcontainer.FRR, ipFamily ipfamily.Family) {
	for _, node := range nodes {
		ginkgo.By(fmt.Sprintf("checking node %s is not peered with the frr instance %s", node.Name, c.Name))
		Eventually(func() error {
			neighbors, err := frr.NeighborsInfo(c)
			framework.ExpectNoError(err)
			err = frr.NeighborsMatchNodes([]corev1.Node{node}, neighbors, ipFamily, c.RouterConfig.VRF)
			return err
		}, 4*time.Minute, 1*time.Second).Should(MatchError(ContainSubstring("not established")))
	}
}

func ValidateFRRPeeredWithNodes(nodes []corev1.Node, c *frrcontainer.FRR, ipFamily ipfamily.Family) {
	ginkgo.By(fmt.Sprintf("checking nodes are peered with the frr instance %s", c.Name))
	Eventually(func() error {
		neighbors, err := frr.NeighborsInfo(c)
		framework.ExpectNoError(err)
		err = frr.NeighborsMatchNodes(nodes, neighbors, ipFamily, c.RouterConfig.VRF)
		if err != nil {
			return fmt.Errorf("failed to match neighbors for %s, %w", c.Name, err)
		}
		return nil
	}, 4*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
}

func ValidatePrefixesForNeighbor(neigh frrcontainer.FRR, nodes []v1.Node, prefixes ...string) {
	ginkgo.By(fmt.Sprintf("checking prefixes %v for %s", prefixes, neigh.Name))
	Eventually(func() error {
		for _, prefix := range prefixes {
			found, err := routes.CheckNeighborHasPrefix(neigh, prefix, nodes)
			framework.ExpectNoError(err)
			if !found {
				fmt.Errorf("Neigh %s does not have prefix %s", neigh.Name, prefix)
			}
		}
		return nil
	}, time.Minute, time.Second).ShouldNot(HaveOccurred())
}

func ValidateNeighborNoPrefixes(neigh frrcontainer.FRR, nodes []v1.Node, prefixes ...string) {
	ginkgo.By(fmt.Sprintf("checking prefixes %v not announced to %s", prefixes, neigh.Name))
	Eventually(func() error {
		for _, prefix := range prefixes {
			found, err := routes.CheckNeighborHasPrefix(neigh, prefix, nodes)
			framework.ExpectNoError(err)
			if found {
				fmt.Errorf("Neigh %s has prefix %s", neigh.Name, prefix)
			}
		}
		return nil
	}, 5*time.Second, time.Second).ShouldNot(HaveOccurred())
}

func ValidateNodesHaveRoutes(pods []*v1.Pod, neigh frrcontainer.FRR, prefixes ...string) {
	ginkgo.By(fmt.Sprintf("Checking routes %v from %s", prefixes, neigh.Name))
	Eventually(func() error {
		for _, prefix := range prefixes {
			for _, pod := range pods {
				if !routes.PodHasPrefixFromContainer(pod, neigh, prefix) {
					return fmt.Errorf("pod %s does not have prefix %s from %s", pod.Name, prefix, neigh.Name)
				}
			}
		}
		return nil
	}, time.Minute, time.Second).ShouldNot(HaveOccurred())
}

func ValidateNodesDoNotHaveRoutes(pods []*v1.Pod, neigh frrcontainer.FRR, prefixes ...string) {
	ginkgo.By(fmt.Sprintf("Checking routes %v not injected from %s", prefixes, neigh.Name))
	Consistently(func() error {
		for _, prefix := range prefixes {
			for _, pod := range pods {
				if routes.PodHasPrefixFromContainer(pod, neigh, prefix) {
					return fmt.Errorf("pod %s has prefix %s from %s", pod.Name, prefix, neigh.Name)
				}
			}
		}
		return nil
	}, 5*time.Second, time.Second).ShouldNot(HaveOccurred())
}
