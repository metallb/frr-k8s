/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	"log"

	"sigs.k8s.io/controller-runtime/pkg/conversion"

	"github.com/metallb/frr-k8s/api/v1beta2"
)

// ConvertTo converts this FRRConfiguration (v1beta1) to the Hub version (v1beta2).
func (src *FRRConfiguration) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta2.FRRConfiguration)
	log.Printf("ConvertTo: Converting FRRConfiguration from Spoke version v1beta1 to Hub version v1beta2;"+
		"source: %s/%s, target: %s/%s", src.Namespace, src.Name, dst.Namespace, dst.Name)

	// Convert to dst.Spec.BGP.
	var bfdProfiles []v1beta2.BFDProfile
	if src.Spec.BGP.BFDProfiles != nil {
		bfdProfiles = make([]v1beta2.BFDProfile, len(src.Spec.BGP.BFDProfiles))
		for i, profile := range src.Spec.BGP.BFDProfiles {
			bfdProfiles[i] = v1beta2.BFDProfile{
				Name:             profile.Name,
				ReceiveInterval:  profile.ReceiveInterval,
				TransmitInterval: profile.TransmitInterval,
				DetectMultiplier: profile.DetectMultiplier,
				EchoInterval:     profile.EchoInterval,
				EchoMode:         profile.EchoMode,
				PassiveMode:      profile.PassiveMode,
				MinimumTTL:       profile.MinimumTTL,
			}
		}
	}
	dst.Spec.BGP.BFDProfiles = bfdProfiles

	var routers []v1beta2.Router
	if src.Spec.BGP.Routers != nil {
		routers = make([]v1beta2.Router, len(src.Spec.BGP.Routers))
		for i, router := range src.Spec.BGP.Routers {
			routers[i] = v1beta2.Router{
				ASN:       router.ASN,
				ID:        router.ID,
				VRF:       router.VRF,
				Neighbors: convertNeighborsTo(router.Neighbors),
				Prefixes:  router.Prefixes,
				Imports:   convertImportsTo(router.Imports),
			}
		}
	}
	dst.Spec.BGP.Routers = routers

	// Convert to dst.Spec.NodeSelector.
	dst.Spec.NodeSelector = src.Spec.NodeSelector

	// Convert to dst.Spec.Raw.
	dst.Spec.Raw = v1beta2.RawConfig{
		Priority: src.Spec.Raw.Priority,
		Config:   src.Spec.Raw.Config,
	}

	// Copy ObjectMeta to preserve name, namespace, labels, etc.
	dst.ObjectMeta = src.ObjectMeta

	return nil
}

func convertNeighborsTo(src []Neighbor) (dst []v1beta2.Neighbor) {
	if src == nil {
		dst = nil
		return
	}

	dst = make([]v1beta2.Neighbor, len(src))
	for i, neighbor := range src {
		dst[i] = v1beta2.Neighbor{
			ASN:           neighbor.ASN,
			DynamicASN:    v1beta2.DynamicASNMode(neighbor.DynamicASN),
			SourceAddress: neighbor.SourceAddress,
			Address:       neighbor.Address,
			Interface:     neighbor.Interface,
			Port:          neighbor.Port,
			Password:      neighbor.Password,
			PasswordSecret: v1beta2.SecretReference{
				Name:      neighbor.PasswordSecret.Name,
				Namespace: neighbor.PasswordSecret.Namespace,
			},
			HoldTime:               neighbor.HoldTime,
			KeepaliveTime:          neighbor.KeepaliveTime,
			ConnectTime:            neighbor.ConnectTime,
			EBGPMultiHop:           neighbor.EBGPMultiHop,
			BFDProfile:             neighbor.BFDProfile,
			EnableGracefulRestart:  neighbor.EnableGracefulRestart,
			ToAdvertise:            convertAdvertiseTo(neighbor.ToAdvertise),
			ToReceive:              convertReceiveTo(neighbor.ToReceive),
			DisableMP:              neighbor.DisableMP,
			DualStackAddressFamily: neighbor.DualStackAddressFamily,
		}
	}
	return
}

