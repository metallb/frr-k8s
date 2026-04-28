#!/bin/bash
# =============================================================================
# EVPN Orchestrator for frr-k8s Integration Tests
# =============================================================================
#
# Orchestrates EVPN networking setup across cluster nodes and an external FRR
# container. Delegates per-node networking to evpn-node-setup.sh (which can
# also be used standalone on any node).
#
# What this script does:
#   ON CLUSTER NODES (via kubectl exec into frr-k8s FRR pods):
#   - Runs evpn-node-setup.sh to configure bridge, VXLAN, VNIs, VRFs
#
#   ON EXTERNAL FRR CONTAINER (via container runtime exec):
#   - Runs evpn-node-setup.sh for networking setup
#   - Configures FRR EVPN BGP via vtysh (neighbor activation, advertise-all-vni,
#     VNI RD/RTs, VRF-VNI binding, prefix advertisement)
#
# What this script does NOT do:
#   - Configure FRR/BGP on the frr-k8s side. That's the FRRConfiguration CRD's
#     job -- testing that is the whole point of these integration tests.
#   - Create or manage containers (handled by the e2e test framework or manually)
#
# Topology:
#
#   cluster network
#   ┌─────────────────────────────────────────────────────────┐
#   │                                                         │
#   │  ┌──────────────────────┐   ┌────────────────────────┐  │
#   │  │ cluster node         │   │ external FRR container │  │
#   │  │                      │   │                        │  │
#   │  │  ┌────────────────┐  │   │  ┌────────────────┐    │  │
#   │  │  │ evpnbr (bridge │  │   │  │ evpnbr (bridge)│    │  │
#   │  │  │  VLAN filter)  │  │   │  │                │    │  │
#   │  │  │  ├─ evpnvx     │  │   │  │  ├─ evpnvx     │    │  │
#   │  │  │  │  (VXLAN)    │◄────────│  (VXLAN)       │    │  │
#   │  │  │  ├─ evpnl2br-* │  │   │  │  ├─ evpnl2br-* │    │  │
#   │  │  │  │  (access)   │  │   │  │  │  (access)   │    │  │
#   │  │  │  └─ SVI (.vid) │  │   │  │  └─ SVI (.vid) │    │  │
#   │  │  └───────┬────────┘  │   │  └────────┬───────┘    │  │
#   │  │          │           │   │           │            │  │
#   │  │  FRR (frr-k8s)       │   │  FRR (standalone)      │  │
#   │  │  config from CRD     │   │  config from script    │  │
#   │  └──────────────────────┘   └────────────────────────┘  │
#   └─────────────────────────────────────────────────────────┘
#
# Expected FRRConfiguration CRDs (applied by test or manually, NOT by this script).
# Values must match the environment variables passed to this script.
# One FRRConfiguration per node with a nodeSelector, since each node has
# a distinct prefix in the VRF:
#
#   # Per-node FRRConfiguration (one per cluster node):
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
#         - <node-prefix>  # must match route in VRF on this node
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
#     EVPN_NODES          - Space-separated Kubernetes node names
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
#     EVPN_L3_VRF         - VRF name (e.g., evpnred)
#
#   Control:
#     EVPN_CLEANUP        - Set to "true" to tear down instead of set up
#
#   Advanced:
#     CONTAINER_RUNTIME   - Container runtime command (default: docker)
#     EVPN_NETWORK        - Docker network for external FRR IP discovery (default: kind)
#     EVPN_BRIDGE         - Bridge device name (default: evpnbr)
#     EVPN_VXLAN          - VXLAN device name (default: evpnvx)
#     EVPN_L3_VRF_TABLE   - VRF routing table number (default: 10)
#     FRRK8S_NAMESPACE    - frr-k8s pod namespace (default: frr-k8s-system)
#     FRRK8S_LABEL        - frr-k8s pod label selector (default: app.kubernetes.io/component=frr-k8s)
#     FRRK8S_CONTAINER    - FRR container name in pod (default: frr)
#
# Usage:
#   # L2 VNI only
#   EVPN_NODES="node1 node2" \
#   EVPN_EXTERNAL="ebgp-single-hop" \
#   EVPN_EXTERNAL_ASN=4200000000 \
#   EVPN_FRR_K8S_ASN=64512 \
#   EVPN_L2_VNI=1000 EVPN_L2_VLAN_ID=100 \
#   ./hack/evpn-setup.sh
#
#   # L3 VNI only
#   EVPN_NODES="node1 node2" \
#   EVPN_EXTERNAL="ebgp-single-hop" \
#   EVPN_EXTERNAL_ASN=4200000000 \
#   EVPN_FRR_K8S_ASN=64512 \
#   EVPN_L3_VNI=3000 EVPN_L3_VLAN_ID=4000 EVPN_L3_VRF=evpnred \
#   ./hack/evpn-setup.sh
#
#   # Cleanup
#   EVPN_CLEANUP=true ... ./hack/evpn-setup.sh
#
# =============================================================================

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)

# Defaults
CONTAINER_RUNTIME="${CONTAINER_RUNTIME:-docker}"
EVPN_NETWORK="${EVPN_NETWORK:-kind}"
EVPN_BRIDGE="${EVPN_BRIDGE:-evpnbr}"
EVPN_VXLAN="${EVPN_VXLAN:-evpnvx}"
EVPN_L3_VRF_TABLE="${EVPN_L3_VRF_TABLE:-10}"
FRRK8S_NAMESPACE="${FRRK8S_NAMESPACE:-frr-k8s-system}"
FRRK8S_LABEL="${FRRK8S_LABEL:-app=frr-k8s}"
FRRK8S_CONTAINER="${FRRK8S_CONTAINER:-frr}"

