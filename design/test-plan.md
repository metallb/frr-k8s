# FRR-K8s testplan

## Summary

This document lists the set of tests that will be implemented.

This list is not meant to be comprehensive, but to serve as a starting point. Whenever a feature of the
daemon will be implemented (i.e. status exposure, filtering well known CIDRs), the feature is expected
to come with a good amount of coverage. What tests to implement will be discussed on the PR.

The baseline infrastructure for testing is the same set of external FRR containers we have in MetalLB, which
are defined for both the default VRF and an extra VRF (vrf red):

- ibgp single hop
- ebgp single hop
- ibgp multi hop
- ebgp multi hop

### Baseline tests

- Each FRR instance connects to all the external containers
- The main router is configured with multiple prefixes, we advertise them to all the neighbours via the "all"
flag
- The main router is configured with multiple prefixes, we advertise only a subset of them explicitly
- The main router is configured with multiple prefixes, we differentiate what we advertise to different neighbors
- Prefixes with communities
- Prefixes with local preferences
- Receiving all the routes
- Receiving only some routes

- Editing the configuration works
- Deleting a given configuration works

### Configuration composition

- One configuration related to one neighbor and one to another (session, advertisement)
- One configuration for the router prefixes, and the other for advertising the prefixes (all)
- One configuration for the router prefixes, and the other for advertising the prefixes (some)
- One configuration where we override the announcement list with all
- One configuration where we override the receiving list with all

### BFD

- The BFD session is established
- The session parameters are propagated

### Node selector

- We apply the configuration to a subset of the nodes
- A general configuration (to advertise), another configuration applied to a subset of the nodes

### Negative tests

Invalid configurations that should be caught by the webhooks:

- Prefixes in the neighbors but not in the router
- prefixes to be associated with local pref but not in the list of advertised ones
- prefixes to be associated with community but not in the list of advertised ones

### Metrics

- The tests must cover (at least) the same set of metrics that metallb is exposing and testing
- In case metrics for the incoming announcements are added, they must be covered as well

### Status

Every time a bit of the status exposed is implemented, it must be covered with tests.
