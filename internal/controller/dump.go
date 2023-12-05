// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"encoding/json"

	"github.com/davecgh/go-spew/spew"
	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frr-k8s/internal/frr"
)

func dumpK8sConfigs(c frrk8sv1beta1.FRRConfigurationList) string {
	res := ""
	for _, cfg := range c.Items {
		res = res + "\n" + dumpResource(sanitizedConfig(cfg))
	}
	return res
}

func dumpFRRConfig(c *frr.Config) string {
	toDump := *c
	noPasswordRouters := make([]*frr.RouterConfig, 0, len(c.Routers))
	for _, r := range toDump.Routers {
		noPasswordNeighbors := make([]*frr.NeighborConfig, 0, len(r.Neighbors))
		for _, n := range r.Neighbors {
			n1 := *n
			n1.Password = "<retracted>"
			noPasswordNeighbors = append(noPasswordNeighbors, &n1)
		}
		r1 := *r
		r1.Neighbors = noPasswordNeighbors
		noPasswordRouters = append(noPasswordRouters, &r1)
	}
	toDump.Routers = noPasswordRouters
	return dumpResource(toDump)
}

func dumpResource(i interface{}) string {
	toDump, err := json.Marshal(i)
	if err != nil {
		return spew.Sdump(i)
	}
	return string(toDump)
}

func sanitizedConfig(c frrk8sv1beta1.FRRConfiguration) frrk8sv1beta1.FRRConfiguration {
	res := c.DeepCopy()
	for _, router := range res.Spec.BGP.Routers {
		for i := range router.Neighbors {
			router.Neighbors[i].Password = "<retracted>"
		}
	}
	delete(res.Annotations, "kubectl.kubernetes.io/last-applied-configuration")

	return *res
}
