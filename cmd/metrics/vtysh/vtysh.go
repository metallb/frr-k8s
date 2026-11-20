// SPDX-License-Identifier:Apache-2.0

package vtysh

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/metallb/frr-k8s/internal/frr"
)

type Cli func(args string) (string, error)

func Run(args string) (string, error) {
	return RunContext(context.Background(), args)
}

// RunContext is Run, killing vtysh when ctx ends, so that a hung FRR daemon
// cannot leave a vtysh behind for every scrape.
func RunContext(ctx context.Context, args string) (string, error) {
	return run(ctx, "/usr/bin/vtysh", args)
}

func run(ctx context.Context, path, command string) (string, error) {
	cmd := exec.CommandContext(ctx, path, "-c", command) //#nosec G204 -- args are hardcoded vtysh commands from within this package
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := errorOrOutput(stderr.String(), string(out)); msg != "" {
			return string(out), fmt.Errorf("vtysh %q: %w: %s", command, err, msg)
		}
		return string(out), fmt.Errorf("vtysh %q: %w", command, err)
	}
	return string(out), nil
}

// errorOrOutput returns the trimmed stderr, or stdout when stderr is empty,
// since vtysh prints errors such as "% Unknown command" to stdout.
func errorOrOutput(stderr, stdout string) string {
	if msg := strings.TrimSpace(stderr); msg != "" {
		return limitString(msg)
	}
	if msg := strings.TrimSpace(stdout); msg != "" {
		return limitString(msg)
	}
	return ""
}

func limitString(s string) string {
	const limit = 256
	if len(s) > limit {
		return s[:limit] + "..."
	}
	return s
}

var _ Cli = Run

func VRFs(frrCli Cli) ([]string, error) {
	// List VRFs without their routing tables.
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
