// SPDX-License-Identifier:Apache-2.0

package vtysh

import (
	"encoding/json"
	"fmt"

	"github.com/metallb/frr-k8s/internal/frr"
)

func GetBFDPeers(frrCli Cli) (map[string][]frr.BFDPeer, error) {
	vrfs, err := VRFs(frrCli)
	if err != nil {
		return nil, err
	}
	res := make(map[string][]frr.BFDPeer)
	for _, vrf := range vrfs {
		peersJSON, err := frrCli(fmt.Sprintf("show bfd vrf %s peers json", vrf))
		if err != nil {
			return nil, err
		}
		peers, err := frr.ParseBFDPeers(peersJSON)
		if err != nil {
			return nil, err
		}
		res[vrf] = peers
	}
	return res, nil
}

func GetBFDPeersCounters(frrCli Cli) (map[string][]frr.BFDPeerCounters, error) {
	vrfs, err := VRFs(frrCli)
	if err != nil {
		return nil, err
	}

	res := make(map[string][]frr.BFDPeerCounters)
	for _, vrf := range vrfs {
		countersJSON, err := frrCli(fmt.Sprintf("show bfd vrf %s peers counters json", vrf))
		if err != nil {
			return nil, err
		}

		parseRes := []frr.BFDPeerCounters{}
		err = json.Unmarshal([]byte(countersJSON), &parseRes)
		if err != nil {
			return nil, err
		}
		res[vrf] = parseRes
	}
	return res, nil
}
