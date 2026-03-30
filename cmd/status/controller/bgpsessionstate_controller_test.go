// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frr-k8s/internal/frr"
)

var (
	fakeBGP = &fakeBGPFetcher{m: make(map[string][]*frr.Neighbor)}
)

type fakeBGPFetcher struct {
	m map[string][]*frr.Neighbor
}

func (f *fakeBGPFetcher) GetBGPNeighbors() (map[string][]*frr.Neighbor, error) {
	return f.m, nil
}

func (f fakeBGPFetcher) expectedStatuses() map[string]map[string]frrk8sv1beta1.BGPSessionStateStatus {
	res := map[string]map[string]frrk8sv1beta1.BGPSessionStateStatus{}

	for vrf, bgpPeers := range f.m {
		res[vrf] = map[string]frrk8sv1beta1.BGPSessionStateStatus{}
		for _, bgpPeer := range bgpPeers {
			bfdStatus := bgpPeer.BFDStatus
			if bfdStatus == "" {
				bfdStatus = noBFDConfigured
			}
			res[vrf][labelFormatForNeighbor(bgpPeer.ID)] = frrk8sv1beta1.BGPSessionStateStatus{
				Node:      testNodeName,
				Peer:      labelFormatForNeighbor(bgpPeer.ID),
				VRF:       vrf,
				BGPStatus: bgpPeer.BGPState,
				BFDStatus: bfdStatus,
			}
		}
	}

	res[""] = map[string]frrk8sv1beta1.BGPSessionStateStatus{}
	for k, v := range res["default"] {
		v.VRF = ""
		res[""][k] = v
	}
	delete(res, "default")

	return res
}

func (f *fakeBGPFetcher) Matches(l frrk8sv1beta1.BGPSessionStateList) error {
	expected := f.expectedStatuses()
	for _, s := range l.Items {
		if s.Status.Peer == "" {
			return fmt.Errorf("status not populated for resource %v", s)
		}
		if _, ok := expected[s.Status.VRF][s.Status.Peer]; !ok {
			return fmt.Errorf("no matching resource for %v \nexpected statuses are %v", s, expected)
		}
		delete(expected[s.Status.VRF], s.Status.Peer)
	}
	for _, statuses := range expected {
		if len(statuses) != 0 {
			return fmt.Errorf("not all expected resources matches, leftover: %v", expected)
		}
	}
	return nil
}

var _ = Describe("BGPSessionState Controller", func() {
	Context("SetupWithManager", func() {
		It("should reconcile correctly", func() {
			fakeBGP.m = map[string][]*frr.Neighbor{
				"default": {
					{
						ID:        "192.168.1.1",
						BGPState:  "Active",
						BFDStatus: "down",
					},
					{
						ID:       "192.168.1.2",
						BGPState: "Active",
					},
					{
						ID:       "fc00:f853:ccd:e899::",
						BGPState: "Active",
					},
					{
						ID:       "eth0",
						BGPState: "Active",
					},
				},
				"red": {
					{
						ID:        "192.168.1.1",
						BGPState:  "Active",
						BFDStatus: "down",
					},
				},
			}

			Eventually(func() error {
				l := frrk8sv1beta1.BGPSessionStateList{}
				err := k8sClient.List(context.Background(), &l)
				if err != nil {
					return err
				}
				return fakeBGP.Matches(l)
			}, 5*time.Second, time.Second).ShouldNot(HaveOccurred())

			By("Updating the first peer's inner BGP and BFD state")
			fakeBGP.m = map[string][]*frr.Neighbor{
				"default": {
					{
						ID:        "192.168.1.1",
						BGPState:  "Established",
						BFDStatus: "Up",
					},
					{
						ID:       "192.168.1.2",
						BGPState: "Active",
					},
					{
						ID:       "fc00:f853:ccd:e899::",
						BGPState: "Active",
					},
					{
						ID:       "eth0",
						BGPState: "Active",
					},
				},
				"red": {
					{
						ID:        "192.168.1.1",
						BGPState:  "Active",
						BFDStatus: "Down",
					},
				},
			}

			Eventually(func() error {
				l := frrk8sv1beta1.BGPSessionStateList{}
				err := k8sClient.List(context.Background(), &l)
				if err != nil {
					return err
				}
				return fakeBGP.Matches(l)
			}, 5*time.Second, time.Second).ShouldNot(HaveOccurred())

			By("Removing the second+third+fourth peers and updating the fifth")
			fakeBGP.m = map[string][]*frr.Neighbor{
				"default": {
					{
						ID:        "192.168.1.1",
						BGPState:  "Established",
						BFDStatus: "Up",
					},
				},
				"red": {
					{
						ID:        "192.168.1.1",
						BGPState:  "Established",
						BFDStatus: "Up",
					},
				},
			}

			Eventually(func() error {
				l := frrk8sv1beta1.BGPSessionStateList{}
				err := k8sClient.List(context.Background(), &l)
				if err != nil {
					return err
				}
				return fakeBGP.Matches(l)
			}, 5*time.Second, time.Second).ShouldNot(HaveOccurred())

		})
	})
})
