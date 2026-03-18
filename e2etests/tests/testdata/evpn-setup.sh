#!/bin/bash
# =============================================================================
# EVPN Host Networking Setup for frr-k8s Integration Tests
# =============================================================================
#
# Prepares the Linux networking infrastructure (bridges, VXLAN, VLANs, SVIs,
# VRFs, access ports) needed for EVPN testing on a kind cluster. Configures
# both kind cluster nodes (frr-k8s side) and the external FRR container (peer).
#
# What this script does:
#   ON ALL CONTAINERS (kind nodes + external FRR):
#   - Bridge with VLAN filtering + VXLAN device (external vnifilter mode)
#   - L2 VNI: VLAN-to-VNI mapping + access port (veth pair for MAC learning)
#   - L3 VNI: Linux VRF + SVI (VLAN sub-interface) + test prefix
#
#   ON EXTERNAL FRR CONTAINER ONLY:
#   - FRR EVPN BGP config via vtysh (neighbor activation, advertise-all-vni,
#     VNI RD/RTs, VRF-VNI binding, prefix advertisement)
#
# What this script does NOT do:
#   - Configure FRR/BGP on the frr-k8s side. That's the FRRConfiguration CRD's
#     job -- testing that is the whole point of these integration tests.
#   - Create or manage containers (handled by the e2e test framework or manually)
#
# Topology:
#
#   kind network (docker bridge)
#   ┌─────────────────────────────────────────────────────────┐
#   │                                                         │
#   │  ┌──────────────────────┐   ┌────────────────────────┐  │
#   │  │ kind-worker (node)   │   │ external FRR container │  │
#   │  │                      │   │                        │  │
#   │  │  ┌───────────────┐   │   │  ┌────────────────┐   │  │
#   │  │  │ evpnbr (bridge│   │   │  │ evpnbr (bridge)│   │  │
#   │  │  │  VLAN filter) │   │   │  │                │   │  │
#   │  │  │  ├─ evpnvx    │   │   │  │  ├─ evpnvx     │   │  │
#   │  │  │  │  (VXLAN)   │◄──────────│  (VXLAN)      │   │  │
#   │  │  │  ├─ evpnaccbr │   │   │  │  ├─ evpnaccbr  │   │  │
#   │  │  │  │  (access)  │   │   │  │  │  (access)   │   │  │
#   │  │  │  └─ SVI (.vid)│   │   │  │  └─ SVI (.vid) │   │  │
#   │  │  └───────┬───────┘   │   │  └────────┬───────┘   │  │
#   │  │          │           │   │           │           │  │
#   │  │  FRR (frr-k8s)       │   │  FRR (standalone)     │  │
#   │  │  config from CRD     │   │  config from script   │  │
#   │  └──────────────────────┘   └────────────────────────┘  │
#   └─────────────────────────────────────────────────────────┘
#
# Expected FRRConfiguration CRDs (applied by test, NOT by this script).
# Values must match the environment variables passed to this script.
# One FRRConfiguration per node with a nodeSelector, since each node has
# a distinct prefix (10.200.<node-index>.0/24) in the VRF:
#
#   # Per-node FRRConfiguration (one per kind worker node):
#   spec:
#     nodeSelector:
#       kubernetes.io/hostname: <node-name>
#     bgp:
#       routers:
#       - asn: $EVPN_FRR_K8S_ASN
#         neighbors:
#         - address: <external-frr-ip>
#           asn: $EVPN_EXTERNAL_ASN
#           addressFamilies: [unicast, evpn]
#         evpn:
#           advertiseVNIs: All
#           l2VNIs:                              # only if EVPN_L2_VNI is set
#           - vni: $EVPN_L2_VNI
#             rd: "$EVPN_FRR_K8S_ASN:$EVPN_L2_VNI"
#             importRTs: ["$EVPN_FRR_K8S_ASN:$EVPN_L2_VNI", "$EVPN_EXTERNAL_ASN:$EVPN_L2_VNI"]
#             exportRTs: ["$EVPN_FRR_K8S_ASN:$EVPN_L2_VNI"]
#       - asn: $EVPN_FRR_K8S_ASN               # only if EVPN_L3_VNI is set
#         vrf: $EVPN_L3_VRF
#         prefixes:
#         - 10.200.<node-index>.0/24  # must match route in VRF on this node
#         evpn:
#           l3VNI:
#             vni: $EVPN_L3_VNI
#             rd: "$EVPN_FRR_K8S_ASN:$EVPN_L3_VNI"
#             importRTs: ["$EVPN_FRR_K8S_ASN:$EVPN_L3_VNI", "$EVPN_EXTERNAL_ASN:$EVPN_L3_VNI"]
#             exportRTs: ["$EVPN_FRR_K8S_ASN:$EVPN_L3_VNI"]
#             advertisePrefixes: [unicast]
#
# Environment variables:
#   Required:
#     EVPN_NODES          - Space-separated kind node container names
#     EVPN_EXTERNAL       - External FRR container name
#     EVPN_EXTERNAL_ASN   - External FRR's BGP ASN
#     EVPN_FRR_K8S_ASN    - frr-k8s BGP ASN (for neighbor config + shared RTs)
#
#   L2 VNI (optional, provide both or neither):
#     EVPN_L2_VNI         - L2 VNI number (e.g., 1000)
#     EVPN_L2_VLAN_ID     - VLAN ID for L2 VNI (e.g., 100)
#
#   L3 VNI (optional, provide all three or none):
#     EVPN_L3_VNI         - L3 VNI number (e.g., 3000)
#     EVPN_L3_VLAN_ID     - VLAN ID for L3 VNI SVI (e.g., 4000)
#     EVPN_L3_VRF         - VRF name (e.g., red)
#
#   Control:
#     EVPN_CLEANUP        - Set to "true" to tear down instead of set up
#
#   Advanced:
#     CONTAINER_RUNTIME   - Container runtime command (default: docker)
#     EVPN_NETWORK        - Docker network for IP discovery (default: kind)
#     EVPN_BRIDGE         - Bridge device name (default: evpnbr)
#     EVPN_VXLAN          - VXLAN device name (default: evpnvx)
#     EVPN_L3_VRF_TABLE   - VRF routing table number (default: 10)
#
# Usage:
#   # L2 VNI only
#   EVPN_NODES="frr-k8s-worker frr-k8s-worker2" \
#   EVPN_EXTERNAL="ebgp-single-hop" \
#   EVPN_EXTERNAL_ASN=4200000000 \
#   EVPN_FRR_K8S_ASN=64512 \
#   EVPN_L2_VNI=1000 EVPN_L2_VLAN_ID=100 \
#   ./hack/evpn-setup.sh
#
#   # L3 VNI only
#   EVPN_NODES="frr-k8s-worker frr-k8s-worker2" \
#   EVPN_EXTERNAL="ebgp-single-hop" \
#   EVPN_EXTERNAL_ASN=4200000000 \
#   EVPN_FRR_K8S_ASN=64512 \
#   EVPN_L3_VNI=3000 EVPN_L3_VLAN_ID=4000 EVPN_L3_VRF=red \
#   ./hack/evpn-setup.sh
#
#   # Cleanup
#   EVPN_CLEANUP=true ... ./hack/evpn-setup.sh
#
# =============================================================================

