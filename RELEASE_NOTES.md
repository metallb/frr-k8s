# FRRK8s Release Notes

## Release v0.0.17

### New Features

Support unnumbered BGP peering (#212, @karampok)

## Release v0.0.16

### New Features

- Make the acceptance of incoming BGP connections optional via an helm parameter. (#229, @fedepaol)

### Bug fixes

- Bug: throw better error when prefix for same localPref is added twice (#221, @karampok)
- The BGP metrics label corresponding to the peer does not have the port anymore. Because now FRR-K8S allow incoming connections, the port might be source port of the tcp connection, making the metrics label not reliable. Additionally, we can only have one peer per VRF, so the port is not relevant. (#206, @fedepaol)

## Release v0.0.15

### New Features

- Add DynamicASN field for a neighbor, which allows the daemon to detect the AS number to use without explicitly setting it. The new field is mutually exclusive with the existing ASN field, and one of them must be specified for any given Neighbor. (#194, @oribon)

### Bug fixes

- Update the kubernetes api / codegen and get rid of the core symlink hack. (#186, @fedepaol)

## Release v0.0.14

### New Features

- Add graceful restart support (#162, @karampok)
- Allow FRR-K8s to accept incoming BGP connection so it can be used to establish sessions among the nodes (to announce pod IPs for example). (#171, @fedepaol)
- Implement the import vrf feature as described in [https://docs.frrouting.org/en/latest/bgp.html#clicmd-import-vrf-VRFNAME](https://docs.frrouting.org/en/latest/bgp.html#clicmd-import-vrf-VRFNAME). By implementing import VRF, we allow routes defined in the RIB of a router related to a given VRF to be imported from another router tied to another VRF. (#160, @fedepaol)

## Release v0.0.13

### Bug fixes

- Fix session flapping when BFD is enabled and the configuration is changed. (#169, @fedepaol)

### Other (Cleanup or Flake)

- Make the generated frr file compatible with frr 8+: split bfd echo interval in echo tx / echo rx. (#168, @fedepaol)+ end
- Export the client-go compatible generated types

## Release v0.0.12

### New features

- Expose the IPv4 or IPv6 source address to use for the BGP session through the k8s API (#137, @karampok)
- Make the type safe code-generator generated types available. (#167, @fedepaol)

### Bug fixes

- Make the all-in-one manifests from the main branch work by setting the image tag as "main", which is the tag under the builds from main are published on quay. (#155, @fedepaol)


## Release v0.0.11

### New Features

- Add a field to the FRRConfiguration CRD to disable MP BGP for the given peer (#128, @AlinaSecret)

### Bug fixes

- Fix the case where merging an FRRConfiguration with no hold / keepalive / connect Time set with one where the time is set to default fails. (#120, @fedepaol)

## Release v0.0.10

### New features

- FRR: bump to 9.0.2, allow to peer with localhost ([PR 118](https://github.com/metallb/frr-k8s/pull/118))
- Add option to configure BGP connect time ([PR_119](https://github.com/metallb/frr-k8s/pull/119))

## Release v0.0.9

### Bug fixes
 - helm: namespace all namespaced resources ([PR 117](https://github.com/metallb/frr-k8s/pull/117))


## Release v0.0.8

### Features

- Support a --always-block parameter. The parameter accepts a list of comma separated cidrs to always block. This is useful to protect well known cidrs such as pods or clusterIPs. ([PR 88](https://github.com/metallb/frr-k8s/pull/88))
- Support restarting the webhook pod when the rotator updates its cert secret.([PR 100](https://github.com/metallb/frr-k8s/pull/100))
- Add a demo environment creation script ([PR 107](https://github.com/metallb/frr-k8s/pull/107))

- Remove the DesiredConfig field from the status API ([PR 110](https://github.com/metallb/frr-k8s/pull/110))

### Bug fixes

- FRRConfigurations from namespaces different than the one the daemon is deployed on were not validated with other resoureces. ([PR 91](https://github.com/metallb/frr-k8s/pull/91))

- Empty always-block flag was not parsed correctly. ([PR 95](https://github.com/metallb/frr-k8s/pull/95))

- helm: webhooks probes pointed to the wrong endpoints. ([PR 97](https://github.com/metallb/frr-k8s/pull/97))

### Chores

- helm: add an option to disable the webhook's cert rotation. ([PR 93](https://github.com/metallb/frr-k8s/pull/93))
- add a new logo!
- CI: add a MetalLB E2E lane. ([PR 99](https://github.com/metallb/frr-k8s/pull/99))
- CI: don't run auto-generated files checks on dependabot PRs ([PR 111](https://github.com/metallb/frr-k8s/pull/111))
- kubectl: don't download if cluster is not reacheable ([PR 112](https://github.com/metallb/frr-k8s/pull/112))

## Release v0.0.4

### Bug fixes

- Merging neighbors always failed when holdtime and keepalivetime were set ([PR #86](https://github.com/metallb/frr-k8s/pull/86)).

### Chores

- Enforce adding release notes in CI ([PR #79](https://github.com/metallb/frr-k8s/pull/79))
- helm: Fix metricRelabelings templating ([PR #83](https://github.com/metallb/frr-k8s/pull/83))

## Release v0.0.3

Support establishing BGP sessions with cleartext passwords ([PR #80](https://github.com/metallb/frr-k8s/pull/80)).

## Release v0.0.2

Helm charts: flip the prometheus service monitor default to false. Service Monitor should be opt-in for those users that have
the prometheus operator deployed.

## Release v0.0.1

First release!
