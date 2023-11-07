// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"fmt"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func splitByNeigh(cfg frrk8sv1beta1.FRRConfiguration) ([]frrk8sv1beta1.FRRConfiguration, error) {
	if len(cfg.Spec.BGP.Routers) != 1 {
		return nil, fmt.Errorf("expected a config with a single router, got %v", cfg)
	}

	router := cfg.Spec.BGP.Routers[0]
	configs := []frrk8sv1beta1.FRRConfiguration{}

	for i, n := range router.Neighbors {
		configs = append(configs, frrk8sv1beta1.FRRConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", cfg.Name, i),
				Namespace: cfg.Namespace,
			},
			Spec: frrk8sv1beta1.FRRConfigurationSpec{
				BGP: frrk8sv1beta1.BGPConfig{
					Routers: []frrk8sv1beta1.Router{
						{
							ASN:       router.ASN,
							VRF:       router.VRF,
							Neighbors: []frrk8sv1beta1.Neighbor{n},
							Prefixes:  router.Prefixes,
						},
					},
				},
			},
		})
	}

	return configs, nil
}

func splitByCommunities(cfg frrk8sv1beta1.FRRConfiguration) ([]frrk8sv1beta1.FRRConfiguration, error) {
	if len(cfg.Spec.BGP.Routers) != 1 {
		return nil, fmt.Errorf("expected a config with a single router, got %v", cfg)
	}

	withCommunityPrefixFor := func(n frrk8sv1beta1.Neighbor, j int) frrk8sv1beta1.Neighbor {
		res := n.DeepCopy()
		res.ToAdvertise.PrefixesWithCommunity = []frrk8sv1beta1.CommunityPrefixes{res.ToAdvertise.PrefixesWithCommunity[j]}
		return *res
	}
	router := cfg.Spec.BGP.Routers[0]
	configs := []frrk8sv1beta1.FRRConfiguration{}

	for i, n := range router.Neighbors {
		for j := range n.ToAdvertise.PrefixesWithCommunity {
			configs = append(configs, frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-%d-%d", cfg.Name, i, j),
					Namespace: cfg.Namespace,
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{
					BGP: frrk8sv1beta1.BGPConfig{
						Routers: []frrk8sv1beta1.Router{
							{
								ASN:       router.ASN,
								VRF:       router.VRF,
								Neighbors: []frrk8sv1beta1.Neighbor{withCommunityPrefixFor(n, j)},
								Prefixes:  router.Prefixes,
							},
						},
					},
				},
			})
		}
	}

	return configs, nil
}

func splitByLocalPref(cfg frrk8sv1beta1.FRRConfiguration) ([]frrk8sv1beta1.FRRConfiguration, error) {
	if len(cfg.Spec.BGP.Routers) != 1 {
		return nil, fmt.Errorf("expected a config with a single router, got %v", cfg)
	}

	withLocalPrefPrefixFor := func(n frrk8sv1beta1.Neighbor, j int) frrk8sv1beta1.Neighbor {
		res := n.DeepCopy()
		res.ToAdvertise.PrefixesWithLocalPref = []frrk8sv1beta1.LocalPrefPrefixes{res.ToAdvertise.PrefixesWithLocalPref[j]}
		return *res
	}
	router := cfg.Spec.BGP.Routers[0]
	configs := []frrk8sv1beta1.FRRConfiguration{}

	for i, n := range router.Neighbors {
		for j := range n.ToAdvertise.PrefixesWithLocalPref {
			configs = append(configs, frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-%d-%d", cfg.Name, i, j),
					Namespace: cfg.Namespace,
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{
					BGP: frrk8sv1beta1.BGPConfig{
						Routers: []frrk8sv1beta1.Router{
							{
								ASN:       router.ASN,
								VRF:       router.VRF,
								Neighbors: []frrk8sv1beta1.Neighbor{withLocalPrefPrefixFor(n, j)},
								Prefixes:  router.Prefixes,
							},
						},
					},
				},
			})
		}
	}

	return configs, nil
}

