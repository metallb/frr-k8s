# Redistribution Support for FRRConfiguration

## Summary

This proposal adds route redistribution to the FRRConfiguration CRD.
Users can advertise routes from a kernel routing table.
The reachability decision stays in the kernel table.
The CRD only bounds which prefixes may leave.

## Motivation

Some agents signal per-node state through kernel routes.
Example: a health checker (kubevip) installs a /32 into table 198 while a local backend is healthy.
FRR should advertise that route only while it exists.
This gives automatic per-node withdrawal and ECMP across healthy nodes.

Today the CRD cannot express this:

1. `router.prefixes` renders unconditional `network` statements. This bypasses the gating.
2. `toAdvertise.allowed.mode: all` only allows prefixes declared in `router.prefixes`. Redistributed routes are always filtered out on egress.

The only workaround is `rawConfig`. It must append permits to the generated `<neighbor>-out` route-maps.
That couples user config to internal naming and sequence numbers. It can silently break on frr-k8s upgrade.

OpenShift's BGP-based VIP management plans to use this pattern in production and carries the fragile rawConfig today in the POC.

### Goals

- Advertise routes redistributed from a kernel table (`table-direct`).
- Filter egress to an explicit prefix allow-list.
- Compose with the generated per-neighbor route-maps. No raw config.

### Non-Goals

- Redistributing other protocols (connected, static, kernel, OSPF). The API leaves room for them.
- Import policy or route modification (communities, med) for redistributed routes.
- Managing the kernel table content. That is the user's agent's job.
- VRF routers. Deferred until FRR's per-VRF `table-direct` behavior is verified and we actually need that.

## Proposal

### User Stories

As a cluster administrator, I want to:

1. Advertise a VIP only while my health-check agent keeps its route in a kernel table.
2. Bound the advertisement to an explicit prefix list so nothing else can leak.
3. Upgrade frr-k8s without my egress filters breaking.

### API Changes

Add a `redistribute` list to `Router`:

```yaml
apiVersion: frrk8s.metallb.io/v1beta1
kind: FRRConfiguration
metadata:
  name: vip-advertisement
spec:
  bgp:
    routers:
    - asn: 64512
      neighbors:
      - address: 192.168.1.1
        asn: 64513
        toAdvertise:
          allowed:
            mode: all
      redistribute:
      - protocol: table-direct
        table: 198
        allowed:
          mode: filtered
          prefixes:
          - prefix: 192.168.111.4/32
          - prefix: 192.168.111.5/32
```

Fields:

- `protocol`: only `table-direct` initially. Enum, extensible.
- `table`: kernel table id. Required for `table-direct`.
- `allowed`: the filter deciding which routes from the table enter BGP.
  Same shape as `toReceive.allowed`, reusing the existing `PrefixSelector` type:
  - `mode`: `filtered` (default) or `all`. With `all`, every route in the table
    enters BGP; `prefixes` must be empty.
  - `prefixes`: `PrefixSelector` entries (`prefix` with optional `le`/`ge`).
    Matching is exact unless a length modifier is given: `192.168.111.0/24`
    matches only the /24 itself; `192.168.111.0/24 le 32` matches every
    contained route, identical to `toReceive` semantics.

Defaults keep the filter fail-closed: `allowed` omitted, or `mode: filtered` with
no `prefixes`, renders a filter that lets nothing through. Advertising an entire
table is always an explicit `mode: all`.

The same selector shape for `toReceive` and `redistribute` gives one mental model
for every filter in the CRD, and a flat prefix list would have no room to grow.

### Dual-stack

`allowed.prefixes` may mix IPv4 and IPv6. The renderer splits them by family.
Each family gets its own route-map, prefix-list and `address-family` block.
With `mode: filtered`, a family with no prefixes renders nothing; with `mode: all`,
both families render. No validation against neighbor families is needed.
`table-direct` supports both families in FRR.

### Generated FRR Configuration

Names are scoped by VRF and family: `redistribute-<vrf>-<table>-<family>`.
An empty `vrf` field maps to the literal `default` (never an empty name segment). Initially always `default`.
The VRF placeholder future-proofs the naming for VRF support.