set -euo pipefail

# Defaults
CONTAINER_RUNTIME="${CONTAINER_RUNTIME:-docker}"
EVPN_NETWORK="${EVPN_NETWORK:-kind}"
EVPN_BRIDGE="${EVPN_BRIDGE:-evpnbr}"
EVPN_VXLAN="${EVPN_VXLAN:-evpnvx}"
EVPN_L3_VRF_TABLE="${EVPN_L3_VRF_TABLE:-10}"

# =============================================================================
# Helpers
# =============================================================================

log() {
    echo "[$(date '+%H:%M:%S')] $1"
}

exec_in() {
    local container=$1; shift
    $CONTAINER_RUNTIME exec "$container" /bin/sh -c "$*"
}

vtysh_in() {
    local container=$1; shift
    $CONTAINER_RUNTIME exec "$container" vtysh "$@"
}

get_container_ip() {
    local container=$1
    $CONTAINER_RUNTIME inspect \
        -f "{{(index .NetworkSettings.Networks \"${EVPN_NETWORK}\").IPAddress}}" \
        "$container"
}

validate_env() {
    local ok=true
    for var in EVPN_NODES EVPN_EXTERNAL EVPN_EXTERNAL_ASN EVPN_FRR_K8S_ASN; do
        if [ -z "${!var:-}" ]; then
            echo "ERROR: $var is required"
            ok=false
        fi
    done
    if [ -z "${EVPN_L2_VNI:-}" ] && [ -z "${EVPN_L3_VNI:-}" ]; then
        echo "ERROR: At least one of EVPN_L2_VNI or EVPN_L3_VNI must be set"
        ok=false
    fi
    if [ -n "${EVPN_L2_VNI:-}" ] && [ -z "${EVPN_L2_VLAN_ID:-}" ]; then
        echo "ERROR: EVPN_L2_VLAN_ID is required when EVPN_L2_VNI is set"
        ok=false
    fi
    if [ -n "${EVPN_L3_VNI:-}" ]; then
        if [ -z "${EVPN_L3_VLAN_ID:-}" ] || [ -z "${EVPN_L3_VRF:-}" ]; then
            echo "ERROR: EVPN_L3_VLAN_ID and EVPN_L3_VRF are required when EVPN_L3_VNI is set"
            ok=false
        fi
    fi
    if [ "$ok" = false ]; then
        exit 1
    fi
}

