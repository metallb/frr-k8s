// SPDX-License-Identifier:Apache-2.0

package vtysh

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func cliScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vtysh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunKeepsDiagnosticsOutOfJSON(t *testing.T) {
	path := cliScript(t, `printf '{"peers": []}\n'; printf 'warning\n' >&2`)
	out, err := run(context.Background(), path, "show bgp vrfs json")
	if err != nil || out != "{\"peers\": []}\n" {
		t.Fatalf("output %q, error %v", out, err)
	}
}

func TestRunReportsCommandFailure(t *testing.T) {
	for _, tt := range []struct {
		name, script, command, diagnostic string
		exitCode                          int
	}{
		{"stderr", `printf 'failed to read daemon\n' >&2; exit 7`, "show bfd vrf default peers json", "failed to read daemon", 7},
		{"stdout", `printf '%% Unknown command: show evpn vni json\n'; exit 1`, "show evpn vni json", "% Unknown command: show evpn vni json", 1},
		{"no output", `exit 1`, "show evpn vni json", "", 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := cliScript(t, tt.script)
			_, err := run(context.Background(), path, tt.command)
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != tt.exitCode {
				t.Fatalf("missing exit status %d: %v", tt.exitCode, err)
			}
			want := fmt.Sprintf("vtysh %q: exit status %d", tt.command, tt.exitCode)
			if tt.diagnostic != "" {
				want += ": " + tt.diagnostic
			}
			if err.Error() != want {
				t.Fatalf("error %q, want %q", err, want)
			}
		})
	}
}

func TestRunStopsWhenContextEnds(t *testing.T) {
	path := cliScript(t, `exec sleep 60`)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := run(ctx, path, "show bgp vrfs json"); err == nil {
		t.Fatal("hung vtysh returned no error")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("hung vtysh ran for %v after its context ended", elapsed)
	}
}
