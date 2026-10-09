# Redistribution Support for FRRConfiguration

## Summary

This proposal adds route redistribution to the FRRConfiguration CRD.
Users can advertise routes from a kernel routing table.
The reachability decision stays in the kernel table.
The CRD decides which routes from the table enter BGP, and which neighbors they are advertised to.
Routes learned from other neighbors are never re-advertised as a side effect.

## Motivation

Some agents signal per-node state through kernel routes.
Example: a health checker (kubevip) installs a /32 into table 198 while a local backend is healthy.
FRR should advertise that route only while it exists.
This gives automatic per-node withdrawal and ECMP across healthy nodes.

Today the CRD cannot express this:

1. `router.prefixes` renders unconditional `network` statements. This bypasses the gating.
2. `toAdvertise.allowed.mode: all` only allows prefixes declared in `router.prefixes` (and imported VRF prefixes). Redistributed routes are always filtered out on egress.

The only workaround is `rawConfig`. It must append permits to the generated `<neighbor>-out` route-maps.
That couples user config to internal naming and sequence numbers. It can silently break on frr-k8s upgrade.

OpenShift's BGP-based VIP management plans to use this pattern in production and carries the fragile rawConfig today in the POC.

### Goals

- Advertise routes redistributed from a kernel table (`table-direct`).
- Bound which table routes enter BGP with the same selector shape `toReceive` uses, or opt in to the whole table explicitly.
- Let each neighbor be opted in to the redistributed routes advertised to it.
- Preserve the existing guarantee that routes received from one neighbor are never re-advertised to another.
- Compose with the generated per-neighbor route-maps. No raw config.

### Non-Goals

- Redistributing other protocols (connected, static, kernel, OSPF). The API leaves room for them.
- Import policy or route modification (communities, med) for redistributed routes.
- Managing the kernel table content. That is the user's agent's job.
- VRF routers. Deferred until FRR's per-VRF `table-direct` behavior is verified and a concrete need exists.

## Proposal

### User Stories

As a cluster administrator, I want to:

1. Advertise a VIP only while my health-check agent keeps its route in a kernel table.
2. Bound what enters BGP from that table to an explicit selector list so nothing else can leak.
3. Choose which neighbors the redistributed routes are advertised to.
4. Upgrade frr-k8s without my egress filters breaking.

### API Changes

Two additions: a `redistribute` list on `Router`, and a `redistributed` opt-in under `Neighbor.toAdvertise`.

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
            mode: filtered
          redistributed:
            tables: [198]
      redistribute:
      - protocol: table-direct
        table: 198
        allowed:
          mode: filtered
          prefixes:
          - prefix: 192.168.111.4/32
          - prefix: 192.168.111.5/32
