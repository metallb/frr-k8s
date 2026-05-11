// SPDX-License-Identifier:Apache-2.0

package infra

import (
	"context"
	_ "embed"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/metallb/frrk8stests/pkg/k8s"
	"go.universe.tf/e2etest/pkg/executor"
	frrcontainer "go.universe.tf/e2etest/pkg/frr/container"
	"go.universe.tf/e2etest/pkg/ipfamily"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

//go:embed data/evpn-node-setup.sh
var evpnNodeSetupScript string

// EVPNConfig holds the parameters for EVPN test networking setup. See
// data/evpn-node-setup.sh for more details on the networking infrastructure
// created on each target.
type EVPNConfig struct {
	// Bridge name of the VLAN-aware bridge where EVPNs are segmented
	// (e.g. "evpnbr").
	Bridge string
	// Vxlan name of the single VXLAN device carrying all VNIs in vnifilter mode
	// (e.g. "evpnvx").
	Vxlan string
	// L2VNI number for the L2 MAC-VRF (e.g. 1000). Zero disables it.
	L2VNI int
	// L2VLANID local VLAN ID segmenting the L2 MAC-VRF on the bridge (e.g.
	// 100).
	L2VLANID int
	// L2IPFmt pattern for L2 access port addresses, with a single %d for the
	// target index (e.g. "10.100.0.%d/24").
	L2IPFmt string
	// L2IPv6Fmt pattern for L2 access port IPv6 addresses, with a single %d
	// for the target index (e.g. "fc00:100::%d/64"). Use L2IPv6() to get the
	// host IP for a given index. Empty disables IPv6 on L2 access ports.
	L2IPv6Fmt string
	// L3VNI number for L3 IP-VRF (e.g. 3000). Zero disables it.
	L3VNI int
	// L3VLANID local VLAN ID segmenting the L3 IP-VRF on the bridge (e.g.
	// 4000).
	L3VLANID int
	// L3VRF name of the Linux VRF for L3 IP-VRF (e.g. "evpnred").
	L3VRF string
	// L3VRFTable number for the VRF.
	L3VRFTable int
	// L3PrefixFmt pattern for L3 VRF connected prefixes, with a single %d for
	// the target index (e.g. "10.200.%d.1/24").
	L3PrefixFmt string
	// L3IPv6PrefixFmt pattern for L3 VRF connected IPv6 prefixes, with a
	// single %d for the target index (e.g. "fc00:200:%d::1/64"). Use
	// L3IPv6Prefix() / L3IPv6PrefixIP(). Empty disables IPv6 on L3 VRF.
	L3IPv6PrefixFmt string
}

// EVPN is the result of SetupEVPN. It provides name-based lookups for the
// addresses assigned to each EVPN target (cluster nodes and external FRR).
type EVPN struct {
	cfg     EVPNConfig
	indices map[string]int // node/container name → target index
}

// L2IP returns the L2 access port IP (without mask) for the named target.
func (s *EVPN) L2IP(name string) string {
	ip, _, _ := net.ParseCIDR(fmt.Sprintf(s.cfg.L2IPFmt, s.indices[name]))
	return ip.String()
}

// L3Prefix returns the L3 VRF connected network prefix (CIDR) for the named
// target.
func (s *EVPN) L3Prefix(name string) string {
	_, network, _ := net.ParseCIDR(fmt.Sprintf(s.cfg.L3PrefixFmt, s.indices[name]))
	return network.String()
}

// L3PrefixIP returns the L3 host IP (without mask) for the named target.
func (s *EVPN) L3PrefixIP(name string) string {
	ip, _, _ := net.ParseCIDR(fmt.Sprintf(s.cfg.L3PrefixFmt, s.indices[name]))
	return ip.String()
}

// L2IPv6 returns the L2 access port IPv6 (without mask) for the named target.
func (s *EVPN) L2IPv6(name string) string {
	ip, _, _ := net.ParseCIDR(fmt.Sprintf(s.cfg.L2IPv6Fmt, s.indices[name]))
	return ip.String()
}

// L3IPv6Prefix returns the L3 VRF connected IPv6 network prefix (CIDR) for the
// named target.
func (s *EVPN) L3IPv6Prefix(name string) string {
	_, network, _ := net.ParseCIDR(fmt.Sprintf(s.cfg.L3IPv6PrefixFmt, s.indices[name]))
	return network.String()
}

// L3IPv6PrefixIP returns the L3 IPv6 host IP (without mask) for the named
// target.
func (s *EVPN) L3IPv6PrefixIP(name string) string {
	ip, _, _ := net.ParseCIDR(fmt.Sprintf(s.cfg.L3IPv6PrefixFmt, s.indices[name]))
	return ip.String()
}

// EVPNContainer is set during SetupEVPN to the ebgp-single-hop container used
// for EVPN peering.
var EVPNContainer *frrcontainer.FRR

// SetupEVPN configures EVPN Linux networking (bridge, VXLAN, VNI, VRF, access
// ports) on all cluster nodes and the ebgp-single-hop container. It returns an
// EVPN that provides name-based address lookups for each target.
func SetupEVPN(cs clientset.Interface, cfg EVPNConfig) (*EVPN, error) {
	ebgpSingleHop := findContainer("ebgp-single-hop")
	if ebgpSingleHop == nil {
		return nil, fmt.Errorf("ebgp-single-hop container not found in FRRContainers")
	}
	EVPNContainer = ebgpSingleHop
	nodes, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	if err := setupEVPNNetworking(cs, nodes.Items, ebgpSingleHop, cfg); err != nil {
		return nil, err
	}
	indices := make(map[string]int, len(nodes.Items)+1)
	for i, node := range nodes.Items {
		indices[node.Name] = i + 1
	}
	indices[ebgpSingleHop.Name] = len(nodes.Items) + 1
	return &EVPN{cfg: cfg, indices: indices}, nil
}

// CleanupEVPN tears down EVPN Linux networking on all cluster nodes and the
// ebgp-single-hop container.
func CleanupEVPN(cs clientset.Interface, cfg EVPNConfig) error {
	ebgpSingleHop := findContainer("ebgp-single-hop")
	if ebgpSingleHop == nil {
		return fmt.Errorf("ebgp-single-hop container not found in FRRContainers")
	}
	nodes, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return err
	}
	return cleanupEVPNNetworking(cs, nodes.Items, ebgpSingleHop, cfg)
}