# =============================================================================
# Network setup (shared between kind nodes and external FRR container)
# =============================================================================

# Creates a bridge with VLAN filtering and a VXLAN device in external/vnifilter
# mode. The VXLAN device supports multiple VNIs on a single device; FRR manages
# VNI-to-VTEP mappings via BGP EVPN signaling.
#
# Args: $1=container, $2=local VTEP IP
setup_bridge_vxlan() {
    local container=$1 local_ip=$2

    log "[$container] Creating bridge and VXLAN (local VTEP: $local_ip)..."
    exec_in "$container" "
        ip link del $EVPN_BRIDGE 2>/dev/null || true
        ip link del $EVPN_VXLAN 2>/dev/null || true

        ip link add $EVPN_BRIDGE type bridge vlan_filtering 1 vlan_default_pvid 0
        ip link set $EVPN_BRIDGE addrgenmode none
        ip link set $EVPN_BRIDGE up

        ip link add $EVPN_VXLAN type vxlan dstport 4789 local $local_ip nolearning external vnifilter
        ip link set $EVPN_VXLAN addrgenmode none
        ip link set $EVPN_VXLAN master $EVPN_BRIDGE
        ip link set $EVPN_VXLAN up

        bridge link set dev $EVPN_VXLAN vlan_tunnel on neigh_suppress on learning off
    "
}

# Adds L2 VNI to the bridge/VXLAN and creates an access port (veth pair) for
# local L2 connectivity. The access port has an IP address that generates ARP
# entries, causing FRR to advertise type-2 (MAC/IP) routes via EVPN.
#
# Args: $1=container, $2=node index (for unique IP: 10.100.0.<index>)
setup_l2_vni() {
    local container=$1 node_index=$2

    log "[$container] Setting up L2 VNI $EVPN_L2_VNI (VLAN $EVPN_L2_VLAN_ID)..."
    exec_in "$container" "
        # Map VLAN to VNI on bridge and VXLAN
        bridge vlan add dev $EVPN_BRIDGE vid $EVPN_L2_VLAN_ID self
        bridge vlan add dev $EVPN_VXLAN vid $EVPN_L2_VLAN_ID
        bridge vni add dev $EVPN_VXLAN vni $EVPN_L2_VNI
        bridge vlan add dev $EVPN_VXLAN vid $EVPN_L2_VLAN_ID tunnel_info id $EVPN_L2_VNI

        # Access port: veth pair, bridge side gets PVID/untagged
        ip link del evpnacc 2>/dev/null || true
        ip link add evpnacc type veth peer name evpnaccbr
        ip link set evpnaccbr master $EVPN_BRIDGE
        bridge vlan add dev evpnaccbr vid $EVPN_L2_VLAN_ID pvid untagged
        ip link set evpnaccbr up
        ip link set evpnacc up
        ip addr add 10.100.0.${node_index}/24 dev evpnacc
    "
}

