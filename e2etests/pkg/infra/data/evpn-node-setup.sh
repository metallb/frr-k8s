#!/bin/bash
# =============================================================================
# EVPN Node Networking Setup
# =============================================================================
#
# Standalone script that configures Linux networking infrastructure for EVPN
# on a single node. Can be copied to any node and executed directly.
#
# What this script configures:
#   - Bridge with VLAN filtering + VXLAN device
#   - L2 VNI: VLAN-to-VNI mapping + interface (evpnl2-$EVPN_L2_VLAN_ID)
#   - L3 VNI: VLAN-to-VNI mapping + VRF + SVI + interface (evpnl3-$EVPN_L3_VLAN_ID)
#
# FRR running on this node is expected to be configured according to the parameters
# provided to this script. If frr-k8s is running, the expected FRRConfiguration would
# be similar to:
#
#   # L2 VNI (one FRRConfiguration per node, with nodeSelector):
#   spec:
#     bgp:
#       routers:
#       - asn: ...
#         neighbors:
#         - address: ...
#           asn: ...
#           addressFamilies: [unicast, evpn]
#         evpn:
#           advertiseVNIs: All
#           l2vnis:
#           - vni: $EVPN_L2_VNI
#             rd: ...
#             importRTs: ...  
#             exportRTs: ...
#
#   # L3 VNI (additional VRF router, one per node):
#   spec:
#     bgp:
#       routers:
#       - asn: ...
#         vrf: $EVPN_L3_VRF
#         prefixes:
#         - $EVPN_L3_PREFIX
#         evpn:
#           l3vni:
#             vni: $EVPN_L3_VNI
#             rd: ...
#             importRTs: ...
#             exportRTs: ...
#             advertisePrefixes: [unicast]
#
# If frr is running, the expected configuration would be similar to:
#
#   # L2 VNI (default VRF router):
#   router bgp ...
#     ...
#     address-family l2vpn evpn
#       neighbor ... activate
#       advertise-all-vni
#       vni $EVPN_L2_VNI
#         ...
#       exit-vni
#     exit-address-family
#
#   # L3 VNI (VRF router):
#   vrf $EVPN_L3_VRF
#     vni $EVPN_L3_VNI
#   exit-vrf
#
#   router bgp ... vrf $EVPN_L3_VRF
#     address-family ipv4 unicast
#       redistribute connected
#     exit-address-family
#     address-family l2vpn evpn
#       advertise ipv4 unicast
#       ...
#     exit-address-family
#
# Environment variables:
#   Required:
#     EVPN_VTEP_IP        - VTEP IP address for this node
#
#   L2 VNI (optional, provide all three or none):
#     EVPN_L2_VNI         - L2 VNI number (e.g., 1000)
#     EVPN_L2_VLAN_ID     - VLAN ID for L2 VNI (e.g., 100)
#     EVPN_L2_IP          - IP/mask for access port (e.g., 10.100.0.1/24)
#
#   L3 VNI (optional, provide all four or none):
#     EVPN_L3_VNI         - L3 VNI number (e.g., 3000)
#     EVPN_L3_VLAN_ID     - VLAN ID for L3 VNI SVI (e.g., 4000)
#     EVPN_L3_VRF         - VRF name (e.g., evpnred)
#     EVPN_L3_PREFIX      - IP/mask for dummy interface (e.g., 10.200.1.1/24)
#
#   Control:
#     EVPN_CLEANUP        - Set to "true" to tear down instead of set up
#
#   Advanced:
#     EVPN_BRIDGE         - Bridge device name (default: evpnbr)
#     EVPN_VXLAN          - VXLAN device name (default: evpnvx)
#     EVPN_L3_VRF_TABLE   - VRF routing table number (default: 10)
#
# Usage:
#   # L2 VNI setup
#   EVPN_VTEP_IP=172.18.0.3 \
#   EVPN_L2_VNI=1000 EVPN_L2_VLAN_ID=100 EVPN_L2_IP=10.100.0.1/24 \
#   ./evpn-node-setup.sh
#
#   # L3 VNI setup
#   EVPN_VTEP_IP=172.18.0.3 \
#   EVPN_L3_VNI=3000 EVPN_L3_VLAN_ID=4000 EVPN_L3_VRF=evpnred \
#   EVPN_L3_PREFIX=10.200.1.1/24 \
#   ./evpn-node-setup.sh
#
#   # Cleanup
#   EVPN_CLEANUP=true EVPN_VTEP_IP=172.18.0.3 \
#   EVPN_L3_VNI=3000 EVPN_L3_VLAN_ID=4000 EVPN_L3_VRF=evpnred \
#   EVPN_L3_PREFIX=10.200.1.1/24 \
#   ./evpn-node-setup.sh
#
# =============================================================================