```

#### `router.redistribute[]`

- `protocol`: only `table-direct` initially. Enum, extensible.
- `table`: kernel table id. Required for `table-direct`, forbidden for future protocols
  without a table (CEL: `protocol == 'table-direct'` if and only if `has(table)`).
- `allowed`: the table filter deciding which routes from the table enter BGP.
  Same shape as `toReceive.allowed`, reusing the existing `PrefixSelector` type:
  - `mode`: `filtered` (default) or `all`. With `all`, every route in the table
    enters BGP; `prefixes` must be empty.
  - `prefixes`: `PrefixSelector` entries (`prefix` with optional `le`/`ge`).
    Matching is exact unless a length modifier is given: `192.168.111.0/24`
    matches only the /24 itself; `192.168.111.0/24 le 32` matches every
    contained route, identical to `toReceive` semantics.

`allowed.prefixes` may mix IPv4 and IPv6; `mode: all` covers both families.

Defaults keep the filter fail-closed: `allowed` omitted, or `mode: filtered` with
no `prefixes`, lets nothing through. Advertising an entire table is always an
explicit `mode: all`. This matches the `filtered` default of `toReceive.allowed`
and ensures a typo or an empty list can never advertise a whole table.

#### `neighbor.toAdvertise.redistributed`

- `tables`: the kernel table ids whose redistributed routes are advertised to
  this neighbor. Each id must match a `redistribute` entry of the router (after
  merging, so the table and the neighbor may come from different
  FRRConfigurations). Omitted or empty: no redistributed routes are advertised
  to the neighbor.

Redistribution is opt-in per neighbor and independent of `toAdvertise.allowed`:
a `filtered` neighbor is sent its declared prefixes plus the tables it is opted
in to, without listing the redistributed prefixes. The opt-in is an object so
per-neighbor knobs can be added later without renaming the field.

### Merge semantics

FRRConfigurations sharing a router merge as follows:

- `redistribute` entries merge per `(router, protocol, table)`: the union of the
  selectors. If any producer declares `mode: all`, the merged filter is `all` -
  one producer widens what enters BGP from the whole table. This mirrors how
  permit unions already behave for `toReceive.allowed` and must be stated in the
  field documentation. Declaring the same table with different protocols fails
  the merge (vacuous while the enum has one value; kept so the rule exists when
  a second protocol lands).
- `toAdvertise.redistributed.tables` merges per neighbor as the union of the ids.
- Neighbor references are validated against the merged router, so the table
  filter and the opting-in neighbor may be owned by different FRRConfigurations.
  A table declared by producer B is advertised to producer A's neighbor only if
  A (or B, for the same neighbor) opted that neighbor in; nothing widens egress
  implicitly.

### Validation

- Reject `table` outside 1-65535 (FRR's `redistribute table-direct (1-65535)`),
  and reject 253, 254 and 255: the kernel's default, main and local tables, which
  would redistribute connected, kernel and default routes.
- Reject `mode: all` combined with a non-empty `prefixes`. This is stricter than
  `toReceive`, which silently ignores `prefixes` in `all` mode, so an accidental
  `all` is not masked by a populated list.
- `prefixes` entries follow the `toReceive` rules (valid CIDR, `le`/`ge` not
  shorter than the prefix length, `ge <= le`), plus the family maximum the
  `toReceive` path lacks: `le`/`ge` at most 32 for IPv4 and 128 for IPv6, and no
  IPv4-mapped IPv6 prefixes. A selector FRR rejects would fail the whole reload
  and take down every advertisement on the node.
- Reject duplicate `(protocol, table)` pairs within one router.
- Reject overlapping table filters within one router, checked per family on the
  merged router. BGP selects a single best path per prefix before per-neighbor
  policy runs, so the same prefix admissible from two tables could not be
  advertised per table; the design rejects the overlap rather than promise it.
  Two `redistribute` entries overlap when a prefix could be admitted by both. A selector is a CIDR plus a length window
  (`[ge, le]`, defaulting to the prefix length); two selectors overlap when one
  CIDR contains the other's network address and the windows intersect.
  `mode: all` is the family-wide selector, so two `mode: all` entries of one
  family always overlap; entries of different families never do. Exact prefixes
  are the degenerate window case, so one predicate covers every combination.
  The check inspects selector space, not kernel-table content: an agent may
  still install one route in two tables, but non-overlapping filters admit it
  from at most one of them, so advertisement stays correct.
- Reject `redistribute` on VRF routers.
- Reject `toAdvertise.redistributed.tables` entries that do not match a
  `redistribute` entry of the merged router, and duplicate ids.
- `PrefixSelector`'s documentation ("a filter of prefixes to receive") and the
  generated API docs are updated to describe it as a generic prefix selector.

### Security considerations

The table filter is the only bound between node-local route writers and external
peers. Any process on the node with `CAP_NET_ADMIN` (a privileged hostNetwork pod,
a compromised health-check agent, a debug shell) can install routes into the
redistributed table. With `mode: filtered` the CRD selectors decide what such a
process can make the node announce; with `mode: all` that decision moves to the
node, and the FRRConfiguration author is trusting every `CAP_NET_ADMIN` process on
it. `mode: filtered` with tight selectors is the recommended production setting.

The per-neighbor opt-in means table routes are advertised to a neighbor only if
a producer opted it in. Routes received from other neighbors, imported, or
declared are never advertised as redistributed routes, and a table route that
shares a prefix with a declared one cannot bypass the opt-in. Peers'
`maximum-prefix` remains the only bound on how many routes a `mode: all` table
can inject.

## Alternatives Considered

- **Keep rawConfig.** Fragile and defeats the purpose.
- **CRD-declared prefixes (`network` statements).** Unconditional. Defeats health gating.
- **A flat `allowedPrefixes` string list.** The first revision of this design.
  Exact-match only, no room for length modifiers or a whole-table mode, and a
  second filter vocabulary next to `toReceive.allowed`. Rejected in review in
  favor of the shared selector shape.
- **Appending the table selectors to the neighbor's allowed prefix-list.** The
  second revision. The neighbor prefix-list is source-blind, so a range or a
  whole-table permit there also matches routes received from other neighbors:
  the node becomes a transit router without anyone declaring it. Rejected for
  origin-scoped egress.
- **A boolean opt-in (`redistributed: true`).** All-or-nothing across tables;
  adding per-table selection later would be a breaking change. The `tables`
  list costs nothing more today.

## Test Plan

Unit:

- Validation rejects: table 0, 65536, 253-255; `mode: all` with prefixes;
  `ge > le`, `le` shorter than the mask, `le 128` on IPv4, IPv4-mapped IPv6;
  duplicate `(protocol, table)`; VRF router; `redistributed.tables` referencing
  an undeclared table or listing an id twice.
- Overlap rejects: the same exact prefix in two tables; nested CIDRs with
  intersecting windows (`10.0.0.0/8 le 32` vs `10.1.0.0/16`); two `mode: all`
  entries of one family. Overlap accepts (must not be over-rejected): nested
  CIDRs with disjoint windows (`10.0.0.0/8 le 16` vs `10.1.0.0/16 ge 24`);
  disjoint CIDRs; the same prefix in an IPv4 and an IPv6 table.
- Merge: same table with disjoint selectors (union, deduplicated); `filtered` +
  `all` in either order (`all`); different tables kept; neighbor `tables` union;
  table declared in one FRRConfiguration and referenced by a neighbor in another
  (accepted); the same table in two FRRConfigurations merges rather than tripping
  the single-router duplicate rule; overlapping filters split across two
  FRRConfigurations are rejected at merge time with both sources named.

E2E:

- Install a route in the table on every node, expect advertisement; remove it on
  one node, expect that node's next-hop to disappear while the others remain
  (the ECMP claim); remove everywhere, expect withdrawal within a stated timeout.
- A non-allowed prefix in the table, and an allowed prefix in a different table,
  are `Consistently` absent at the peer.
- **No transit**: peer A advertises X; with `mode: all` redistribution and peer B
  opted in, B must not receive X.
- `mode: all` advertises an arbitrary table route to an opted-in neighbor and not
  to a neighbor which did not opt in.
- **No bypass through declared prefixes**: declare prefix P toward neighbor N
  without opting N into the table, install P in the table; N must
  `Consistently` not advertise it. Opt N in; P is advertised.
- Dual-stack: one route per family; a v4-only peer sees only the v4 route, a
  v6-only peer only the v6 route, a dual-stack peer both.