# Adds L3 VNI with a Linux VRF, SVI, and a test prefix. The VRF is created in
# the kernel so FRR's zebra can adopt it. The SVI bridges the VXLAN L3 VNI into
# the VRF for symmetric IRB. A dummy interface with a test prefix provides a
# connected route for FRR to advertise as a type-5 (IP prefix) route.
#
# Args: $1=container, $2=node index (for unique prefix: 10.200.<index>.0/24)
setup_l3_vni() {
    local container=$1 node_index=$2

    log "[$container] Setting up L3 VNI $EVPN_L3_VNI (VLAN $EVPN_L3_VLAN_ID, VRF $EVPN_L3_VRF)..."
    exec_in "$container" "
        # Linux VRF -- FRR zebra will adopt this when it processes 'vrf <name>'
        ip link add $EVPN_L3_VRF type vrf table $EVPN_L3_VRF_TABLE 2>/dev/null || true
        ip link set $EVPN_L3_VRF up

        # Map VLAN to L3 VNI
        bridge vlan add dev $EVPN_BRIDGE vid $EVPN_L3_VLAN_ID self
        bridge vlan add dev $EVPN_VXLAN vid $EVPN_L3_VLAN_ID
        bridge vni add dev $EVPN_VXLAN vni $EVPN_L3_VNI
        bridge vlan add dev $EVPN_VXLAN vid $EVPN_L3_VLAN_ID tunnel_info id $EVPN_L3_VNI

        # SVI: VLAN sub-interface on the bridge, enslaved to VRF
        ip link del ${EVPN_BRIDGE}.${EVPN_L3_VLAN_ID} 2>/dev/null || true
        ip link add ${EVPN_BRIDGE}.${EVPN_L3_VLAN_ID} link $EVPN_BRIDGE type vlan id $EVPN_L3_VLAN_ID
        ip link set ${EVPN_BRIDGE}.${EVPN_L3_VLAN_ID} addrgenmode none
        ip link set ${EVPN_BRIDGE}.${EVPN_L3_VLAN_ID} master $EVPN_L3_VRF
        ip link set ${EVPN_BRIDGE}.${EVPN_L3_VLAN_ID} up

        # Test prefix: dummy interface in VRF with a connected route
        ip link del evpndummy 2>/dev/null || true
        ip link add evpndummy type dummy
        ip link set evpndummy master $EVPN_L3_VRF
        ip link set evpndummy up
        ip addr add 10.200.${node_index}.1/24 dev evpndummy
    "
}

# =============================================================================
# External FRR EVPN BGP configuration
# =============================================================================