# =============================================================================
# Helpers
# =============================================================================

log() {
    echo "[$(date '+%H:%M:%S')] $1"
}

vtysh_in() {
    local container=$1; shift
    $CONTAINER_RUNTIME exec "$container" vtysh "$@"
}

get_node_ip() {
    local node=$1
    kubectl get node "$node" -o jsonpath='{.status.addresses[?(@.type=="InternalIP")].address}'
}

get_container_ip() {
    local container=$1
    $CONTAINER_RUNTIME inspect \
        -f "{{(index .NetworkSettings.Networks \"${EVPN_NETWORK}\").IPAddress}}" \
        "$container"
}

get_frr_pod() {
    local node=$1
    kubectl get pods -n "$FRRK8S_NAMESPACE" -l "$FRRK8S_LABEL" \
        --field-selector "spec.nodeName=$node" \
        -o jsonpath='{.items[0].metadata.name}'
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
# Node setup delegation
# =============================================================================

# Builds the env var export lines for evpn-node-setup.sh.
# Args: $1=local_ip, $2=l2_ip (or ""), $3=l3_prefix (or "")
build_node_env() {
    local local_ip=$1 l2_ip=$2 l3_prefix=$3

    echo "export EVPN_LOCAL_IP=$local_ip"
    echo "export EVPN_BRIDGE=$EVPN_BRIDGE"
    echo "export EVPN_VXLAN=$EVPN_VXLAN"
    if [ -n "${EVPN_L2_VNI:-}" ]; then
        echo "export EVPN_L2_VNI=$EVPN_L2_VNI"
        echo "export EVPN_L2_VLAN_ID=$EVPN_L2_VLAN_ID"
        echo "export EVPN_L2_IP=$l2_ip"
    fi
    if [ -n "${EVPN_L3_VNI:-}" ]; then
        echo "export EVPN_L3_VNI=$EVPN_L3_VNI"
        echo "export EVPN_L3_VLAN_ID=$EVPN_L3_VLAN_ID"
        echo "export EVPN_L3_VRF=$EVPN_L3_VRF"
        echo "export EVPN_L3_VRF_TABLE=$EVPN_L3_VRF_TABLE"
        echo "export EVPN_L3_PREFIX=$l3_prefix"
    fi
    if [ "${EVPN_CLEANUP:-}" = "true" ]; then
        echo "export EVPN_CLEANUP=true"
    fi
}

# Runs evpn-node-setup.sh on a cluster node via kubectl exec into its frr-k8s pod.
# Args: $1=node_name, $2=local_ip, $3=l2_ip, $4=l3_prefix
run_node_setup_k8s() {
    local node=$1 local_ip=$2 l2_ip=$3 l3_prefix=$4
    local pod
    pod=$(get_frr_pod "$node")
    log "[$node] Running node setup via pod $pod..."
    {
        build_node_env "$local_ip" "$l2_ip" "$l3_prefix"
        cat "$SCRIPT_DIR/evpn-node-setup.sh"
    } | kubectl exec -n "$FRRK8S_NAMESPACE" "$pod" -c "$FRRK8S_CONTAINER" -i -- bash
}

# Runs evpn-node-setup.sh on the external FRR container via container runtime.
# Args: $1=container, $2=local_ip, $3=l2_ip, $4=l3_prefix
run_node_setup_container() {
    local container=$1 local_ip=$2 l2_ip=$3 l3_prefix=$4
    log "[$container] Running node setup via $CONTAINER_RUNTIME..."
    {
        build_node_env "$local_ip" "$l2_ip" "$l3_prefix"
        cat "$SCRIPT_DIR/evpn-node-setup.sh"
    } | $CONTAINER_RUNTIME exec -i "$container" bash
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
configure_external_frr() {
    local container=$1

    log "[$container] Configuring FRR for EVPN..."

    # Discover node IPs for neighbor activation
    local neighbor_ips=""
    for node in $EVPN_NODES; do
        neighbor_ips="$neighbor_ips $(get_node_ip "$node")"
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

    # --- Cluster nodes (via kubectl exec into frr-k8s pods) ---
    local node_index=1
    for node in $EVPN_NODES; do
        local node_ip l2_ip="" l3_prefix=""
        node_ip=$(get_node_ip "$node")
        log "[$node] VTEP IP: $node_ip"

        [ -n "${EVPN_L2_VNI:-}" ] && l2_ip="10.100.0.${node_index}/24"
        [ -n "${EVPN_L3_VNI:-}" ] && l3_prefix="10.200.${node_index}.1/24"

        run_node_setup_k8s "$node" "$node_ip" "$l2_ip" "$l3_prefix"

        node_index=$((node_index + 1))
    done

    # --- External FRR container (via container runtime) ---
    local ext_ip
    ext_ip=$(get_container_ip "$EVPN_EXTERNAL")
    log "[$EVPN_EXTERNAL] VTEP IP: $ext_ip"

    local ext_l2_ip="" ext_l3_prefix=""
    # Use index 10 for external container to avoid IP collisions with nodes
    [ -n "${EVPN_L2_VNI:-}" ] && ext_l2_ip="10.100.0.10/24"
    [ -n "${EVPN_L3_VNI:-}" ] && ext_l3_prefix="10.200.10.1/24"

    run_node_setup_container "$EVPN_EXTERNAL" "$ext_ip" "$ext_l2_ip" "$ext_l3_prefix"

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
    run_node_setup_container "$EVPN_EXTERNAL" "x" "" ""

    for node in $EVPN_NODES; do
        run_node_setup_k8s "$node" "x" "" ""
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
