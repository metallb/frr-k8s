// SPDX-License-Identifier:Apache-2.0

package helmchart

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/util/yaml"
)

const chartPath = "../../charts/frr-k8s"

// helmBinary returns the helm binary to render the chart with: the one in the
// HELM environment variable, which "make test" sets, or the one in the PATH.
// The test is skipped when HELM is not set and there is no helm in the PATH.
func helmBinary(t *testing.T) string {
	t.Helper()
	if helm := os.Getenv("HELM"); helm != "" {
		// The test runs in the directory of the package, so a path relative
		// to the root of the repository would not be found.
		if strings.ContainsRune(helm, filepath.Separator) && !filepath.IsAbs(helm) {
			t.Fatalf("HELM must be an absolute path or a command name, got %q", helm)
		}
		path, err := exec.LookPath(helm)
		if err != nil {
			t.Fatalf("HELM is set but cannot be used: %v", err)
		}
		return path
	}
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm not found: set HELM or run the tests with \"make test\"")
	}
	return helm
}

// trackChartFiles walks the chart, which makes its files inputs of the test
// for the go test cache: without it a cached result would be reused after a
// change to the chart, since only helm reads those files.
func trackChartFiles(t *testing.T) {
	t.Helper()
	err := filepath.WalkDir(chartPath, func(_ string, _ fs.DirEntry, err error) error {
		return err
	})
	if err != nil {
		t.Fatalf("failed to walk the chart: %v", err)
	}
}

// renderServiceMonitor renders the chart with the given values on top of the
// one that enables the ServiceMonitor, and returns its endpoints.
func renderServiceMonitor(t *testing.T, helm, values string) []map[string]any {
	t.Helper()
	cmd := exec.Command(helm, "template", "frrk8s", chartPath,
		"--namespace", "frr-k8s-system",
		"--show-only", "templates/service-monitor.yaml",
		"--set", "prometheus.serviceMonitor.enabled=true",
		"--values", "-")
	var stdout, stderr bytes.Buffer
	cmd.Stdin = strings.NewReader(values)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, stderr.String())
	}

	rendered := stdout.String()
	decoder := yaml.NewYAMLOrJSONDecoder(&stdout, 4096)
	for {
		var obj struct {
			Kind string `json:"kind"`
			Spec struct {
				Endpoints []map[string]any `json:"endpoints"`
			} `json:"spec"`
		}
		err := decoder.Decode(&obj)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("failed to parse the rendered manifests: %v\n%s", err, rendered)
		}
		if obj.Kind == "ServiceMonitor" {
			return obj.Spec.Endpoints
		}
	}
	t.Fatalf("no ServiceMonitor in the rendered manifests:\n%s", rendered)
	return nil
}

func TestServiceMonitorTLSConfig(t *testing.T) {
	const withSecret = "tls:\n  metricsTLSSecret: metrics-certs\n"

	tests := []struct {
		name   string
		values string
		// want is the tlsConfig expected on every endpoint, nil when the
		// endpoints must not have one.
		want map[string]any
	}{
		{
			name: "self-signed certificate, verification skipped",
			want: map[string]any{"insecureSkipVerify": true},
		},
		{
			name:   "TLS secret, verification on",
			values: withSecret,
		},
		{
			name: "TLS secret with ca and serverName",
			values: withSecret + `
prometheus:
  serviceMonitor:
    tlsConfig:
      serverName: frr-k8s-metrics
      ca:
        secret:
          name: metrics-certs
          key: ca.crt
`,
			want: map[string]any{
				"serverName": "frr-k8s-metrics",
				"ca": map[string]any{
					"secret": map[string]any{"name": "metrics-certs", "key": "ca.crt"},
				},
			},
		},
		{
			name: "self-signed certificate, other tlsConfig fields are kept",
			values: `
prometheus:
  serviceMonitor:
    tlsConfig:
      serverName: frr-k8s-metrics
`,
			want: map[string]any{"insecureSkipVerify": true, "serverName": "frr-k8s-metrics"},
		},
		{
			name: "TLS secret, explicit insecureSkipVerify true is honored",
			values: withSecret + `
prometheus:
  serviceMonitor:
    tlsConfig:
      insecureSkipVerify: true
`,
			want: map[string]any{"insecureSkipVerify": true},
		},
		{
			name: "self-signed certificate, explicit insecureSkipVerify false is honored",
			values: `
prometheus:
  serviceMonitor:
    tlsConfig:
      insecureSkipVerify: false
`,
			want: map[string]any{"insecureSkipVerify": false},
		},
		{
			name: "self-signed certificate, null insecureSkipVerify keeps verification on",
			values: `
prometheus:
  serviceMonitor:
    tlsConfig:
      insecureSkipVerify: null
      serverName: frr-k8s-metrics
`,
			want: map[string]any{"serverName": "frr-k8s-metrics"},
		},
		{
			name: "TLS secret, null insecureSkipVerify keeps verification on",
			values: withSecret + `
prometheus:
  serviceMonitor:
    tlsConfig:
      insecureSkipVerify: null
`,
		},
		{
			name: "self-signed certificate, tlsConfig set to false keeps verification on",
			values: `
prometheus:
  serviceMonitor:
    tlsConfig: false
`,
		},
		{
			name: "self-signed certificate, tlsConfig set to an empty string keeps verification on",
			values: `
prometheus:
  serviceMonitor:
    tlsConfig: ""
`,
		},
	}

	helm := helmBinary(t)
	trackChartFiles(t)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			endpoints := renderServiceMonitor(t, helm, tc.values)
			if len(endpoints) != 2 {
				t.Fatalf("expected 2 endpoints, got %d: %v", len(endpoints), endpoints)
			}
			for _, endpoint := range endpoints {
				got, found := endpoint["tlsConfig"]
				if tc.want == nil {
					if found {
						t.Errorf("endpoint %v: unexpected tlsConfig %v", endpoint["port"], got)
					}
					continue
				}
				if diff := cmp.Diff(tc.want, got); diff != "" {
					t.Errorf("endpoint %v: unexpected tlsConfig (-want +got):\n%s", endpoint["port"], diff)
				}
			}
		})
	}
}