set -euo pipefail

# Defaults
EVPN_BRIDGE="${EVPN_BRIDGE:-evpnbr}"
EVPN_VXLAN="${EVPN_VXLAN:-evpnvx}"
EVPN_L3_VRF_TABLE="${EVPN_L3_VRF_TABLE:-10}"

# =============================================================================
# Helpers
# =============================================================================

log() {
    echo "[$(date '+%H:%M:%S')] $1"
}

validate_env() {
    local ok=true

    # Cleanup only needs device names (EVPN_BRIDGE, EVPN_VXLAN, EVPN_L3_VLAN_ID, EVPN_L3_VRF)
    if [ "${EVPN_CLEANUP:-}" = "true" ]; then
        return
    fi

    if [ -z "${EVPN_VTEP_IP:-}" ]; then
        echo "ERROR: EVPN_VTEP_IP is required"
        ok=false
    fi

    if [ -z "${EVPN_L2_VNI:-}" ] && [ -z "${EVPN_L3_VNI:-}" ]; then
        echo "ERROR: At least one of EVPN_L2_VNI or EVPN_L3_VNI must be set"
        ok=false
    fi

    if [ -n "${EVPN_L2_VNI:-}" ]; then
        if [ -z "${EVPN_L2_VLAN_ID:-}" ]; then
            echo "ERROR: EVPN_L2_VLAN_ID is required when EVPN_L2_VNI is set"
            ok=false
        fi
        if [ -z "${EVPN_L2_IP:-}" ]; then
            echo "ERROR: EVPN_L2_IP is required when EVPN_L2_VNI is set"
            ok=false
        fi
    fi

    if [ -n "${EVPN_L3_VNI:-}" ]; then
        if [ -z "${EVPN_L3_VLAN_ID:-}" ] || [ -z "${EVPN_L3_VRF:-}" ] || [ -z "${EVPN_L3_PREFIX:-}" ]; then
            echo "ERROR: EVPN_L3_VLAN_ID, EVPN_L3_VRF and EVPN_L3_PREFIX are required when EVPN_L3_VNI is set"
            ok=false
        fi
    fi

    if [ "$ok" = false ]; then
        exit 1
    fi
}

# =============================================================================
# Network setup
# =============================================================================

# Creates a bridge with VLAN filtering and a VXLAN device in external/vnifilter
# mode. The VXLAN device supports multiple VNIs on a single device; FRR manages
# VNI-to-VTEP mappings via BGP EVPN signaling.
setup_bridge_vxlan() {
    log "Creating bridge and VXLAN (local VTEP: $EVPN_VTEP_IP)..."

    ip link del "$EVPN_BRIDGE" 2>/dev/null || true
    ip link del "$EVPN_VXLAN" 2>/dev/null || true

    ip link add "$EVPN_BRIDGE" type bridge vlan_filtering 1 vlan_default_pvid 0
    ip link set "$EVPN_BRIDGE" addrgenmode none
    ip link set "$EVPN_BRIDGE" up

    ip link add "$EVPN_VXLAN" type vxlan dstport 4789 local "$EVPN_VTEP_IP" nolearning external vnifilter
    ip link set "$EVPN_VXLAN" addrgenmode none
    ip link set "$EVPN_VXLAN" master "$EVPN_BRIDGE"
    ip link set "$EVPN_VXLAN" up

    bridge link set dev "$EVPN_VXLAN" vlan_tunnel on neigh_suppress on learning off
}

# Adds L2 VNI to the bridge/VXLAN and creates an access port (veth pair) for
# local L2 connectivity. The access port has an IP address that generates ARP
# entries, causing FRR to advertise type-2 (MAC/IP) routes via EVPN.
setup_l2_vni() {
    log "Setting up L2 VNI $EVPN_L2_VNI (VLAN $EVPN_L2_VLAN_ID)..."

    # Map VLAN to VNI on bridge and VXLAN
    bridge vlan add dev "$EVPN_BRIDGE" vid "$EVPN_L2_VLAN_ID" self
    bridge vlan add dev "$EVPN_VXLAN" vid "$EVPN_L2_VLAN_ID"
    bridge vni add dev "$EVPN_VXLAN" vni "$EVPN_L2_VNI"
    bridge vlan add dev "$EVPN_VXLAN" vid "$EVPN_L2_VLAN_ID" tunnel_info id "$EVPN_L2_VNI"

    # Access port: veth pair, bridge side gets PVID/untagged
    local l2_iface="evpnl2-${EVPN_L2_VLAN_ID}"
    local l2_iface_br="evpnl2br-${EVPN_L2_VLAN_ID}"
    ip link del "$l2_iface" 2>/dev/null || true
    ip link add "$l2_iface" type veth peer name "$l2_iface_br"
    ip link set "$l2_iface_br" master "$EVPN_BRIDGE"
    bridge vlan add dev "$l2_iface_br" vid "$EVPN_L2_VLAN_ID" pvid untagged
    ip link set "$l2_iface_br" up
    ip link set "$l2_iface" up
    ip addr add "$EVPN_L2_IP" dev "$l2_iface"
}

