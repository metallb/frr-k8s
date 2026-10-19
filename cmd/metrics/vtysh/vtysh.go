// SPDX-License-Identifier:Apache-2.0

package vtysh

import (
	"os/exec"

	"github.com/metallb/frr-k8s/internal/frr"
)

type Cli func(args string) (string, error)

func Run(args string) (string, error) {
	out, err := exec.Command("/usr/bin/vtysh", "-c", args).CombinedOutput() //#nosec G204 -- args are hardcoded vtysh commands from within this package
	return string(out), err
}

var _ Cli = Run

func VRFs(frrCli Cli) ([]string, error) {
	// "show bgp vrfs json" lists the VRFs only. "show bgp vrf all json" would
	// dump every VRF's full BGP table, which on a full-table router means
	// hundreds of MB of JSON and seconds of bgpd CPU per call.
	vrfs, err := frrCli("show bgp vrfs json")
	if err != nil {
		return nil, err
	}
	parsedVRFs, err := frr.ParseVRFs(vrfs)
	if err != nil {
		return nil, err
	}
	return parsedVRFs, nil
}