# Configures EVPN on the external FRR container. This uses a three-phase
# approach to handle the FRR timing issue where bgpd needs time to learn about
# VRF-VNI associations from zebra before route-targets can be configured.
#
# Both sides use the same route-target values (EVPN_FRR_K8S_ASN:VNI) so that
# routes are imported/exported correctly regardless of eBGP/iBGP.
#
# NOTE: This configures FRR via vtysh (runtime config). If the test framework
# later calls UpdateBGPConfigFile, these changes will be overwritten. For
# integration tests (Phase 7), EVPN config should be included in the BGP
# config template instead.
configure_external_frr() {
    local container=$1

    log "[$container] Configuring FRR for EVPN..."

    # Discover node IPs for neighbor activation
    local neighbor_ips=""
    for node in $EVPN_NODES; do
        neighbor_ips="$neighbor_ips $(get_container_ip "$node")"
    done

    # =========================================================================
    # Phase 1: VRF-VNI binding (zebra config)
    # Must happen before BGP VRF config so bgpd learns the association.
    # =========================================================================
    if [ -n "${EVPN_L3_VNI:-}" ]; then
        log "[$container] Phase 1: VRF-VNI binding..."
        vtysh_in "$container" \
            -c "configure terminal" \
            -c "vrf ${EVPN_L3_VRF}" \
            -c "vni ${EVPN_L3_VNI}" \
            -c "exit-vrf" \
            -c "end"
        sleep 2
    fi

    # =========================================================================
    # Phase 2: Global EVPN BGP config
    # Define neighbors, activate in l2vpn evpn, enable VNI advertisement.
    # =========================================================================
    log "[$container] Phase 2: Global EVPN BGP config..."
    local VTYSH_CMDS="-c 'configure terminal'"
    VTYSH_CMDS="$VTYSH_CMDS -c 'router bgp ${EVPN_EXTERNAL_ASN}'"

    # Ensure neighbors exist (idempotent if already configured by test framework)
    for ip in $neighbor_ips; do
        VTYSH_CMDS="$VTYSH_CMDS -c 'neighbor ${ip} remote-as ${EVPN_FRR_K8S_ASN}'"
    done

    VTYSH_CMDS="$VTYSH_CMDS -c 'address-family l2vpn evpn'"
    for ip in $neighbor_ips; do
        VTYSH_CMDS="$VTYSH_CMDS -c 'neighbor ${ip} activate'"
    done
    VTYSH_CMDS="$VTYSH_CMDS -c 'advertise-all-vni'"

    # L2 VNI: RD and route-targets (inside vni/exit-vni block)
    if [ -n "${EVPN_L2_VNI:-}" ]; then
        VTYSH_CMDS="$VTYSH_CMDS -c 'vni ${EVPN_L2_VNI}'"
        VTYSH_CMDS="$VTYSH_CMDS -c 'rd ${EVPN_EXTERNAL_ASN}:${EVPN_L2_VNI}'"
        VTYSH_CMDS="$VTYSH_CMDS -c 'route-target import ${EVPN_FRR_K8S_ASN}:${EVPN_L2_VNI}'"
        VTYSH_CMDS="$VTYSH_CMDS -c 'route-target import ${EVPN_EXTERNAL_ASN}:${EVPN_L2_VNI}'"
        VTYSH_CMDS="$VTYSH_CMDS -c 'route-target export ${EVPN_EXTERNAL_ASN}:${EVPN_L2_VNI}'"
        VTYSH_CMDS="$VTYSH_CMDS -c 'exit-vni'"
    fi

    VTYSH_CMDS="$VTYSH_CMDS -c 'exit-address-family'"
    VTYSH_CMDS="$VTYSH_CMDS -c 'end'"

    local output
    if ! output=$(eval "vtysh_in $container $VTYSH_CMDS" 2>&1); then
        log "[$container] ERROR: Failed to configure global EVPN BGP"
        log "[$container] vtysh output: $output"
        return 1
    fi

    sleep 2

    # =========================================================================
    # Phase 3: IP-VRF BGP config with route-targets (L3 VNI)
    # Must happen after bgpd has learned the VRF-VNI association from phase 1.
    # =========================================================================
    if [ -n "${EVPN_L3_VNI:-}" ]; then
        log "[$container] Phase 3: IP-VRF BGP config..."
        VTYSH_CMDS="-c 'configure terminal'"
        VTYSH_CMDS="$VTYSH_CMDS -c 'router bgp ${EVPN_EXTERNAL_ASN} vrf ${EVPN_L3_VRF}'"
        VTYSH_CMDS="$VTYSH_CMDS -c 'address-family ipv4 unicast'"
        VTYSH_CMDS="$VTYSH_CMDS -c 'redistribute connected'"
        VTYSH_CMDS="$VTYSH_CMDS -c 'exit-address-family'"
        VTYSH_CMDS="$VTYSH_CMDS -c 'address-family l2vpn evpn'"
        VTYSH_CMDS="$VTYSH_CMDS -c 'rd ${EVPN_EXTERNAL_ASN}:${EVPN_L3_VNI}'"
        VTYSH_CMDS="$VTYSH_CMDS -c 'route-target import ${EVPN_FRR_K8S_ASN}:${EVPN_L3_VNI}'"
        VTYSH_CMDS="$VTYSH_CMDS -c 'route-target import ${EVPN_EXTERNAL_ASN}:${EVPN_L3_VNI}'"
        VTYSH_CMDS="$VTYSH_CMDS -c 'route-target export ${EVPN_EXTERNAL_ASN}:${EVPN_L3_VNI}'"
        VTYSH_CMDS="$VTYSH_CMDS -c 'advertise ipv4 unicast'"
        VTYSH_CMDS="$VTYSH_CMDS -c 'advertise ipv6 unicast'"
        VTYSH_CMDS="$VTYSH_CMDS -c 'exit-address-family'"
        VTYSH_CMDS="$VTYSH_CMDS -c 'end'"

        if ! output=$(eval "vtysh_in $container $VTYSH_CMDS" 2>&1); then
            log "[$container] ERROR: Failed to configure IP-VRF BGP"
            log "[$container] vtysh output: $output"
            return 1
        fi
    fi

    log "[$container] FRR EVPN configuration complete"
}

# =============================================================================
# Cleanup
# =============================================================================

cleanup_networking() {
    local container=$1
    log "[$container] Cleaning up EVPN networking..."
    exec_in "$container" "
        ip link del evpnacc 2>/dev/null || true
        ip link del evpndummy 2>/dev/null || true
        ip link del ${EVPN_BRIDGE}.${EVPN_L3_VLAN_ID:-0} 2>/dev/null || true
        ip link del $EVPN_VXLAN 2>/dev/null || true
        ip link del $EVPN_BRIDGE 2>/dev/null || true
        ip link del ${EVPN_L3_VRF:-__nonexistent__} 2>/dev/null || true
    " || true
}

