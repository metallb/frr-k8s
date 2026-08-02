// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"bytes"
	"context"
	"fmt"
	"text/template"

	"github.com/metallb/frrk8stests/pkg/k8s"
	frrconfig "go.universe.tf/e2etest/pkg/frr/config"
	frrcontainer "go.universe.tf/e2etest/pkg/frr/container"
	"go.universe.tf/e2etest/pkg/ipfamily"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

type prependASNConfig struct {
	RouterASN  uint32
	PrependASN uint32
	Neighbors  []string
	HasV4      bool
	HasV6      bool
}

const prependASNConfigTemplate = `
route-map PREPEND-ASN permit 10
  set as-path prepend {{ .PrependASN }}
!
router bgp {{ .RouterASN }}
{{- if .HasV4 }}
  address-family ipv4 unicast
{{- range .Neighbors }}
    neighbor {{ . }} route-map PREPEND-ASN out
{{- end }}
  exit-address-family
{{- end }}
{{- if .HasV6 }}
  address-family ipv6 unicast
{{- range .Neighbors }}
    neighbor {{ . }} route-map PREPEND-ASN out
{{- end }}
  exit-address-family
{{- end }}
`

var prependASNTmpl = template.Must(template.New("prependASN").Parse(prependASNConfigTemplate))

func appendPrependASNConfig(baseConfig string, cfg prependASNConfig) (string, error) {
	if cfg.PrependASN == 0 {
		return baseConfig, nil
	}
	var buf bytes.Buffer
	if err := prependASNTmpl.Execute(&buf, cfg); err != nil {
		return "", fmt.Errorf("rendering prepend ASN config: %w", err)
	}
	return baseConfig + buf.String(), nil
}

// PairWithNodesWithPrependASN generates a BGP configuration that advertises
// prefixes with a prepended ASN in the AS path, and writes it to the container.
// Modifiers are applied to a copy of the container to set NeighborConfig fields
// (e.g. ToAdvertiseV4/V6) before generating the config, following the same
// pattern as PairWithNodes.
func PairWithNodesWithPrependASN(c *frrcontainer.FRR, cs clientset.Interface, prependASN uint32, peerFamily ipfamily.Family, modifiers ...func(c *frrcontainer.FRR)) error {
	config := *c
	for _, m := range modifiers {
		m(&config)
	}

	baseConfig, err := frrconfig.BGPPeersForAllNodes(cs, config.NeighborConfig, config.RouterConfig, peerFamily, frrconfig.MultiProtocolEnabled)
	if err != nil {
		return fmt.Errorf("generating base BGP config: %w", err)
	}

	nodes, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing nodes: %w", err)
	}

	neighbors, err := k8s.NodeIPsForFamily(nodes.Items, peerFamily)
	if err != nil {
		return fmt.Errorf("getting node IPs: %w", err)
	}

	cfg := prependASNConfig{
		RouterASN:  config.RouterConfig.ASN,
		PrependASN: prependASN,
		Neighbors:  neighbors,
		HasV4:      len(config.NeighborConfig.ToAdvertiseV4) > 0,
		HasV6:      len(config.NeighborConfig.ToAdvertiseV6) > 0,
	}

	fullConfig, err := appendPrependASNConfig(baseConfig, cfg)
	if err != nil {
		return fmt.Errorf("appending prepend ASN config: %w", err)
	}

	if err := c.UpdateConfigFile(fullConfig); err != nil {
		return fmt.Errorf("writing BGP config: %w", err)
	}

	return nil
}