```
router bgp 64512
 address-family ipv4 unicast
  redistribute table-direct 198 route-map redistribute-default-198-ipv4
route-map redistribute-default-198-ipv4 permit 1
 match ip address prefix-list redistribute-default-198-allowed-ipv4
route-map redistribute-default-198-ipv4 deny 2
ip prefix-list redistribute-default-198-allowed-ipv4 seq 1 permit 192.168.111.4/32
ip prefix-list redistribute-default-198-allowed-ipv4 seq 2 permit 192.168.111.5/32
```

Length modifiers render as prefix-list modifiers: `{prefix: 10.0.0.0/8, le: 32}`
becomes `permit 10.0.0.0/8 le 32`. `mode: all` renders the permit clause without
a `match` (everything in the table passes); the explicit `deny 2` stays in every
variant so config merged later cannot widen the filter. An empty filter renders
only the deny clause.

IPv6 prefixes render the same under `address-family ipv6 unicast`, with `ipv6 prefix-list` and `-ipv6` names.

#### Two filter layers

The redistribute filter and `toAdvertise` are independent layers and both apply:

1. `redistribute[].allowed` decides what enters BGP **from the table**.
2. `neighbor.toAdvertise` decides what leaves **toward that neighbor**.

For neighbors with `toAdvertise.allowed.mode: all`, the redistribute selectors are
appended to the neighbor's generated allowed prefix-lists
(`ToAdvertisePrefixListV4`/`V6`), modifiers included. For `mode: all`
redistribution the appended entry is the family-wide selector
(`0.0.0.0/0 le 32`, `::/0 le 128`). No extra route-map clauses.
When the neighbor has no declared prefixes, the appended entries must replace the
`deny any` placeholder entry, not follow it. Prefix-lists are first-match.
Neighbor modifiers like `set ip next-hop` live in the main permit rule and apply uniformly.
Neighbors with explicit `allowed.prefixes` are untouched. They advertise a redistributed route only if it also matches their own allow-list.
`toAdvertise` semantics for declared prefixes stay unchanged.

```
# neighbor 192.168.1.1 with toAdvertise.allowed.mode: all, redistribute {table 198, filtered, 192.168.111.0/24 le 32}
ip prefix-list 192.168.1.1-pl-ipv4 seq 1 permit 192.168.111.0/24 le 32
```

`table-direct` reads the kernel table directly. No `ip import-table` is needed.

### Validation

- Reject `table` outside 1-65535. This mirrors FRR's `redistribute table-direct (1-65535)`.
- Reject `mode: all` combined with a non-empty `prefixes`.
- `prefixes` entries follow the `toReceive` rules: valid CIDR, `le`/`ge` not
  shorter than the prefix length, `ge <= le` when both are set.
- Reject duplicate `(protocol, table)` pairs within one router. Future table-less protocols are not affected.
- Reject `redistribute` on VRF routers.
- Merge across FRRConfigurations, per router and table: the union of the
  selectors. If any producer declares `mode: all`, the merged filter is `all` -
  one producer widens advertisement for the whole table; this mirrors how
  permit unions already behave for `toReceive` and must be called out in the
  field documentation. Fail the merge if the same table is declared with
  different protocols.
- The webhook's outgoing-prefix check (`validateOutgoingPrefixes`) must
  accept redistributed routes: the redistribute selectors join the router's
  known prefixes (`mode: all` joins the family-wide selector). Otherwise a
  `filtered` neighbor listing a redistributed prefix is falsely rejected.

## Alternatives Considered

- **Keep rawConfig.** Fragile and defeats the purpose.
- **CRD-declared prefixes (`network` statements).** Unconditional. Defeats health gating.
- **A flat `allowedPrefixes` string list.** The first revision of this design.
  Exact-match only, no room for length modifiers or a whole-table mode, and a
  second filter vocabulary next to `toReceive.allowed`. Rejected in review in
  favor of the shared selector shape.

## Test Plan

- Unit: api_to_config coverage for the new stanza.
- Unit: neighbor modifiers (e.g. `set ip next-hop`) apply to redistributed prefixes.
- Unit: `mode: all` with non-empty `prefixes` is rejected; omitted `allowed` renders a deny-only filter; merge with one `mode: all` producer yields `all`.
- E2E: install route in table, expect advertisement; remove route, expect withdrawal; verify a non-allowed prefix in the table never leaves.
- E2E: an `le` selector advertises a covered /32 and not an uncovered one.
- E2E: `mode: all` advertises an arbitrary table route while a `filtered` neighbor's `toAdvertise` still drops it.
- E2E: dual-stack variant (mixed v4/v6 selectors).