cleanup_external_frr() {
    local container=$1
    log "[$container] Cleaning up FRR EVPN config..."

    # Step 1: Remove VNI binding from VRF
    if [ -n "${EVPN_L3_VNI:-}" ]; then
        vtysh_in "$container" \
            -c "configure terminal" \
            -c "vrf ${EVPN_L3_VRF}" \
            -c "no vni ${EVPN_L3_VNI}" \
            -c "exit-vrf" \
            -c "end" 2>/dev/null || true
    fi

    # Step 2: Remove L2 VNI config from global BGP l2vpn evpn
    if [ -n "${EVPN_L2_VNI:-}" ]; then
        vtysh_in "$container" \
            -c "configure terminal" \
            -c "router bgp ${EVPN_EXTERNAL_ASN}" \
            -c "address-family l2vpn evpn" \
            -c "no vni ${EVPN_L2_VNI}" \
            -c "exit-address-family" \
            -c "end" 2>/dev/null || true
    fi

    # Step 3: Remove VRF BGP instance
    if [ -n "${EVPN_L3_VNI:-}" ]; then
        vtysh_in "$container" \
            -c "configure terminal" \
            -c "no router bgp ${EVPN_EXTERNAL_ASN} vrf ${EVPN_L3_VRF}" \
            -c "end" 2>/dev/null || true
    fi

    # Step 4: Remove VRF from FRR
    if [ -n "${EVPN_L3_VRF:-}" ]; then
        vtysh_in "$container" \
            -c "configure terminal" \
            -c "no vrf ${EVPN_L3_VRF}" \
            -c "end" 2>/dev/null || true
    fi
}

# =============================================================================
# Orchestration
# =============================================================================

run_setup() {
    log "Starting EVPN setup..."
    log "  Nodes:        $EVPN_NODES"
    log "  External FRR: $EVPN_EXTERNAL"
    log "  L2 VNI/VLAN:  ${EVPN_L2_VNI:-none}/${EVPN_L2_VLAN_ID:-none}"
    log "  L3 VNI/VLAN:  ${EVPN_L3_VNI:-none}/${EVPN_L3_VLAN_ID:-none}"
    log "  L3 VRF:       ${EVPN_L3_VRF:-none}"
    log "  External ASN: $EVPN_EXTERNAL_ASN"
    log "  frr-k8s ASN:  $EVPN_FRR_K8S_ASN"
    log "  Bridge/VXLAN: $EVPN_BRIDGE/$EVPN_VXLAN"

    # --- Kind nodes ---
    local node_index=1
    for node in $EVPN_NODES; do
        local node_ip
        node_ip=$(get_container_ip "$node")
        log "[$node] VTEP IP: $node_ip"

        setup_bridge_vxlan "$node" "$node_ip"
        [ -n "${EVPN_L2_VNI:-}" ] && setup_l2_vni "$node" "$node_index"
        [ -n "${EVPN_L3_VNI:-}" ] && setup_l3_vni "$node" "$node_index"

        node_index=$((node_index + 1))
    done

    # --- External FRR container ---
    local ext_ip
    ext_ip=$(get_container_ip "$EVPN_EXTERNAL")
    log "[$EVPN_EXTERNAL] VTEP IP: $ext_ip"

    setup_bridge_vxlan "$EVPN_EXTERNAL" "$ext_ip"
    # Use index 10 for external container to avoid IP collisions with nodes
    [ -n "${EVPN_L2_VNI:-}" ] && setup_l2_vni "$EVPN_EXTERNAL" 10
    [ -n "${EVPN_L3_VNI:-}" ] && setup_l3_vni "$EVPN_EXTERNAL" 10

    configure_external_frr "$EVPN_EXTERNAL"

    # Allow time for EVPN fabric convergence across all VTEPs
    log "Waiting for EVPN fabric convergence..."
    sleep 5

    log "EVPN setup complete"
}

run_cleanup() {
    log "Starting EVPN cleanup..."

    # Clean external FRR config before removing its network devices
    cleanup_external_frr "$EVPN_EXTERNAL"
    cleanup_networking "$EVPN_EXTERNAL"

    for node in $EVPN_NODES; do
        cleanup_networking "$node"
    done

    log "EVPN cleanup complete"
}

# =============================================================================
# Main
# =============================================================================

validate_env

if [ "${EVPN_CLEANUP:-}" = "true" ]; then
    run_cleanup
else
    run_setup
fi
