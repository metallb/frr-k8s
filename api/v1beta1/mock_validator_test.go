// SPDX-License-Identifier:Apache-2.0

package v1beta1

import (
	"errors"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type mockValidator struct {
	configs    *FRRConfigurationList
	nodes      *v1.NodeList
	forceError bool
}

func (m *mockValidator) Validate(objects ...client.ObjectList) error {
	for _, obj := range objects { // assuming one object per type
		switch list := obj.(type) {
		case *FRRConfigurationList:
			m.configs = list
		case *v1.NodeList:
			m.nodes = list
		default:
			panic("unexpected type")
		}
	}

	if m.forceError {
		return errors.New("error!")
	}
	return nil
}
