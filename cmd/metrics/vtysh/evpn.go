// SPDX-License-Identifier:Apache-2.0

package vtysh

import (
	"github.com/metallb/frr-k8s/internal/frr"
)

func GetEVPNVNIs(frrCli Cli) ([]frr.EVPNVNI, error) {
	res, err := frrCli("show evpn vni json")
	if err != nil {
		return nil, err
	}
	return frr.ParseEVPNVNIs(res)
}
