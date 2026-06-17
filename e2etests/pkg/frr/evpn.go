// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"bytes"
	"context"
	"fmt"
	"strings"
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
	L2VNI           uint32
	L3VNI           uint32
	L3VRF           string
	AdvertiseFamily ipfamily.Family
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
			IPv4:         cfg.AdvertiseFamily == ipfamily.IPv4 || cfg.AdvertiseFamily == ipfamily.DualStack,
			IPv6:         cfg.AdvertiseFamily == ipfamily.IPv6 || cfg.AdvertiseFamily == ipfamily.DualStack,
		}
	}

	evpnConfig, err := renderEVPNConfig(data)
	if err != nil {
		return fmt.Errorf("rendering EVPN config: %w", err)
	}

	vrfPreamble, err := renderVRFPreamble(data.L3VNI)
	if err != nil {
		return fmt.Errorf("rendering VRF preamble: %w", err)
	}

	fullConfig := insertBeforeRouterBGP(baseConfig, vrfPreamble) + evpnConfig
	if err := c.UpdateConfigFile(fullConfig); err != nil {
		return fmt.Errorf("writing BGP config: %w", err)
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
	VNI          uint32
	RD           string
	ImportRTSelf string
	ImportRTPeer string
	ExportRT     string
}

type evpnL3VNIData struct {
	VNI          uint32
	VRF          string
	RD           string
	ImportRTSelf string
	ImportRTPeer string
	ExportRT     string
	IPv4         bool
	IPv6         bool
}

// evpnConfigTemplate contains the EVPN-specific router bgp stanzas appended
// to the base BGP config. The VRF definition is handled separately by
// evpnVRFTemplate because vtysh --mark requires it before any router bgp block.
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
  {{- if .L3VNI.IPv4 }}
  address-family ipv4 unicast
    redistribute connected
  exit-address-family
  {{- end }}
  {{- if .L3VNI.IPv6 }}
  address-family ipv6 unicast
    redistribute connected
  exit-address-family
  {{- end }}
  address-family l2vpn evpn
    rd {{ .L3VNI.RD }}
    route-target import {{ .L3VNI.ImportRTSelf }}
    route-target import {{ .L3VNI.ImportRTPeer }}
    route-target export {{ .L3VNI.ExportRT }}
    {{- if .L3VNI.IPv4 }}
    advertise ipv4 unicast
    {{- end }}
    {{- if .L3VNI.IPv6 }}
    advertise ipv6 unicast
    {{- end }}
  exit-address-family
{{- end }}
`

const evpnVRFTemplate = `
vrf {{ .VRF }}
  vni {{ .VNI }}
exit-vrf
`

var (
	evpnTmpl    = template.Must(template.New("evpnConfig").Parse(evpnConfigTemplate))
	evpnVRFTmpl = template.Must(template.New("evpnVRF").Parse(evpnVRFTemplate))
)

func renderEVPNConfig(data evpnTemplateData) (string, error) {
	var buf bytes.Buffer
	if err := evpnTmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func renderVRFPreamble(l3vni *evpnL3VNIData) (string, error) {
	if l3vni == nil {
		return "", nil
	}
	var buf bytes.Buffer
	if err := evpnVRFTmpl.Execute(&buf, l3vni); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// insertBeforeRouterBGP inserts preamble text before the first "router bgp"
// line in config. This is needed because vtysh --mark (used by frr-reload.py)
// cannot parse VRF definitions that appear after router bgp blocks.
func insertBeforeRouterBGP(config, preamble string) string {
	if preamble == "" {
		return config
	}
	idx := strings.Index(config, "\nrouter bgp ")
	if idx == -1 {
		return config + preamble
	}
	return config[:idx] + "\n" + strings.TrimRight(preamble, "\n") + config[idx:]
}
