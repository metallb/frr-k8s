# EVPN Support for FRRConfiguration

## Summary

This proposal adds Ethernet VPN (EVPN) configuration support to the FRRConfiguration CRD in frr-k8s. EVPN is a BGP-based control plane for VXLAN overlays, enabling Layer 2 and Layer 3 VPN connectivity with better scalability and multi-tenancy support compared to traditional overlay protocols.

The primary motivation is to support the [OVN-Kubernetes EVPN enhancement](https://github.com/ovn-kubernetes/ovn-kubernetes/pull/5089), which requires advertising User Defined Networks (UDNs) externally via EVPN. This enhancement enables OVN-Kubernetes to integrate with external networks using standard EVPN/VXLAN technology instead of proprietary overlay protocols.

This proposal focuses on extending the FRRConfiguration API with EVPN-specific fields, adding validation, and implementing FRR configuration rendering. No architectural changes to frr-k8s are required.

## Motivation

OVN-Kubernetes is adding support for exposing User Defined Networks (UDNs) to external networks using EVPN. This enables:

1. **External connectivity**: Kubernetes workloads can communicate with external networks (VMs, bare-metal, other clusters) using EVPN/VXLAN
2. **Multi-tenancy**: Extending user defined networks isolation to the provider network allowing overlapping IP spaces without conflict

Currently, frr-k8s only supports basic BGP unicast route advertisement. OVN-Kubernetes needs the ability to:
- Configure BGP neighbors for the L2VPN EVPN address family
- Advertise VXLAN Network Identifiers (VNIs) for both L2 and L3 VPNs
- Configure route distinguishers (RD) for route traceability and route targets (RT) for controlling route distribution between VRFs
- Advertise unicast prefixes as EVPN type-5 routes for L3 VPN connectivity

### Goals

- Add EVPN configuration to the FRRConfiguration CRD API
- Support both Layer 2 VNI (MAC-VRF, bridging) and Layer 3 VNI (IP-VRF, routing) configuration
- Enable BGP neighbor activation for the L2VPN EVPN address family
- Support route distinguisher (RD) and route target (RT) configuration for VNIs

### Non-Goals

The following features are not required by OVN-Kubernetes and are therefore out of scope for this proposal:

- **Route-map based filtering and customization**: Advanced route filtering and customization (prefix filtering, VNI filtering, route modification) via route-maps
- **Advanced EVPN features**: Multi-homing, BUM handling customization, advertise-PIP, advertise-svi-ip, and other advanced EVPN capabilities beyond basic L2/L3 VPN functionality
- **Alternative data planes**: This proposal only supports EVPN over VXLAN. Other data planes (MPLS, SR-MPLS, SRv6) are out of scope

## Proposal

### User Stories

As a **cluster administrator**, I want to:

1. **Configure EVPN neighbors**: Enable BGP sessions with external EVPN routers (route reflectors or VTEPs) by activating the L2VPN EVPN address family on existing BGP neighbors.

2. **Advertise L2 VNIs**: Advertise L2 VNIs to EVPN neighbors, enabling Layer 2 connectivity to external networks.

3. **Advertise L3 VNIs**: Advertise L3 VNIs with explicitly configured unicast prefixes to EVPN neighbors, enabling Layer 3 connectivity to external networks.

4. **Customize route parameters**: Override the route distinguisher and route targets for VNIs to control route distribution.

### Design Details

#### API Changes

The proposal adds an `evpn` field to the existing `Router` configuration and an `addressFamilies` field to the `Neighbor` configuration.

**Router Configuration:**

```yaml
apiVersion: frrk8s.metallb.io/v1beta1
kind: FRRConfiguration
metadata:
  name: evpn-example
spec:
  bgp:
    routers:
    - asn: 64512
      neighbors:
      - address: 192.168.1.1
        asn: 64512
        addressFamilies: ["unicast", "evpn"]  # NEW: activate EVPN address family
      prefixes:
      - 100.64.0.1/32
      evpn:                                    # NEW: EVPN configuration
        advertiseVNIs: All                     # Advertise all VNIs
        l2vnis:                                 # Layer 2 VNI customizations
        - vni: 1000
          rd: "64512:1000"
          importRTs: ["64512:1000"]
          exportRTs: ["64512:1000"]
    - asn: 64512
      vrf: tenant-red
      prefixes:
      - 10.0.1.0/24
      evpn:
        l3vni:                                  # Layer 3 VNI configuration
          vni: 2000
          rd: "64512:2000"
          importRTs: ["64512:2000", "*:999"]   # Supports wildcard RTs
          exportRTs: ["64512:2000"]
          advertisePrefixes: ["unicast"]        # Advertise unicast routes
```

**New API Types:**

```go
// Neighbor gets a new field:
type Neighbor struct {
    // ... existing fields ...

    // AddressFamilies specifies which address families to activate this
    // neighbor for. Supported values: "unicast", "evpn".
    // +optional
    // +kubebuilder:default:={"unicast"}
    // +kubebuilder:validation:MaxItems=2
    // +kubebuilder:validation:Enum=unicast;evpn
    AddressFamilies []string `json:"addressFamilies,omitempty"`
}

// Router gets a new field:
type Router struct {
    // ... existing fields ...

    // EVPN is the EVPN configuration for this router.
    // +optional
    EVPN *EVPNConfig `json:"evpn,omitempty"`
}

// EVPNConfig contains EVPN-specific configuration.
type EVPNConfig struct {
    // AdvertiseVNIs controls how VNIs are advertised to EVPN neighbors.
    // - "Disabled": No VNI advertisements
    // - "All": Advertise all VNIs (enables advertise-all-vni)
    // Note: Can only be provided for router instances with EVPN neighbors.
    // +optional
    // +kubebuilder:validation:Enum=Disabled;All
    AdvertiseVNIs *VNIAdvertisement `json:"advertiseVNIs,omitempty"`

    // L2VNIs contains configuration for Layer 2 VNIs (MAC-VRF).
    // Note: Can only be provided for router instances with EVPN neighbors.
    // +optional
    L2VNIs []L2VNI `json:"l2vnis,omitempty"`

    // L3VNI contains configuration for the Layer 3 VNI (IP-VRF).
    // +optional
    L3VNI *L3VNI `json:"l3vni,omitempty"`
}

// VNIAdvertisement defines how VNIs are advertised in EVPN.
type VNIAdvertisement string

const (
    VNIAdvertisementDisabled VNIAdvertisement = "Disabled"
    VNIAdvertisementAll VNIAdvertisement = "All"
)

// VNIBase contains common fields for all VNI types.
type VNIBase struct {
    // VNI is the VXLAN Network Identifier (1-16777215).
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:Minimum=1
    // +kubebuilder:validation:Maximum=16777215
    VNI uint32 `json:"vni"`

    // RD is the route distinguisher for this VNI.
    // Format: A.B.C.D:MN|EF:OPQR|GHJK:MN (e.g., "65000:100" or "192.0.2.1:100")
    // +optional
    RD RouteDistinguisher `json:"rd,omitempty"`

    // ImportRTs is the list of route targets to import.
    // Format: A.B.C.D:MN|EF:OPQR|GHJK:MN|*:OPQR|*:MN (e.g., "65000:100" or "192.0.2.1:100" or "*:42000000")
    // +optional
    // +kubebuilder:validation:MaxItems=100
    ImportRTs []RouteTarget `json:"importRTs,omitempty"`

    // ExportRTs is the list of route targets to export.
    // Format: A.B.C.D:MN|EF:OPQR|GHJK:MN|*:OPQR|*:MN (e.g., "65000:100" or "192.0.2.1:100" or "*:42000000")
    // +optional
    // +kubebuilder:validation:MaxItems=100
    ExportRTs []RouteTarget `json:"exportRTs,omitempty"`
}

// L2VNI represents a Layer 2 VNI configuration (MAC-VRF).
type L2VNI struct {
    VNIBase `json:",inline"`
}

// L3VNI represents a Layer 3 VNI configuration (IP-VRF).
type L3VNI struct {
    VNIBase `json:",inline"`

    // AdvertisePrefixes controls which prefixes to advertise as EVPN type-5 routes.
    // - "unicast": advertise the unicast prefixes of the router
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:MinItems=1
    // +kubebuilder:validation:MaxItems=1
    // +kubebuilder:validation:Enum=unicast
    AdvertisePrefixes []string `json:"advertisePrefixes"`
}

// RouteDistinguisher defines an 8-byte BGP identifier.
// +kubebuilder:validation:XValidation:rule="self.split(':').size() == 2",message="RD must contain exactly one colon"
// +kubebuilder:validation:XValidation:rule="!self.split(':')[0].contains('.') || (self.split(':')[0].isIP() && uint(self.split(':')[1]) <= 65535u)",message="RD with IPv4 administrator must have format A.B.C.D:MN where MN <= 65535"
// +kubebuilder:validation:XValidation:rule="self.split(':')[0].contains('.') || uint(self.split(':')[0]) <= 65535u || uint(self.split(':')[1]) <= 65535u",message="RD with 4-byte ASN administrator must have format GHJK:MN where GHJK <= 4294967295 and MN <= 65535"
// +kubebuilder:validation:XValidation:rule="self.split(':')[0].contains('.') || uint(self.split(':')[0]) > 65535u || uint(self.split(':')[1]) <= 4294967295u",message="RD with 2-byte ASN administrator must have format EF:OPQR where EF <= 65535 and OPQR <= 4294967295"
type RouteDistinguisher string

// RouteTarget defines a BGP Extended Community for route filtering.
// +kubebuilder:validation:XValidation:rule="self.split(':').size() == 2",message="RT must contain exactly one colon"
// +kubebuilder:validation:XValidation:rule="self.split(':')[0] != '*' || uint(self.split(':')[1]) <= 4294967295u",message="RT with wildcard administrator must have format *:OPQR where OPQR <= 4294967295"
// +kubebuilder:validation:XValidation:rule="!self.split(':')[0].contains('.') || (self.split(':')[0].isIP() && uint(self.split(':')[1]) <= 65535u)",message="RT with IPv4 administrator must have format A.B.C.D:MN where MN <= 65535"
// +kubebuilder:validation:XValidation:rule="self.split(':')[0] == '*' || self.split(':')[0].contains('.') || uint(self.split(':')[0]) <= 65535u || uint(self.split(':')[1]) <= 65535u",message="RT with 4-byte ASN administrator must have format GHJK:MN where GHJK <= 4294967295 and MN <= 65535"
// +kubebuilder:validation:XValidation:rule="self.split(':')[0] == '*' || self.split(':')[0].contains('.') || uint(self.split(':')[0]) > 65535u || uint(self.split(':')[1]) <= 4294967295u",message="RT with 2-byte ASN administrator must have format EF:OPQR where EF <= 65535 and OPQR <= 4294967295"
type RouteTarget string

```

#### Key Design Decisions

1. **Neighbor Address Families**: A new `addressFamilies` field on neighbors enables activation for different BGP address families. This allows existing neighbors to be activated for EVPN without creating separate neighbor entries or cross-referencing neighbors from other places.

2. **VNI Advertisement**: The `advertiseVNIs` field enables control over VNI advertisement behavior:
   - Omitted or "Disabled": VNIs are not advertised (e.g., route reflector or IP-VRF only router), conservatively not advertising unless explicitly specified
   - "All": Enable `advertise-all-vni` for L2 VNI auto-discovery

   The field is a pointer so it remains absent from configurations where it's not relevant, avoiding unnecessary clutter.

3. **Scope Restrictions**: Both `advertiseVNIs` and `l2vnis` can only be provided for router instances with EVPN neighbors (i.e., routers participating in the EVPN underlay). VRF routers advertising type-5 routes only configure `l3vni`.

#### FRR Configuration Output

For the YAML example above, frr-k8s will generate the following FRR configuration:

**Default VRF Router (EVPN underlay with L2 VNI):**
```
router bgp 64512
  neighbor 192.168.1.1 remote-as 64512
  address-family ipv4 unicast
    neighbor 192.168.1.1 activate
    network 100.64.0.1/32
  exit-address-family
  address-family l2vpn evpn
    neighbor 192.168.1.1 activate
    advertise-all-vni
    vni 1000
      rd 64512:1000
      route-target import 64512:1000
      route-target export 64512:1000
    exit-vni
  exit-address-family
!
```

**VRF Router (L3 VNI with type-5 routes):**
```
vrf tenant-red
  vni 2000
exit-vrf
!
router bgp 64512 vrf tenant-red
  address-family ipv4 unicast
    network 10.0.1.0/24
  exit-address-family
  address-family l2vpn evpn
    advertise ipv4 unicast
    rd 64512:2000
    route-target import 64512:2000
    route-target import *:999
    route-target export 64512:2000
  exit-address-family
!
```

#### Impacts on Users

- New optional `evpn` field on router configuration
- New optional `addressFamilies` field on neighbor configuration (defaults to `["unicast"]` for backward compatibility)
- EVPN configuration is entirely opt-in; existing configurations are unaffected
- All new fields are optional
- Default behavior for existing configurations remains unchanged
- The `addressFamilies` field defaults to `["unicast"]`, maintaining current behavior

#### Validation

Admission webhooks will enforce:

1. **Unique VNI numbers**: VNI numbers must be unique across all routers in a FRRConfiguration
2. **Scope validation**: `advertiseVNIs` and `l2vnis` can only be configured on routers with EVPN neighbors

#### Metrics

TBD.

### Test Plan

#### Unit Tests

1. **Webhook validation tests** (`internal/webhooks/frrconfiguration_webhook_test.go`):
   - Duplicate VNI detection across routers
   - Scope validation for advertiseVNIs and l2vnis

2. **API-to-config translation tests** (`internal/controller/api_to_config_test.go`):
   - L2VNI translation
   - L3VNI translation
   - advertiseVNIs translation
   - Neighbor address family translation

3. **Template rendering tests** (`internal/frr/frr_test.go`):
   - EVPN neighbor activation rendering
   - advertise-all-vni rendering
   - VNI block rendering with RD/RTs
   - L3VNI prefix advertisement rendering
   - VRF declaration with VNI binding

#### Integration Tests

Using the existing external FRR containers infrastructure:

1. **Basic EVPN session establishment**:
   - Configure FRRConfiguration with EVPN neighbor
   - Verify EVPN session comes up
   - Verify EVPN routes are exchanged

2. **L2 VNI advertisement**:
   - Configure L2VNI with RD and RTs
   - Verify VNI is advertised to EVPN peer
   - Verify custom RD/RT appear in routes

3. **L3 VNI advertisement**:
   - Configure L3VNI with RD and RTs
   - Verify VNI is advertised to EVPN peer
   - Verify custom RD/RT appear in routes

4. **Route target filtering**:
   - Configure different import/export RTs
   - Verify only routes with matching RTs are imported
   - Test wildcard route target matching

5. **Configuration composition**:
   - Multiple FRRConfigurations affecting different aspects
   - Verify merging works correctly

6. **Configuration updates**:
   - Add/remove VNIs dynamically
   - Change RD/RT values
   - Verify FRR configuration is updated correctly

7. **Negative tests**:
   - Duplicate VNI numbers (should be rejected)
   - Invalid RD/RT formats (should be rejected)
   - Invalid configuration in non underlay EVPN routers (should be rejected)

## Alternatives

### Flat Design Without EVPN Config Structure

An alternative approach would be to use a flatter design without the dedicated `EVPN` configuration structure. Instead, EVPN-specific filtering could be handled by extending the existing `ToAdvertise` and `ToReceive` fields with address family qualification, similar to how we qualify neighbor address families.

This approach could potentially simplify future extensions for EVPN-specific route filtering. However, the current nested design was chosen as it:
- Provides clearer separation between unicast and EVPN configuration
- Allows for future neighbor-level and VNI-level route-map support within the EVPN structure
- Makes the EVPN-specific scope restrictions (e.g., `advertiseVNIs` only valid for routers with EVPN neighbors) more explicit

## Development Phases

### Phase 1: API and CRD Generation
- Add EVPN types to `api/v1beta1/frrconfiguration_types.go`
- Add AddressFamilies field to Neighbor
- Add EVPN field to Router
- Generate CRDs with `make manifests`
- Generate deepcopy functions with `make generate`

### Phase 2: Webhook Validation
- Implement `validateEVPNConfig()` in `internal/webhooks/frrconfiguration_webhook.go`
- Add duplicate VNI detection
- Add duplicate route target detection
- Add scope validation
- Add unit tests for validation

### Phase 3: Internal Types and Translation
- Add EVPN types to `internal/frr/config.go`
- Implement translation in `internal/controller/api_to_config.go`
- Add unit tests for translation

### Phase 4: Template Rendering
- Create `internal/frr/templates/evpn.tmpl`
- Update `internal/frr/templates/frr.tmpl` to include EVPN template
- Handle address family activation for neighbors
- Handle VRF declaration with VNI binding
- Handle L2 VNI blocks in EVPN address family
- Handle L3 VNI RD/RT in EVPN address family (no vni block)
- Add unit tests with golden file comparison

### Phase 5: Integration Testing
- Set up EVPN-capable external FRR containers
- Implement integration tests covering all use cases
- Verify EVPN session establishment
- Verify route advertisement and filtering

### Phase 6: Documentation
- Update API documentation
- Add EVPN configuration examples
- Document OVN-Kubernetes integration patterns
