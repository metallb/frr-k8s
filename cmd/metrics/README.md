# FRR metrics

Existing BGP/BFD series, authentication and HTTPS behavior are preserved. The
same collectors run in Kubernetes and on external hosts.

| New series | Labels | Meaning |
| --- | --- | --- |
| `frrk8s_bgp_family_announced_prefixes`, `frrk8s_bgp_family_received_prefixes` | `peer,vrf,afi,safi` | FRR's per-family advertised/accepted prefix counts |
| `frrk8s_evpn_vni_info` | `vni,vrf,type,interface` | Observed L2/L3 VNI inventory, value 1 |
| `frrk8s_evpn_macs`, `frrk8s_evpn_neighbors`, `frrk8s_evpn_remote_vteps` | `vni,vrf` | L2 MAC, ARP/ND and known remote-VTEP counts |
| `frrk8s_evpn_scrape_success` | none | 1 for a complete VNI read/parse, 0 on failure |

Family gauges distinguish `ipv4/unicast`, `ipv6/unicast` and `l2vpn/evpn`.
Missing counts are omitted; explicit zeros are exported. Existing prefix totals
still sum all families, including those without a new family gauge.
Message counters remain per BGP session.

The VNI collector adds one `show evpn vni json` read per scrape. An empty inventory
is valid and emits only success=1. A failed read/parse emits success=0 and no VNI
measurements. Missing measurements are omitted; L3 VNIs have inventory only.
Inventory and known VTEPs do not establish forwarding health. MAC/ARP-ND counts
measure occupancy, not learning-event rates.

Parser tests are based on a [FRR 10.4.3 capture](https://github.com/dfarrell07/frr-k8s/blob/87f63e17f5d234c03f1b9ddd721f0960b50d1f37/cmd/metrics/collector/testdata/evpn_vni.json).
Existing generated-configuration EVPN tests exercise the authenticated endpoint.
