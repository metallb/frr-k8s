# FRR metrics

Per-family BGP prefix and scrape-status series:

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `frrk8s_bgp_family_announced_prefixes` | gauge | `peer,vrf,afi,safi` | Advertised prefixes per address family |
| `frrk8s_bgp_family_received_prefixes` | gauge | `peer,vrf,afi,safi` | Accepted prefixes per address family |
| `frrk8s_scrape_success` | gauge | `collector` (`bgp`, `bfd`) | 1 if all collector reads and parses succeed, 0 otherwise |

`frrk8s_bgp_announced_prefixes_total` and `frrk8s_bgp_received_prefixes_total`
sum all address families FRR reports; the family gauges cover only `ipv4/unicast`,
`ipv6/unicast` and `l2vpn/evpn`, so other families (for example `ipv4Vpn`) count toward
the totals only.
Missing or null per-family counts are omitted; explicit zeros are exported.
Gauges; don't `rate()` them.

`frrk8s_scrape_success` is 1 when every read and parse succeeded, including an empty
inventory. On 0, the BGP collector omits its other series, while the BFD
collector still exports session state when only its counter read failed.