func convertAdvertiseTo(src Advertise) (dst v1beta2.Advertise) {
	dst = v1beta2.Advertise{
		Allowed: v1beta2.AllowedOutPrefixes{
			Prefixes: src.Allowed.Prefixes,
			Mode:     v1beta2.AllowMode(src.Allowed.Mode),
		},
		PrefixesWithLocalPref: convertPrefixesWithLocalPrefTo(src.PrefixesWithLocalPref),
		PrefixesWithCommunity: convertPrefixesWithCommunityTo(src.PrefixesWithCommunity),
	}
	return
}

func convertPrefixesWithLocalPrefTo(src []LocalPrefPrefixes) (dst []v1beta2.LocalPrefPrefixes) {
	if src == nil {
		dst = nil
		return
	}

	dst = make([]v1beta2.LocalPrefPrefixes, len(src))
	for i, prefix := range src {
		dst[i] = v1beta2.LocalPrefPrefixes{
			Prefixes:  prefix.Prefixes,
			LocalPref: prefix.LocalPref,
		}
	}
	return
}

func convertPrefixesWithCommunityTo(src []CommunityPrefixes) (dst []v1beta2.CommunityPrefixes) {
	if src == nil {
		dst = nil
		return
	}

	dst = make([]v1beta2.CommunityPrefixes, len(src))
	for i, prefix := range src {
		dst[i] = v1beta2.CommunityPrefixes{
			Prefixes:  prefix.Prefixes,
			Community: prefix.Community,
		}
	}
	return
}

func convertReceiveTo(src Receive) (dst v1beta2.Receive) {
	dst = v1beta2.Receive{
		Allowed: v1beta2.AllowedInPrefixes{
			Prefixes: convertPrefixSelectorTo(src.Allowed.Prefixes),
			Mode:     v1beta2.AllowMode(src.Allowed.Mode),
		},
	}
	return
}

func convertPrefixSelectorTo(src []PrefixSelector) (dst []v1beta2.PrefixSelector) {
	if src == nil {
		dst = nil
		return
	}

	dst = make([]v1beta2.PrefixSelector, len(src))
	for i, prefixSelector := range src {
		dst[i] = v1beta2.PrefixSelector{
			Prefix: prefixSelector.Prefix,
			LE:     prefixSelector.LE,
			GE:     prefixSelector.GE,
		}
	}
	return
}

func convertImportsTo(src []Import) (dst []v1beta2.Import) {
	if src == nil {
		dst = nil
		return
	}

	dst = make([]v1beta2.Import, len(src))
	for i, imp := range src {
		dst[i] = v1beta2.Import{
			VRF: imp.VRF,
		}
	}
	return
}

// ConvertFrom converts the Hub version (v1beta2) to this FRRConfiguration (v1beta1).
func (dst *FRRConfiguration) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta2.FRRConfiguration)
	log.Printf("ConvertFrom: Converting FRRConfiguration from Hub version v1beta2 to Spoke version v1beta1;"+
		"source: %s/%s, target: %s/%s", src.Namespace, src.Name, dst.Namespace, dst.Name)

	// Convert to dst.Spec.BGP.
	var bfdProfiles []BFDProfile
	if src.Spec.BGP.BFDProfiles != nil {
		bfdProfiles = make([]BFDProfile, len(src.Spec.BGP.BFDProfiles))
		for i, profile := range src.Spec.BGP.BFDProfiles {
			bfdProfiles[i] = BFDProfile{
				Name:             profile.Name,
				ReceiveInterval:  profile.ReceiveInterval,
				TransmitInterval: profile.TransmitInterval,
				DetectMultiplier: profile.DetectMultiplier,
				EchoInterval:     profile.EchoInterval,
				EchoMode:         profile.EchoMode,
				PassiveMode:      profile.PassiveMode,
				MinimumTTL:       profile.MinimumTTL,
			}
		}
	}
	dst.Spec.BGP.BFDProfiles = bfdProfiles

	var routers []Router
	if src.Spec.BGP.Routers != nil {
		routers = make([]Router, len(src.Spec.BGP.Routers))
		for i, router := range src.Spec.BGP.Routers {
			routers[i] = Router{
				ASN:       router.ASN,
				ID:        router.ID,
				VRF:       router.VRF,
				Neighbors: convertNeighborsFrom(router.Neighbors),
				Prefixes:  router.Prefixes,
				Imports:   convertImportsFrom(router.Imports),
			}
		}
	}
	dst.Spec.BGP.Routers = routers

	// Convert to dst.Spec.NodeSelector.
	dst.Spec.NodeSelector = src.Spec.NodeSelector

	// Convert to dst.Spec.Raw.
	dst.Spec.Raw = RawConfig{
		Priority: src.Spec.Raw.Priority,
		Config:   src.Spec.Raw.Config,
	}

	// Copy ObjectMeta to preserve name, namespace, labels, etc.
	dst.ObjectMeta = src.ObjectMeta

	return nil
}

