// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"encoding/json"

	"github.com/davecgh/go-spew/spew"
	frrk8sv1beta1 "github.com/metallb/frrk8s/api/v1beta1"
	"github.com/metallb/frrk8s/internal/frr"
)

func dumpK8sConfigs(c frrk8sv1beta1.FRRConfigurationList) string {
	// TODO hide secrets when we have them
	res := ""
	for _, cfg := range c.Items {
		res = res + "\n" + dumpResource(cfg)
	}
	return res
}

func dumpFRRConfig(c *frr.Config) string {
	// TODO hide secrets when we have them
	return dumpResource(*c)
}

func dumpResource(i interface{}) string {
	toDump, err := json.Marshal(i)
	if err != nil {
		return spew.Sdump(i)
	}
	return string(toDump)
}