func findContainer(name string) *frrcontainer.FRR {
	for _, c := range FRRContainers {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func setupEVPNNetworking(cs clientset.Interface, nodes []corev1.Node, externalFRR *frrcontainer.FRR, cfg EVPNConfig) error {
	pods, err := k8s.FRRK8sPods(cs)
	if err != nil {
		return fmt.Errorf("listing frr-k8s pods: %w", err)
	}

	for i, node := range nodes {
		pod, err := podForNode(pods, node.Name)
		if err != nil {
			return err
		}
		podExec := executor.ForPod(pod.Namespace, pod.Name, k8s.FRRContainerName)

		nodeIP, err := k8s.NodeIPForFamily(node, ipfamily.IPv4)
		if err != nil {
			return fmt.Errorf("getting IP for node %s: %w", node.Name, err)
		}
		env := buildTargetEnv(cfg, nodeIP, i+1)
		if err := runTargetSetup(podExec, env); err != nil {
			return fmt.Errorf("node setup on %s: %w", node.Name, err)
		}
	}

	extIndex := len(nodes) + 1
	extEnv := buildTargetEnv(cfg, externalFRR.Ipv4, extIndex)
	extExec := executor.ForContainer(externalFRR.Name)
	if err := runTargetSetup(extExec, extEnv); err != nil {
		return fmt.Errorf("node setup on external FRR %s: %w", externalFRR.Name, err)
	}

	time.Sleep(5 * time.Second)
	return nil
}

func cleanupEVPNNetworking(cs clientset.Interface, nodes []corev1.Node, externalFRR *frrcontainer.FRR, cfg EVPNConfig) error {
	extIndex := len(nodes) + 1
	extEnv := buildCleanupEnv(cfg, externalFRR.Ipv4, extIndex)
	extExec := executor.ForContainer(externalFRR.Name)
	if err := runTargetSetup(extExec, extEnv); err != nil {
		return fmt.Errorf("cleanup on external FRR %s: %w", externalFRR.Name, err)
	}

	pods, err := k8s.FRRK8sPods(cs)
	if err != nil {
		return fmt.Errorf("listing frr-k8s pods: %w", err)
	}

	for i, node := range nodes {
		pod, err := podForNode(pods, node.Name)
		if err != nil {
			return err
		}
		podExec := executor.ForPod(pod.Namespace, pod.Name, k8s.FRRContainerName)
		nodeIP, err := k8s.NodeIPForFamily(node, ipfamily.IPv4)
		if err != nil {
			return fmt.Errorf("getting IP for node %s: %w", node.Name, err)
		}
		env := buildCleanupEnv(cfg, nodeIP, i+1)
		if err := runTargetSetup(podExec, env); err != nil {
			return fmt.Errorf("cleanup on %s: %w", node.Name, err)
		}
	}

	return nil
}

func podForNode(pods []*corev1.Pod, nodeName string) (*corev1.Pod, error) {
	for _, pod := range pods {
		if pod.Spec.NodeName == nodeName {
			return pod, nil
		}
	}
	return nil, fmt.Errorf("no frr-k8s pod found on node %s", nodeName)
}

func buildTargetEnv(cfg EVPNConfig, vtepIP string, targetIndex int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "export EVPN_VTEP_IP=%s\n", vtepIP)
	fmt.Fprintf(&b, "export EVPN_BRIDGE=%s\n", cfg.Bridge)
	fmt.Fprintf(&b, "export EVPN_VXLAN=%s\n", cfg.Vxlan)

	if cfg.L2VNI > 0 {
		fmt.Fprintf(&b, "export EVPN_L2_VNI=%d\n", cfg.L2VNI)
		fmt.Fprintf(&b, "export EVPN_L2_VLAN_ID=%d\n", cfg.L2VLANID)
		if cfg.L2IPFmt != "" {
			fmt.Fprintf(&b, "export EVPN_L2_IP=%s\n", fmt.Sprintf(cfg.L2IPFmt, targetIndex))
		}
		if cfg.L2IPv6Fmt != "" {
			fmt.Fprintf(&b, "export EVPN_L2_IPV6=%s\n", fmt.Sprintf(cfg.L2IPv6Fmt, targetIndex))
		}
	}

	if cfg.L3VNI > 0 {
		fmt.Fprintf(&b, "export EVPN_L3_VNI=%d\n", cfg.L3VNI)
		fmt.Fprintf(&b, "export EVPN_L3_VLAN_ID=%d\n", cfg.L3VLANID)
		fmt.Fprintf(&b, "export EVPN_L3_VRF=%s\n", cfg.L3VRF)
		fmt.Fprintf(&b, "export EVPN_L3_VRF_TABLE=%d\n", cfg.L3VRFTable)
		if cfg.L3PrefixFmt != "" {
			fmt.Fprintf(&b, "export EVPN_L3_PREFIX=%s\n", fmt.Sprintf(cfg.L3PrefixFmt, targetIndex))
		}
		if cfg.L3IPv6PrefixFmt != "" {
			fmt.Fprintf(&b, "export EVPN_L3_IPV6_PREFIX=%s\n", fmt.Sprintf(cfg.L3IPv6PrefixFmt, targetIndex))
		}
	}

	return b.String()
}

func buildCleanupEnv(cfg EVPNConfig, vtepIP string, targetIndex int) string {
	env := buildTargetEnv(cfg, vtepIP, targetIndex)
	return env + "export EVPN_CLEANUP=true\n"
}

func runTargetSetup(exec executor.Executor, env string) error {
	script := env + "\n" + evpnNodeSetupScript
	out, err := exec.Exec("bash", "-c", script)
	if err != nil {
		return fmt.Errorf("running script: %w\noutput: %s", err, out)
	}
	return nil
}