func splitByLocalPrefAndCommunities(cfg frrk8sv1beta1.FRRConfiguration) ([]frrk8sv1beta1.FRRConfiguration, error) {
	if len(cfg.Spec.BGP.Routers) != 1 {
		return nil, fmt.Errorf("expected a config with a single router, got %v", cfg)
	}

	withoutCommunities := func(n frrk8sv1beta1.Neighbor) frrk8sv1beta1.Neighbor {
		res := n.DeepCopy()
		res.ToAdvertise.PrefixesWithCommunity = nil
		return *res
	}

	withoutLocalPrefs := func(n frrk8sv1beta1.Neighbor) frrk8sv1beta1.Neighbor {
		res := n.DeepCopy()
		res.ToAdvertise.PrefixesWithLocalPref = nil
		return *res
	}

	router := cfg.Spec.BGP.Routers[0]
	configs := []frrk8sv1beta1.FRRConfiguration{}

	for i, n := range router.Neighbors {
		configs = append(configs,
			frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-%d-without-communities", cfg.Name, i),
					Namespace: cfg.Namespace,
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{
					BGP: frrk8sv1beta1.BGPConfig{
						Routers: []frrk8sv1beta1.Router{
							{
								ASN:       router.ASN,
								VRF:       router.VRF,
								Neighbors: []frrk8sv1beta1.Neighbor{withoutCommunities(n)},
								Prefixes:  router.Prefixes,
							},
						},
					},
				},
			},
			frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-%d-without-localprefs", cfg.Name, i),
					Namespace: cfg.Namespace,
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{
					BGP: frrk8sv1beta1.BGPConfig{
						Routers: []frrk8sv1beta1.Router{
							{
								ASN:       router.ASN,
								VRF:       router.VRF,
								Neighbors: []frrk8sv1beta1.Neighbor{withoutLocalPrefs(n)},
								Prefixes:  router.Prefixes,
							},
						},
					},
				},
			},
		)
	}

	return configs, nil
}

func duplicateNeighsWithReceiveAll(cfg frrk8sv1beta1.FRRConfiguration) ([]frrk8sv1beta1.FRRConfiguration, error) {
	if len(cfg.Spec.BGP.Routers) != 1 {
		return nil, fmt.Errorf("expected a config with a single router, got %v", cfg)
	}

	router := cfg.Spec.BGP.Routers[0]
	configs := []frrk8sv1beta1.FRRConfiguration{}

	withReceiveAll := func(n frrk8sv1beta1.Neighbor) frrk8sv1beta1.Neighbor {
		res := n.DeepCopy()
		res.ToReceive.Allowed.Mode = frrk8sv1beta1.AllowAll
		return *res
	}

	for i, n := range router.Neighbors {
		configs = append(configs, frrk8sv1beta1.FRRConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d-orig", cfg.Name, i),
				Namespace: cfg.Namespace,
			},
			Spec: frrk8sv1beta1.FRRConfigurationSpec{
				BGP: frrk8sv1beta1.BGPConfig{
					Routers: []frrk8sv1beta1.Router{
						{
							ASN:       router.ASN,
							VRF:       router.VRF,
							Neighbors: []frrk8sv1beta1.Neighbor{n},
							Prefixes:  router.Prefixes,
						},
					},
				},
			},
		},
			frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-%d-receive-all", cfg.Name, i),
					Namespace: cfg.Namespace,
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{
					BGP: frrk8sv1beta1.BGPConfig{
						Routers: []frrk8sv1beta1.Router{
							{
								ASN:       router.ASN,
								VRF:       router.VRF,
								Neighbors: []frrk8sv1beta1.Neighbor{withReceiveAll(n)},
								Prefixes:  router.Prefixes,
							},
						},
					},
				},
			})
	}

	return configs, nil
}

func splitByNeighReceiveAndAdvertise(cfg frrk8sv1beta1.FRRConfiguration) ([]frrk8sv1beta1.FRRConfiguration, error) {
	if len(cfg.Spec.BGP.Routers) != 1 {
		return nil, fmt.Errorf("expected a config with a single router, got %v", cfg)
	}

	router := cfg.Spec.BGP.Routers[0]
	configs := []frrk8sv1beta1.FRRConfiguration{}

	withoutAdvertise := func(n frrk8sv1beta1.Neighbor) frrk8sv1beta1.Neighbor {
		res := n.DeepCopy()
		res.ToAdvertise = frrk8sv1beta1.Advertise{}
		return *res
	}

	withoutReceive := func(n frrk8sv1beta1.Neighbor) frrk8sv1beta1.Neighbor {
		res := n.DeepCopy()
		res.ToReceive = frrk8sv1beta1.Receive{}
		return *res
	}

	for i, n := range router.Neighbors {
		configs = append(configs, frrk8sv1beta1.FRRConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d-no-advertise", cfg.Name, i),
				Namespace: cfg.Namespace,
			},
			Spec: frrk8sv1beta1.FRRConfigurationSpec{
				BGP: frrk8sv1beta1.BGPConfig{
					Routers: []frrk8sv1beta1.Router{
						{
							ASN:       router.ASN,
							VRF:       router.VRF,
							Neighbors: []frrk8sv1beta1.Neighbor{withoutAdvertise(n)},
							Prefixes:  []string{},
						},
					},
				},
			},
		},
			frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-%d-no-receive", cfg.Name, i),
					Namespace: cfg.Namespace,
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{
					BGP: frrk8sv1beta1.BGPConfig{
						Routers: []frrk8sv1beta1.Router{
							{
								ASN:       router.ASN,
								VRF:       router.VRF,
								Neighbors: []frrk8sv1beta1.Neighbor{withoutReceive(n)},
								Prefixes:  router.Prefixes,
							},
						},
					},
				},
			})
	}

	return configs, nil
}