# Adds L3 VNI with a Linux VRF, SVI, and a test prefix. The VRF is created in
# the kernel so FRR's zebra can adopt it. The SVI bridges the VXLAN L3 VNI into
# the VRF for symmetric IRB. A dummy interface with a test prefix provides a
# connected route for FRR to advertise as a type-5 (IP prefix) route.
setup_l3_vni() {
    log "Setting up L3 VNI $EVPN_L3_VNI (VLAN $EVPN_L3_VLAN_ID, VRF $EVPN_L3_VRF)..."

    # Linux VRF -- FRR zebra will adopt this when it processes 'vrf <name>'
    ip link add "$EVPN_L3_VRF" type vrf table "$EVPN_L3_VRF_TABLE" 2>/dev/null || true
    ip link set "$EVPN_L3_VRF" up

    # Map VLAN to L3 VNI
    bridge vlan add dev "$EVPN_BRIDGE" vid "$EVPN_L3_VLAN_ID" self
    bridge vlan add dev "$EVPN_VXLAN" vid "$EVPN_L3_VLAN_ID"
    bridge vni add dev "$EVPN_VXLAN" vni "$EVPN_L3_VNI"
    bridge vlan add dev "$EVPN_VXLAN" vid "$EVPN_L3_VLAN_ID" tunnel_info id "$EVPN_L3_VNI"

    # SVI: VLAN sub-interface on the bridge, enslaved to VRF
    ip link del "${EVPN_BRIDGE}.${EVPN_L3_VLAN_ID}" 2>/dev/null || true
    ip link add "${EVPN_BRIDGE}.${EVPN_L3_VLAN_ID}" link "$EVPN_BRIDGE" type vlan id "$EVPN_L3_VLAN_ID"
    ip link set "${EVPN_BRIDGE}.${EVPN_L3_VLAN_ID}" addrgenmode none
    ip link set "${EVPN_BRIDGE}.${EVPN_L3_VLAN_ID}" master "$EVPN_L3_VRF"
    ip link set "${EVPN_BRIDGE}.${EVPN_L3_VLAN_ID}" up

    # Test prefix: dummy interface in VRF with a connected route
    local l3_iface="evpnl3-${EVPN_L3_VLAN_ID}"
    ip link del "$l3_iface" 2>/dev/null || true
    ip link add "$l3_iface" type dummy
    ip link set "$l3_iface" master "$EVPN_L3_VRF"
    ip link set "$l3_iface" up
    ip addr add "$EVPN_L3_PREFIX" dev "$l3_iface"
}

# =============================================================================
# Cleanup
# =============================================================================

cleanup_networking() {
    log "Cleaning up EVPN networking..."
    if [ -n "${EVPN_L2_VLAN_ID:-}" ]; then
        ip link del "evpnl2-${EVPN_L2_VLAN_ID}" 2>/dev/null || true
    fi
    if [ -n "${EVPN_L3_VLAN_ID:-}" ]; then
        ip link del "evpnl3-${EVPN_L3_VLAN_ID}" 2>/dev/null || true
        ip link del "${EVPN_BRIDGE}.${EVPN_L3_VLAN_ID}" 2>/dev/null || true
    fi
    ip link del "$EVPN_VXLAN" 2>/dev/null || true
    ip link del "$EVPN_BRIDGE" 2>/dev/null || true
    ip link del "${EVPN_L3_VRF:-__nonexistent__}" 2>/dev/null || true
}

# =============================================================================
# Main
# =============================================================================

validate_env

if [ "${EVPN_CLEANUP:-}" = "true" ]; then
    cleanup_networking
else
    setup_bridge_vxlan
    [ -n "${EVPN_L2_VNI:-}" ] && setup_l2_vni
    [ -n "${EVPN_L3_VNI:-}" ] && setup_l3_vni
    log "Node setup complete"
fi
