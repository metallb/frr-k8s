# FRR metrics

Per-family BGP prefix series:

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `frrk8s_bgp_family_announced_prefixes` | gauge | `peer,vrf,afi,safi` | Advertised prefixes per address family |
| `frrk8s_bgp_family_received_prefixes` | gauge | `peer,vrf,afi,safi` | Accepted prefixes per address family |

`frrk8s_bgp_announced_prefixes_total` and `frrk8s_bgp_received_prefixes_total`
sum all address families FRR reports; the family gauges cover only `ipv4/unicast`,
`ipv6/unicast` and `l2vpn/evpn`, so other families (for example `ipv4Vpn`) count toward
the totals only.
Missing or null per-family counts are omitted; explicit zeros are exported.
Gauges; don't `rate()` them.
