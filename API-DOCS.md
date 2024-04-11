# API Reference

## Packages
- [frrk8s.metallb.io/v1beta1](#frrk8smetallbiov1beta1)


## frrk8s.metallb.io/v1beta1

Package v1alpha1 contains API Schema definitions for the frrk8s v1alpha1 API group

### Resource Types
- [FRRConfiguration](#frrconfiguration)
- [FRRNodeState](#frrnodestate)



#### Advertise





_Appears in:_
- [Neighbor](#neighbor)

| Field | Description |
| --- | --- |
| `allowed` _[AllowedOutPrefixes](#allowedoutprefixes)_ | Allowed is is the list of prefixes allowed to be propagated to this neighbor. They must match the prefixes defined in the router. |
| `withLocalPref` _[LocalPrefPrefixes](#localprefprefixes) array_ | PrefixesWithLocalPref is a list of prefixes that are associated to a local preference when being advertised. The prefixes associated to a given local pref must be in the prefixes allowed to be advertised. |
| `withCommunity` _[CommunityPrefixes](#communityprefixes) array_ | PrefixesWithCommunity is a list of prefixes that are associated to a bgp community when being advertised. The prefixes associated to a given local pref must be in the prefixes allowed to be advertised. |


#### AllowedInPrefixes





_Appears in:_
- [Receive](#receive)

| Field | Description |
| --- | --- |
| `prefixes` _[PrefixSelector](#prefixselector) array_ |  |
| `mode` _[AllowMode](#allowmode)_ | Mode is the mode to use when handling the prefixes. When set to "filtered", only the prefixes in the given list will be allowed. When set to "all", all the prefixes configured on the router will be allowed. |


#### AllowedOutPrefixes





_Appears in:_
- [Advertise](#advertise)

| Field | Description |
| --- | --- |
| `prefixes` _string array_ |  |
| `mode` _[AllowMode](#allowmode)_ | Mode is the mode to use when handling the prefixes. When set to "filtered", only the prefixes in the given list will be allowed. When set to "all", all the prefixes configured on the router will be allowed. |


#### BFDProfile



BFDProfile is the configuration related to the BFD protocol associated to a BGP session.

_Appears in:_
- [BGPConfig](#bgpconfig)

| Field | Description |
| --- | --- |
| `name` _string_ | The name of the BFD Profile to be referenced in other parts of the configuration. |
| `receiveInterval` _integer_ | The minimum interval that this system is capable of receiving control packets in milliseconds. Defaults to 300ms. |
| `transmitInterval` _integer_ | The minimum transmission interval (less jitter) that this system wants to use to send BFD control packets in milliseconds. Defaults to 300ms |
| `detectMultiplier` _integer_ | Configures the detection multiplier to determine packet loss. The remote transmission interval will be multiplied by this value to determine the connection loss detection timer. |
| `echoInterval` _integer_ | Configures the minimal echo receive transmission interval that this system is capable of handling in milliseconds. Defaults to 50ms |
| `echoMode` _boolean_ | Enables or disables the echo transmission mode. This mode is disabled by default, and not supported on multi hops setups. |
| `passiveMode` _boolean_ | Mark session as passive: a passive session will not attempt to start the connection and will wait for control packets from peer before it begins replying. |
| `minimumTtl` _integer_ | For multi hop sessions only: configure the minimum expected TTL for an incoming BFD control packet. |


#### BGPConfig



BGPConfig is the configuration related to the BGP protocol.

_Appears in:_
- [FRRConfigurationSpec](#frrconfigurationspec)

| Field | Description |
| --- | --- |
| `routers` _[Router](#router) array_ | Routers is the list of routers we want FRR to configure (one per VRF). |
| `bfdProfiles` _[BFDProfile](#bfdprofile) array_ | BFDProfiles is the list of bfd profiles to be used when configuring the neighbors. |


#### CommunityPrefixes



CommunityPrefixes is a list of prefixes associated to a community.

_Appears in:_
- [Advertise](#advertise)

| Field | Description |
| --- | --- |
| `prefixes` _string array_ | Prefixes is the list of prefixes associated to the community. |
| `community` _string_ | Community is the community associated to the prefixes. |


#### FRRConfiguration



FRRConfiguration is a piece of FRR configuration.



| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `frrk8s.metallb.io/v1beta1`
| `kind` _string_ | `FRRConfiguration`
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `spec` _[FRRConfigurationSpec](#frrconfigurationspec)_ |  |
| `status` _[FRRConfigurationStatus](#frrconfigurationstatus)_ |  |


#### FRRConfigurationSpec



FRRConfigurationSpec defines the desired state of FRRConfiguration.

_Appears in:_
- [FRRConfiguration](#frrconfiguration)

| Field | Description |
| --- | --- |
| `bgp` _[BGPConfig](#bgpconfig)_ | BGP is the configuration related to the BGP protocol. |
| `raw` _[RawConfig](#rawconfig)_ | Raw is a snippet of raw frr configuration that gets appended to the one rendered translating the type safe API. |
| `nodeSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#labelselector-v1-meta)_ | NodeSelector limits the nodes that will attempt to apply this config. When specified, the configuration will be considered only on nodes whose labels match the specified selectors. When it is not specified all nodes will attempt to apply this config. |


#### FRRConfigurationStatus



FRRConfigurationStatus defines the observed state of FRRConfiguration.

_Appears in:_
- [FRRConfiguration](#frrconfiguration)



#### FRRNodeState



FRRNodeState exposes the status of the FRR instance running on each node.



| Field | Description |
| --- | --- |
| `apiVersion` _string_ | `frrk8s.metallb.io/v1beta1`
| `kind` _string_ | `FRRNodeState`
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |
| `spec` _[FRRNodeStateSpec](#frrnodestatespec)_ |  |
| `status` _[FRRNodeStateStatus](#frrnodestatestatus)_ |  |


#### FRRNodeStateSpec



FRRNodeStateSpec defines the desired state of FRRNodeState.

_Appears in:_
- [FRRNodeState](#frrnodestate)



#### FRRNodeStateStatus



FRRNodeStateStatus defines the observed state of FRRNodeState.

_Appears in:_
- [FRRNodeState](#frrnodestate)

| Field | Description |
| --- | --- |
| `runningConfig` _string_ | RunningConfig represents the current FRR running config, which is the configuration the FRR instance is currently running with. |
| `lastConversionResult` _string_ | LastConversionResult is the status of the last translation between the `FRRConfiguration`s resources and FRR's configuration, contains "success" or an error. |
| `lastReloadResult` _string_ | LastReloadResult represents the status of the last configuration update operation by FRR, contains "success" or an error. |


#### LocalPrefPrefixes



LocalPrefPrefixes is a list of prefixes associated to a local preference.

_Appears in:_
- [Advertise](#advertise)

| Field | Description |
| --- | --- |
| `prefixes` _string array_ | Prefixes is the list of prefixes associated to the local preference. |
| `localPref` _integer_ | LocalPref is the local preference associated to the prefixes. |


#### Neighbor



Neighbor represents a BGP Neighbor we want FRR to connect to.

_Appears in:_
- [Router](#router)

| Field | Description |
| --- | --- |
| `asn` _integer_ | ASN is the AS number to use for the local end of the session. |
| `sourceaddress` _string_ | SourceAddress is the IPv4 or IPv6 source address to use for the BGP session to this neighbour, may be specified as either an IP address directly or as an interface name |
| `address` _string_ | Address is the IP address to establish the session with. |
| `port` _integer_ | Port is the port to dial when establishing the session. Defaults to 179. |
| `password` _string_ | Password to be used for establishing the BGP session. Password and PasswordSecret are mutually exclusive. |
| `passwordSecret` _[SecretReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#secretreference-v1-core)_ | PasswordSecret is name of the authentication secret for the neighbor. the secret must be of type "kubernetes.io/basic-auth", and created in the same namespace as the frr-k8s daemon. The password is stored in the secret as the key "password". Password and PasswordSecret are mutually exclusive. |
| `holdTime` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#duration-v1-meta)_ | HoldTime is the requested BGP hold time, per RFC4271. Defaults to 180s. |
| `keepaliveTime` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#duration-v1-meta)_ | KeepaliveTime is the requested BGP keepalive time, per RFC4271. Defaults to 60s. |
| `connectTime` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v/#duration-v1-meta)_ | Requested BGP connect time, controls how long BGP waits between connection attempts to a neighbor. |
| `ebgpMultiHop` _boolean_ | EBGPMultiHop indicates if the BGPPeer is multi-hops away. |
| `bfdProfile` _string_ | BFDProfile is the name of the BFD Profile to be used for the BFD session associated to the BGP session. If not set, the BFD session won't be set up. |
| `toAdvertise` _[Advertise](#advertise)_ | ToAdvertise represents the list of prefixes to advertise to the given neighbor and the associated properties. |
| `toReceive` _[Receive](#receive)_ | ToReceive represents the list of prefixes to receive from the given neighbor. |
| `disableMP` _boolean_ | To set if we want to disable MP BGP that will separate IPv4 and IPv6 route exchanges into distinct BGP sessions. |


#### PrefixSelector



PrefixSelector is a filter of prefixes to receive.

_Appears in:_
- [AllowedInPrefixes](#allowedinprefixes)

| Field | Description |
| --- | --- |
| `prefix` _string_ |  |
| `le` _integer_ | The prefix length modifier. This selector accepts any matching prefix with length less or equal the given value. |
| `ge` _integer_ | The prefix length modifier. This selector accepts any matching prefix with length greater or equal the given value. |


#### RawConfig



RawConfig is a snippet of raw frr configuration that gets appended to the rendered configuration.

_Appears in:_
- [FRRConfigurationSpec](#frrconfigurationspec)

| Field | Description |
| --- | --- |
| `priority` _integer_ | Priority is the order with this configuration is appended to the bottom of the rendered configuration. A higher value means the raw config is appended later in the configuration file. |
| `rawConfig` _string_ | Config is a raw FRR configuration to be appended to the configuration rendered via the k8s api. |


#### Receive



Receive represents a list of prefixes to receive from the given neighbor.

_Appears in:_
- [Neighbor](#neighbor)

| Field | Description |
| --- | --- |
| `allowed` _[AllowedInPrefixes](#allowedinprefixes)_ | Allowed is the list of prefixes allowed to be received from this neighbor. |


#### Router



Router represent a neighbor router we want FRR to connect to.

_Appears in:_
- [BGPConfig](#bgpconfig)

| Field | Description |
| --- | --- |
| `asn` _integer_ | ASN is the AS number to use for the local end of the session. |
| `id` _string_ | ID is the BGP router ID |
| `vrf` _string_ | VRF is the host vrf used to establish sessions from this router. |
| `neighbors` _[Neighbor](#neighbor) array_ | Neighbors is the list of neighbors we want to establish BGP sessions with. |
| `prefixes` _string array_ | Prefixes is the list of prefixes we want to advertise from this router instance. |


