// SPDX-License-Identifier:Apache-2.0

package vtysh

import (
	"os/exec"

	"github.com/metallb/frr-k8s/internal/frr"
)

type Cli func(args string) (string, error)

func Run(args string) (string, error) {
	out, err := exec.Command("/usr/bin/vtysh", "-c", args).CombinedOutput()
	return string(out), err
}

var _ Cli = Run

func VRFs(frrCli Cli) ([]string, error) {
	vrfs, err := frrCli("show bgp vrf all json")
	if err != nil {
		return nil, err
	}
	parsedVRFs, err := frr.ParseVRFs(vrfs)
	if err != nil {
		return nil, err
	}
	return parsedVRFs, nil
}
