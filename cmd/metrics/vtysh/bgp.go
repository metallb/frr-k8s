// SPDX-License-Identifier:Apache-2.0

package vtysh

import (
	"fmt"

	"github.com/metallb/frr-k8s/internal/frr"
)

func GetBGPNeighbors(frrCli Cli) (map[string][]*frr.Neighbor, error) {
	vrfs, err := VRFs(frrCli)
	if err != nil {
		return nil, err
	}
	neighbors := make(map[string][]*frr.Neighbor, 0)
	for _, vrf := range vrfs {
		res, err := frrCli(fmt.Sprintf("show bgp vrf %s neighbors json", vrf))
		if err != nil {
			return nil, err
		}

		neighborsPerVRF, err := frr.ParseNeighbours(res)
		if err != nil {
			return nil, err
		}
		neighbors[vrf] = neighborsPerVRF
	}
	return neighbors, nil
}