func convertNeighborsFrom(src []v1beta2.Neighbor) (dst []Neighbor) {
	if src == nil {
		dst = nil
		return
	}

	dst = make([]Neighbor, len(src))
	for i, neighbor := range src {
		dst[i] = Neighbor{
			ASN:           neighbor.ASN,
			DynamicASN:    DynamicASNMode(neighbor.DynamicASN),
			SourceAddress: neighbor.SourceAddress,
			Address:       neighbor.Address,
			Interface:     neighbor.Interface,
			Port:          neighbor.Port,
			Password:      neighbor.Password,
			PasswordSecret: SecretReference{
				Name:      neighbor.PasswordSecret.Name,
				Namespace: neighbor.PasswordSecret.Namespace,
			},
			HoldTime:               neighbor.HoldTime,
			KeepaliveTime:          neighbor.KeepaliveTime,
			ConnectTime:            neighbor.ConnectTime,
			EBGPMultiHop:           neighbor.EBGPMultiHop,
			BFDProfile:             neighbor.BFDProfile,
			EnableGracefulRestart:  neighbor.EnableGracefulRestart,
			ToAdvertise:            convertAdvertiseFrom(neighbor.ToAdvertise),
			ToReceive:              convertReceiveFrom(neighbor.ToReceive),
			DisableMP:              neighbor.DisableMP,
			DualStackAddressFamily: neighbor.DualStackAddressFamily,
		}
	}
	return
}

func convertAdvertiseFrom(src v1beta2.Advertise) (dst Advertise) {
	dst = Advertise{
		Allowed: AllowedOutPrefixes{
			Prefixes: src.Allowed.Prefixes,
			Mode:     AllowMode(src.Allowed.Mode),
		},
		PrefixesWithLocalPref: convertPrefixesWithLocalPrefFrom(src.PrefixesWithLocalPref),
		PrefixesWithCommunity: convertPrefixesWithCommunityFrom(src.PrefixesWithCommunity),
	}
	return
}

func convertPrefixesWithLocalPrefFrom(src []v1beta2.LocalPrefPrefixes) (dst []LocalPrefPrefixes) {
	if src == nil {
		dst = nil
		return
	}

	dst = make([]LocalPrefPrefixes, len(src))
	for i, prefix := range src {
		dst[i] = LocalPrefPrefixes{
			Prefixes:  prefix.Prefixes,
			LocalPref: prefix.LocalPref,
		}
	}
	return
}

func convertPrefixesWithCommunityFrom(src []v1beta2.CommunityPrefixes) (dst []CommunityPrefixes) {
	if src == nil {
		dst = nil
		return
	}

	dst = make([]CommunityPrefixes, len(src))
	for i, prefix := range src {
		dst[i] = CommunityPrefixes{
			Prefixes:  prefix.Prefixes,
			Community: prefix.Community,
		}
	}
	return
}

func convertReceiveFrom(src v1beta2.Receive) (dst Receive) {
	dst = Receive{
		Allowed: AllowedInPrefixes{
			Prefixes: convertPrefixSelectorFrom(src.Allowed.Prefixes),
			Mode:     AllowMode(src.Allowed.Mode),
		},
	}
	return
}

func convertPrefixSelectorFrom(src []v1beta2.PrefixSelector) (dst []PrefixSelector) {
	if src == nil {
		dst = nil
		return
	}

	dst = make([]PrefixSelector, len(src))
	for i, prefixSelector := range src {
		dst[i] = PrefixSelector{
			Prefix: prefixSelector.Prefix,
			LE:     prefixSelector.LE,
			GE:     prefixSelector.GE,
		}
	}
	return
}

func convertImportsFrom(src []v1beta2.Import) (dst []Import) {
	if src == nil {
		dst = nil
		return
	}

	dst = make([]Import, len(src))
	for i, imp := range src {
		dst[i] = Import{
			VRF: imp.VRF,
		}
	}
	return
}
