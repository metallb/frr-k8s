// SPDX-License-Identifier:Apache-2.0

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	v1beta1 "github.com/metallb/frr-k8s/pkg/client/clientset/versioned/typed/api/v1beta1"
	rest "k8s.io/client-go/rest"
	testing "k8s.io/client-go/testing"
)

type FakeApiV1beta1 struct {
	*testing.Fake
}

func (c *FakeApiV1beta1) FRRConfigurations(namespace string) v1beta1.FRRConfigurationInterface {
	return &FakeFRRConfigurations{c, namespace}
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *FakeApiV1beta1) RESTClient() rest.Interface {
	var ret *rest.RESTClient
	return ret
}
