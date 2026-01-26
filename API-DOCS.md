# API Reference

## Packages
- [frrk8s.metallb.io/v1beta1](#frrk8smetallbiov1beta1)


## frrk8s.metallb.io/v1beta1

Package v1alpha1 contains API Schema definitions for the frrk8s v1alpha1 API group

### Resource Types
- [BGPSessionState](#bgpsessionstate)
- [FRRConfiguration](#frrconfiguration)
- [FRRNodeState](#frrnodestate)



#### Advertise







_Appears in:_
- [Neighbor](#neighbor)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `allowed` _[AllowedOutPrefixes](#allowedoutprefixes)_ | Allowed is is the list of prefixes allowed to be propagated to<br />this neighbor. They must match the prefixes defined in the router. |  |  |
| `withLocalPref` _[LocalPrefPrefixes](#localprefprefixes) array_ | PrefixesWithLocalPref is a list of prefixes that are associated to a local<br />preference when being advertised. The prefixes associated to a given local pref<br />must be in the prefixes allowed to be advertised. |  |  |
| `withCommunity` _[CommunityPrefixes](#communityprefixes) array_ | PrefixesWithCommunity is a list of prefixes that are associated to a<br />bgp community when being advertised. The prefixes associated to a given local pref<br />must be in the prefixes allowed to be advertised. |  |  |


#### AllowedInPrefixes







_Appears in:_
- [Receive](#receive)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `prefixes` _[PrefixSelector](#prefixselector) array_ |  |  |  |


#### AllowedOutPrefixes







_Appears in:_
- [Advertise](#advertise)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `prefixes` _string array_ |  |  |  |


#### BFDProfile



BFDProfile is the configuration related to the BFD protocol associated
to a BGP session.



_Appears in:_
- [BGPConfig](#bgpconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | The name of the BFD Profile to be referenced in other parts<br />of the configuration. |  |  |
| `receiveInterval` _integer_ | The minimum interval that this system is capable of<br />receiving control packets in milliseconds.<br />Defaults to 300ms. |  | Maximum: 60000 <br />Minimum: 10 <br /> |
| `transmitInterval` _integer_ | The minimum transmission interval (less jitter)<br />that this system wants to use to send BFD control packets in<br />milliseconds. Defaults to 300ms |  | Maximum: 60000 <br />Minimum: 10 <br /> |
| `detectMultiplier` _integer_ | Configures the detection multiplier to determine<br />packet loss. The remote transmission interval will be multiplied<br />by this value to determine the connection loss detection timer. |  | Maximum: 255 <br />Minimum: 2 <br /> |
| `echoInterval` _integer_ | Configures the minimal echo receive transmission<br />interval that this system is capable of handling in milliseconds.<br />Defaults to 50ms |  | Maximum: 60000 <br />Minimum: 10 <br /> |
| `echoMode` _boolean_ | Enables or disables the echo transmission mode.<br />This mode is disabled by default, and not supported on multi<br />hops setups. |  |  |
| `passiveMode` _boolean_ | Mark session as passive: a passive session will not<br />attempt to start the connection and will wait for control packets<br />from peer before it begins replying. |  |  |
| `minimumTtl` _integer_ | For multi hop sessions only: configure the minimum<br />expected TTL for an incoming BFD control packet. |  | Maximum: 254 <br />Minimum: 1 <br /> |


#### BGPConfig



BGPConfig is the configuration related to the BGP protocol.



_Appears in:_
- [FRRConfigurationSpec](#frrconfigurationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `routers` _[Router](#router) array_ | Routers is the list of routers we want FRR to configure (one per VRF). |  |  |
| `bfdProfiles` _[BFDProfile](#bfdprofile) array_ | BFDProfiles is the list of bfd profiles to be used when configuring the neighbors. |  |  |


#### BGPSessionState



BGPSessionState exposes the status of a BGP Session from the FRR instance running on the node.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `frrk8s.metallb.io/v1beta1` | | |
| `kind` _string_ | `BGPSessionState` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[BGPSessionStateSpec](#bgpsessionstatespec)_ |  |  |  |
| `status` _[BGPSessionStateStatus](#bgpsessionstatestatus)_ |  |  |  |


#### BGPSessionStateSpec



BGPSessionStateSpec defines the desired state of BGPSessionState.



_Appears in:_
- [BGPSessionState](#bgpsessionstate)



#### BGPSessionStateStatus



BGPSessionStateStatus defines the observed state of BGPSessionState.



_Appears in:_
- [BGPSessionState](#bgpsessionstate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `bgpStatus` _string_ |  |  |  |
| `bfdStatus` _string_ |  |  |  |
| `node` _string_ |  |  |  |
| `peer` _string_ |  |  |  |
| `vrf` _string_ |  |  |  |


#### CommunityPrefixes



CommunityPrefixes is a list of prefixes associated to a community.



_Appears in:_
- [Advertise](#advertise)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `prefixes` _string array_ | Prefixes is the list of prefixes associated to the community. |  | Format: cidr <br />MinItems: 1 <br /> |
| `community` _string_ | Community is the community associated to the prefixes. |  |  |


#### DynamicASNMode

_Underlying type:_ _string_





_Appears in:_
- [Neighbor](#neighbor)

| Field | Description |
| --- | --- |
| `internal` |  |
| `external` |  |


#### EVPNConfig



EVPNConfig contains configuration related to EVPN.



_Appears in:_
- [Router](#router)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `advertiseVNIs` _[VNIAdvertisement](#vniadvertisement)_ | AdvertiseVNIs controls how VNIs are advertised to EVPN neighbors.<br />- "Disabled": No VNI advertisements<br />- "All": Avertise all VNIs<br />Note: Can only be provided for router instances with EVPN neighbors. |  | Enum: [Disabled All] <br /> |
| `advertiseSVI` _boolean_ | AdvertiseSVI enables advertising the SVI IP/MAC as a type-2 route. |  |  |
| `l2vnis` _[L2VNI](#l2vni) array_ | L2VNIs contains configuration for Layer 2 VNIs.<br />Note: Can only be provided for router instances with EVPN neighbors. |  |  |
| `l3vni` _[L3VNI](#l3vni)_ | L3VNI contains configuration for the Layer 3 VNI.<br />Note: Can only be provided for router instances with no neighbors.<br />This is a temporary limitation until proper EVPN prefix filtering is implemented. |  |  |


#### ExportRouteTarget

_Underlying type:_ _string_

ExportRouteTarget defines a BGP Extended Community for route filtering on export.
Does NOT support wildcard matching (wildcards are only valid for import).



_Appears in:_
- [L2VNI](#l2vni)
- [L3VNI](#l3vni)
- [VNI](#vni)



#### FRRConfiguration



FRRConfiguration is a piece of FRR configuration.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `frrk8s.metallb.io/v1beta1` | | |
| `kind` _string_ | `FRRConfiguration` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[FRRConfigurationSpec](#frrconfigurationspec)_ |  |  |  |
| `status` _[FRRConfigurationStatus](#frrconfigurationstatus)_ |  |  |  |


#### FRRConfigurationSpec



FRRConfigurationSpec defines the desired state of FRRConfiguration.



_Appears in:_
- [FRRConfiguration](#frrconfiguration)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `bgp` _[BGPConfig](#bgpconfig)_ | BGP is the configuration related to the BGP protocol. |  |  |
| `raw` _[RawConfig](#rawconfig)_ | Raw is a snippet of raw frr configuration that gets appended to the<br />one rendered translating the type safe API. |  |  |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#labelselector-v1-meta)_ | NodeSelector limits the nodes that will attempt to apply this config.<br />When specified, the configuration will be considered only on nodes<br />whose labels match the specified selectors.<br />When it is not specified all nodes will attempt to apply this config. |  |  |


#### FRRConfigurationStatus



FRRConfigurationStatus defines the observed state of FRRConfiguration.



_Appears in:_
- [FRRConfiguration](#frrconfiguration)



#### FRRNodeState



FRRNodeState exposes the status of the FRR instance running on each node.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `frrk8s.metallb.io/v1beta1` | | |
| `kind` _string_ | `FRRNodeState` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[FRRNodeStateSpec](#frrnodestatespec)_ |  |  |  |
| `status` _[FRRNodeStateStatus](#frrnodestatestatus)_ |  |  |  |


#### FRRNodeStateSpec



FRRNodeStateSpec defines the desired state of FRRNodeState.



_Appears in:_
- [FRRNodeState](#frrnodestate)



#### FRRNodeStateStatus



FRRNodeStateStatus defines the observed state of FRRNodeState.



_Appears in:_
- [FRRNodeState](#frrnodestate)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `runningConfig` _string_ | RunningConfig represents the current FRR running config, which is the configuration the FRR instance is currently running with. |  |  |
| `lastConversionResult` _string_ | LastConversionResult is the status of the last translation between the `FRRConfiguration`s resources and FRR's configuration, contains "success" or an error. |  |  |
| `lastReloadResult` _string_ | LastReloadResult represents the status of the last configuration update operation by FRR, contains "success" or an error. |  |  |


#### Import



Import represents the possible imported VRFs to a given router.



_Appears in:_
- [Router](#router)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `vrf` _string_ | Vrf is the vrf we want to import from |  |  |


#### ImportRouteTarget

_Underlying type:_ _string_

ImportRouteTarget defines a BGP Extended Community for route filtering on import.
Supports wildcard matching with "*" as the global administrator (e.g., "*:100").



_Appears in:_
- [L2VNI](#l2vni)
- [L3VNI](#l3vni)
- [VNI](#vni)



#### L2VNI



L2VNI represents a Layer 2 VNI configuration.



_Appears in:_
- [EVPNConfig](#evpnconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `vni` _integer_ | VNI is the VXLAN Network Identifier (1-16777215). |  | Maximum: 1.6777215e+07 <br />Minimum: 1 <br />Required: \{\} <br /> |
| `rd` _[RouteDistinguisher](#routedistinguisher)_ | RD is the route distinguisher for this VNI.<br />Format: A.B.C.D:MN\|EF:OPQR\|GHJK:MN (e.g., "65000:100" or "192.0.2.1:100") |  |  |
| `importRTs` _[ImportRouteTarget](#importroutetarget) array_ | ImportRTs is the list of route targets to import.<br />Format: A.B.C.D:MN\|EF:OPQR\|GHJK:MN\|*:MN\|*:OPQR (e.g., "65000:100", "192.0.2.1:100", "*:100") |  | MaxItems: 100 <br /> |
| `exportRTs` _[ExportRouteTarget](#exportroutetarget) array_ | ExportRTs is the list of route targets to export.<br />Format: A.B.C.D:MN\|EF:OPQR\|GHJK:MN (e.g., "65000:100", "192.0.2.1:100") |  | MaxItems: 100 <br /> |


#### L3VNI



L3VNI represents a Layer 3 VNI configuration.



_Appears in:_
- [EVPNConfig](#evpnconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `vni` _integer_ | VNI is the VXLAN Network Identifier (1-16777215). |  | Maximum: 1.6777215e+07 <br />Minimum: 1 <br />Required: \{\} <br /> |
| `rd` _[RouteDistinguisher](#routedistinguisher)_ | RD is the route distinguisher for this VNI.<br />Format: A.B.C.D:MN\|EF:OPQR\|GHJK:MN (e.g., "65000:100" or "192.0.2.1:100") |  |  |
| `importRTs` _[ImportRouteTarget](#importroutetarget) array_ | ImportRTs is the list of route targets to import.<br />Format: A.B.C.D:MN\|EF:OPQR\|GHJK:MN\|*:MN\|*:OPQR (e.g., "65000:100", "192.0.2.1:100", "*:100") |  | MaxItems: 100 <br /> |
| `exportRTs` _[ExportRouteTarget](#exportroutetarget) array_ | ExportRTs is the list of route targets to export.<br />Format: A.B.C.D:MN\|EF:OPQR\|GHJK:MN (e.g., "65000:100", "192.0.2.1:100") |  | MaxItems: 100 <br /> |
| `advertisePrefixes` _string array_ | AdvertisePrefixes controls which prefixes to advertise as EVPN type-5 routes.<br />- "unicast": advertise the unicast prefixes of the router. |  | Enum: [unicast] <br />MaxItems: 1 <br />MinItems: 1 <br />Required: \{\} <br /> |


#### LocalPrefPrefixes



LocalPrefPrefixes is a list of prefixes associated to a local preference.



_Appears in:_
- [Advertise](#advertise)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `prefixes` _string array_ | Prefixes is the list of prefixes associated to the local preference. |  | Format: cidr <br />MinItems: 1 <br /> |
| `localPref` _integer_ | LocalPref is the local preference associated to the prefixes. |  |  |


#### Neighbor



Neighbor represents a BGP Neighbor we want FRR to connect to.



_Appears in:_
- [Router](#router)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `asn` _integer_ | ASN is the AS number to use for the local end of the session.<br />ASN and DynamicASN are mutually exclusive and one of them must be specified. |  | Maximum: 4.294967295e+09 <br />Minimum: 0 <br /> |
| `dynamicASN` _[DynamicASNMode](#dynamicasnmode)_ | DynamicASN detects the AS number to use for the local end of the session<br />without explicitly setting it via the ASN field. Limited to:<br />internal - if the neighbor's ASN is different than the router's the connection is denied.<br />external - if the neighbor's ASN is the same as the router's the connection is denied.<br />ASN and DynamicASN are mutually exclusive and one of them must be specified. |  | Enum: [internal external] <br /> |
| `sourceaddress` _string_ | SourceAddress is the IPv4 or IPv6 source address to use for the BGP<br />session to this neighbour, may be specified as either an IP address<br />directly or as an interface name |  |  |
| `address` _string_ | Address is the IP address to establish the session with. |  |  |
| `interface` _string_ | Interface is the node interface over which the unnumbered BGP peering will<br />be established. No API validation takes place as that string value<br />represents an interface name on the host and if user provides an invalid<br />value, only the actual BGP session will not be established.<br />Address and Interface are mutually exclusive and one of them must be specified.<br />Note: when enabling unnumbered, the neighbor will be enabled for both<br />IPv4 and IPv6 address families. |  |  |
| `port` _integer_ | Port is the port to dial when establishing the session.<br />Defaults to 179. |  | Maximum: 16384 <br />Minimum: 0 <br /> |
| `password` _string_ | Password to be used for establishing the BGP session.<br />Password and PasswordSecret are mutually exclusive. |  |  |
| `passwordSecret` _[SecretReference](#secretreference)_ | PasswordSecret is name of the authentication secret for the neighbor.<br />the secret must be of type "kubernetes.io/basic-auth", and created in the<br />same namespace as the frr-k8s daemon. The password is stored in the<br />secret as the key "password".<br />Password and PasswordSecret are mutually exclusive. |  |  |
| `holdTime` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#duration-v1-meta)_ | HoldTime is the requested BGP hold time, per RFC4271.<br />Defaults to 180s. |  |  |
| `keepaliveTime` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#duration-v1-meta)_ | KeepaliveTime is the requested BGP keepalive time, per RFC4271.<br />Defaults to 60s. |  |  |
| `connectTime` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#duration-v1-meta)_ | Requested BGP connect time, controls how long BGP waits between connection attempts to a neighbor. |  |  |
| `ebgpMultiHop` _boolean_ | EBGPMultiHop indicates if the BGPPeer is multi-hops away. |  |  |
| `bfdProfile` _string_ | BFDProfile is the name of the BFD Profile to be used for the BFD session associated<br />to the BGP session. If not set, the BFD session won't be set up. |  |  |
| `enableGracefulRestart` _boolean_ | EnableGracefulRestart allows BGP peer to continue to forward data packets along<br />known routes while the routing protocol information is being restored. If<br />the session is already established, the configuration will have effect<br />after reconnecting to the peer |  |  |
| `toAdvertise` _[Advertise](#advertise)_ | ToAdvertise represents the list of prefixes to advertise to the given neighbor<br />and the associated properties. Only applies to IPv4 and IPv6 unicast address families. |  |  |
| `toReceive` _[Receive](#receive)_ | ToReceive represents the list of prefixes to receive from the given neighbor.<br />Only applies to IPv4 and IPv6 unicast address families. |  |  |
| `disableMP` _boolean_ | DisableMP is no longer used and has no effect.<br />Use DualStackAddressFamily instead to enable the neighbor for both IPv4 and IPv6 address families.<br />Deprecated: DisableMP is deprecated in favor of DualStackAddressFamily. | false |  |
| `dualStackAddressFamily` _boolean_ | To set if we want to enable the neighbor not only for the ipfamily related to its session,<br />but also the other one. This allows to advertise/receive IPv4 prefixes over IPv6 sessions and vice versa. | false |  |
| `addressFamilies` _string array_ | AddressFamilies specifies which address families to activate this neighbor for.<br />Supported values: "unicast" (IPv4/IPv6 unicast based on neighbor IP), "evpn" (L2VPN EVPN).<br />Defaults to "unicast" when not specified. | ["unicast"] | Enum: [unicast evpn] <br />MaxItems: 2 <br /> |


#### PrefixSelector



PrefixSelector is a filter of prefixes to receive.



_Appears in:_
- [AllowedInPrefixes](#allowedinprefixes)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `prefix` _string_ |  |  | Format: cidr <br /> |
| `le` _integer_ | The prefix length modifier. This selector accepts any matching prefix with length<br />less or equal the given value. |  | Maximum: 128 <br />Minimum: 1 <br /> |
| `ge` _integer_ | The prefix length modifier. This selector accepts any matching prefix with length<br />greater or equal the given value. |  | Maximum: 128 <br />Minimum: 1 <br /> |


#### RawConfig



RawConfig is a snippet of raw frr configuration that gets appended to the
rendered configuration.

WARNING: The RawConfig feature is UNSUPPORTED and intended ONLY FOR EXPERIMENTATION.
It should not be used in production environments. This feature is provided as-is without any
guarantees of stability, compatibility, or support. Use at your own risk.



_Appears in:_
- [FRRConfigurationSpec](#frrconfigurationspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `priority` _integer_ | Priority is the order with this configuration is appended to the<br />bottom of the rendered configuration. A higher value means the<br />raw config is appended later in the configuration file. |  |  |
| `rawConfig` _string_ | Config is a raw FRR configuration to be appended to the configuration<br />rendered via the k8s api. |  |  |


#### Receive



Receive represents a list of prefixes to receive from the given neighbor.



_Appears in:_
- [Neighbor](#neighbor)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `allowed` _[AllowedInPrefixes](#allowedinprefixes)_ | Allowed is the list of prefixes allowed to be received from<br />this neighbor. |  |  |


#### RouteDistinguisher

_Underlying type:_ _string_

RouteDistinguisher defines an 8-byte BGP identifier.



_Appears in:_
- [L2VNI](#l2vni)
- [L3VNI](#l3vni)
- [VNI](#vni)



#### Router



Router represent a neighbor router we want FRR to connect to.



_Appears in:_
- [BGPConfig](#bgpconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `asn` _integer_ | ASN is the AS number to use for the local end of the session. |  | Maximum: 4.294967295e+09 <br />Minimum: 0 <br /> |
| `id` _string_ | ID is the BGP router ID |  |  |
| `vrf` _string_ | VRF is the host vrf used to establish sessions from this router. |  |  |
| `neighbors` _[Neighbor](#neighbor) array_ | Neighbors is the list of neighbors we want to establish BGP sessions with. |  |  |
| `prefixes` _string array_ | Prefixes is the list of prefixes we want to advertise from this router instance. |  |  |
| `imports` _[Import](#import) array_ | Imports is the list of imported VRFs we want for this router / vrf. |  |  |
| `evpn` _[EVPNConfig](#evpnconfig)_ | EVPN specific configuration for the router. |  |  |


#### SecretReference



SecretReference represents a Secret Reference. It has enough information to retrieve secret
in any namespace.



_Appears in:_
- [Neighbor](#neighbor)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | name is unique within a namespace to reference a secret resource. |  |  |
| `namespace` _string_ | namespace defines the space within which the secret name must be unique. |  |  |


#### VNI



VNI contains common fields for all VNI types.



_Appears in:_
- [L2VNI](#l2vni)
- [L3VNI](#l3vni)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `vni` _integer_ | VNI is the VXLAN Network Identifier (1-16777215). |  | Maximum: 1.6777215e+07 <br />Minimum: 1 <br />Required: \{\} <br /> |
| `rd` _[RouteDistinguisher](#routedistinguisher)_ | RD is the route distinguisher for this VNI.<br />Format: A.B.C.D:MN\|EF:OPQR\|GHJK:MN (e.g., "65000:100" or "192.0.2.1:100") |  |  |
| `importRTs` _[ImportRouteTarget](#importroutetarget) array_ | ImportRTs is the list of route targets to import.<br />Format: A.B.C.D:MN\|EF:OPQR\|GHJK:MN\|*:MN\|*:OPQR (e.g., "65000:100", "192.0.2.1:100", "*:100") |  | MaxItems: 100 <br /> |
| `exportRTs` _[ExportRouteTarget](#exportroutetarget) array_ | ExportRTs is the list of route targets to export.<br />Format: A.B.C.D:MN\|EF:OPQR\|GHJK:MN (e.g., "65000:100", "192.0.2.1:100") |  | MaxItems: 100 <br /> |


#### VNIAdvertisement

_Underlying type:_ _string_

VNIAdvertisement defines how VNIs are advertised in EVPN.

_Validation:_
- Enum: [Disabled All]

_Appears in:_
- [EVPNConfig](#evpnconfig)

| Field | Description |
| --- | --- |
| `Disabled` | VNIAdvertisementDisabled disables VNI advertisement.<br /> |
| `All` | VNIAdvertisementAll enables advertisement of all VNIs.<br /> |


