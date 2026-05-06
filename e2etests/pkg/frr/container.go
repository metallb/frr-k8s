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

// EVPNConfig holds the EVPN parameters needed for BGP peering configuration on
// the external FRR container.
type EVPNConfig struct {
	L2VNI int
	L3VNI int
	L3VRF string
}

// PairWithNodesForEVPN generates a BGP configuration with EVPN support for all
// cluster nodes and writes it to the container.
func PairWithNodesForEVPN(c *frrcontainer.FRR, cs clientset.Interface, cfg EVPNConfig, peerFamily ipfamily.Family) error {
	baseConfig, err := frrconfig.BGPPeersForAllNodes(cs, c.NeighborConfig, c.RouterConfig, peerFamily, frrconfig.MultiProtocolDisabled)
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

	data := evpnTemplateData{
		RouterASN: c.RouterConfig.ASN,
		Neighbors: neighbors,
	}

	if cfg.L2VNI > 0 {
		data.L2VNI = &evpnL2VNIData{
			VNI:          cfg.L2VNI,
			RD:           fmt.Sprintf("%d:%d", data.RouterASN, cfg.L2VNI),
			ImportRTSelf: fmt.Sprintf("%d:%d", data.RouterASN, cfg.L2VNI),
			ImportRTPeer: fmt.Sprintf("%d:%d", c.NeighborConfig.ASN, cfg.L2VNI),
			ExportRT:     fmt.Sprintf("%d:%d", data.RouterASN, cfg.L2VNI),
		}
	}

	if cfg.L3VNI > 0 {
		data.L3VNI = &evpnL3VNIData{
			VNI:          cfg.L3VNI,
			VRF:          cfg.L3VRF,
			RD:           fmt.Sprintf("%d:%d", data.RouterASN, cfg.L3VNI),
			ImportRTSelf: fmt.Sprintf("%d:%d", data.RouterASN, cfg.L3VNI),
			ImportRTPeer: fmt.Sprintf("%d:%d", c.NeighborConfig.ASN, cfg.L3VNI),
			ExportRT:     fmt.Sprintf("%d:%d", data.RouterASN, cfg.L3VNI),
		}
	}

	evpnConfig, err := renderEVPNConfig(data)
	if err != nil {
		return fmt.Errorf("rendering EVPN config: %w", err)
	}

	if err := c.UpdateBGPConfigFile(baseConfig + evpnConfig); err != nil {
		return fmt.Errorf("writing BGP config: %w", err)
	}

	// The VRF/VNI association is a zebra command that cannot go through
	// bgpd.conf (which is what UpdateBGPConfigFile writes to), so we apply
	// it via vtysh after writing the BGP config.
	if cfg.L3VNI > 0 {
		out, err := c.Exec("vtysh",
			"-c", "configure terminal",
			"-c", fmt.Sprintf("vrf %s", cfg.L3VRF),
			"-c", fmt.Sprintf("vni %d", cfg.L3VNI),
			"-c", "exit-vrf")
		if err != nil {
			return fmt.Errorf("vtysh vrf/vni config failed: %w\noutput: %s", err, out)
		}
	}

	return nil
}

type evpnTemplateData struct {
	RouterASN uint32
	Neighbors []string
	L2VNI     *evpnL2VNIData
	L3VNI     *evpnL3VNIData
}

type evpnL2VNIData struct {
	VNI          int
	RD           string
	ImportRTSelf string
	ImportRTPeer string
	ExportRT     string
}

type evpnL3VNIData struct {
	VNI          int
	VRF          string
	RD           string
	ImportRTSelf string
	ImportRTPeer string
	ExportRT     string
}

// evpnConfigTemplate contains only the EVPN-specific FRR stanzas that are
// appended to the base BGP config produced by BGPPeersForAllNodes. It reopens
// the router bgp block to add the l2vpn evpn address family.
const evpnConfigTemplate = `
router bgp {{ .RouterASN }}
  address-family l2vpn evpn
  {{- range .Neighbors }}
    neighbor {{ . }} activate
  {{- end }}
    advertise-all-vni
  {{- if .L2VNI }}
    vni {{ .L2VNI.VNI }}
      rd {{ .L2VNI.RD }}
      route-target import {{ .L2VNI.ImportRTSelf }}
      route-target import {{ .L2VNI.ImportRTPeer }}
      route-target export {{ .L2VNI.ExportRT }}
    exit-vni
  {{- end }}
  exit-address-family
{{- if .L3VNI }}

router bgp {{ .RouterASN }} vrf {{ .L3VNI.VRF }}
  address-family ipv4 unicast
    redistribute connected
  exit-address-family
  address-family l2vpn evpn
    rd {{ .L3VNI.RD }}
    route-target import {{ .L3VNI.ImportRTSelf }}
    route-target import {{ .L3VNI.ImportRTPeer }}
    route-target export {{ .L3VNI.ExportRT }}
    advertise ipv4 unicast
  exit-address-family
{{- end }}
`

var evpnTmpl = template.Must(template.New("evpnConfig").Parse(evpnConfigTemplate))

func renderEVPNConfig(data evpnTemplateData) (string, error) {
	var buf bytes.Buffer
	if err := evpnTmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
