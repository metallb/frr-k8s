# FRR metrics

Per-family BGP prefix, EVPN VNI and scrape-status series:

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `frrk8s_bgp_family_announced_prefixes` | gauge | `peer,vrf,afi,safi` | Advertised prefixes per address family |
| `frrk8s_bgp_family_received_prefixes` | gauge | `peer,vrf,afi,safi` | Accepted prefixes per address family |
| `frrk8s_evpn_vni_info` | gauge | `vni,vrf,type,interface` | EVPN VNI inventory; constant 1 |
| `frrk8s_evpn_l2_macs` | gauge | `vni,vrf` | Host MAC addresses zebra holds for the L2 VNI |
| `frrk8s_evpn_l2_neighbors` | gauge | `vni,vrf` | ARP/ND entries zebra holds for the L2 VNI |
| `frrk8s_evpn_l2_remote_vteps` | gauge | `vni,vrf` | Remote VTEPs known for the L2 VNI |
| `frrk8s_evpn_l3_router_macs` | gauge | `vni,vrf` | Router MACs zebra holds for the L3 VNI |
| `frrk8s_evpn_l3_nexthops` | gauge | `vni,vrf` | Next hops zebra holds for the L3 VNI |
| `frrk8s_scrape_success` | gauge | `collector` (`bgp`, `bfd`, `evpn`) | 1 if all collector reads and parses succeed, 0 otherwise |

`frrk8s_bgp_announced_prefixes_total` and `frrk8s_bgp_received_prefixes_total`
sum all address families FRR reports; the family gauges cover only `ipv4/unicast`,
`ipv6/unicast` and `l2vpn/evpn`, so other families (for example `ipv4Vpn`) count toward
the totals only.
Missing or null per-family and EVPN counts are omitted; explicit zeros are exported.
Gauges; don't `rate()` them.

For the BGP series, `vrf` is the VRF of the BGP session. For the EVPN series it is the
VNI's tenant VRF (FRR's `tenantVrf`), which can differ from the VRF of the session that
carries the EVPN routes; `interface` is the VNI's VXLAN device.

`frrk8s_scrape_success` is 1 when every read and parse succeeded, including an empty
inventory. On 0, the BGP and EVPN collectors omit their other series, while the BFD
collector still exports session state when only its counter read failed.

Remote VTEPs are L2-only because FRR reports `"n/a"` for L3.
