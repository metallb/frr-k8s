// SPDX-License-Identifier:Apache-2.0

package webhooks

import (
	"context"
	"errors"

	"github.com/metallb/frr-k8s/api/v1beta1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

type mockValidator struct {
	configs    *v1beta1.FRRConfigurationList
	nodes      *v1.NodeList
	forceError bool
}

func (m *mockValidator) Validate(objects ...client.ObjectList) error {
	for _, obj := range objects { // assuming one object per type
		switch list := obj.(type) {
		case *v1beta1.FRRConfigurationList:
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

type webhookStartupCache struct {
	cache.Cache
	nodes, configs bool
	wait           func(context.Context) bool
}

func (c *webhookStartupCache) GetInformer(_ context.Context, obj client.Object, _ ...cache.InformerGetOption) (cache.Informer, error) {
	switch obj.(type) {
	case *v1.Node:
		c.nodes = true
	case *v1beta1.FRRConfiguration:
		c.configs = true
	}
	return nil, nil
}

func (c *webhookStartupCache) WaitForCacheSync(ctx context.Context) bool { return c.wait(ctx) }

type webhookStartupManager struct {
	ctrl.Manager
	cache  cache.Cache
	server webhook.Server
}

func (m *webhookStartupManager) GetCache() cache.Cache            { return m.cache }
func (m *webhookStartupManager) GetClient() client.Client         { return nil }
func (m *webhookStartupManager) GetScheme() *runtime.Scheme       { return runtime.NewScheme() }
func (m *webhookStartupManager) GetWebhookServer() webhook.Server { return m.server }
