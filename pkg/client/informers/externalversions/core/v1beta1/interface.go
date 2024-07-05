// SPDX-License-Identifier:Apache-2.0

// Code generated by informer-gen. DO NOT EDIT.

package v1beta1

import (
	internalinterfaces "github.com/metallb/frr-k8s/pkg/client/informers/externalversions/internalinterfaces"
)

// Interface provides access to all the informers in this group version.
type Interface interface {
	// FRRConfigurations returns a FRRConfigurationInformer.
	FRRConfigurations() FRRConfigurationInformer
}

type version struct {
	factory          internalinterfaces.SharedInformerFactory
	namespace        string
	tweakListOptions internalinterfaces.TweakListOptionsFunc
}

// New returns a new Interface.
func New(f internalinterfaces.SharedInformerFactory, namespace string, tweakListOptions internalinterfaces.TweakListOptionsFunc) Interface {
	return &version{factory: f, namespace: namespace, tweakListOptions: tweakListOptions}
}

// FRRConfigurations returns a FRRConfigurationInformer.
func (v *version) FRRConfigurations() FRRConfigurationInformer {
	return &fRRConfigurationInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}
