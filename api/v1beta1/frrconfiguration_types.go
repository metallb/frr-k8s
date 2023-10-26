/*
Copyright 2023.

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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FRRConfigurationSpec defines the desired state of FRRConfiguration.
type FRRConfigurationSpec struct {
	// +optional
	BGP BGPConfig `json:"bgp,omitempty"`

	// +optional
	Raw RawConfig `json:"raw,omitempty"`
	// Limits the nodes that will attempt to apply this config.
	// When specified, the configuration will be considered only on nodes
	// whose labels match the specified selectors.
	// When it is not specified all nodes will attempt to apply this config.
	// +optional
	NodeSelector metav1.LabelSelector `json:"nodeSelector,omitempty"`
}

type RawConfig struct {
	// Sets the order with this configuration is appended to the
	// bottom of the rendered configuration. A higher value means the
	// raw config is appended later in the configuration file.
	Priority int `json:"priority,omitempty"`

	// A raw FRR configuration to be appended to the configuration
	// rendered via the k8s api.
	Config string `json:"rawConfig,omitempty"`
}

type BGPConfig struct {
	// The list of routers we want FRR to configure (one per VRF).
	// +optional
	Routers []Router `json:"routers"`
	// The list of bfd profiles to be used when configuring the neighbors.
	// +optional
	BFDProfiles []BFDProfile `json:"bfdProfiles,omitempty"`
}

// Router represent a neighbor router we want FRR to connect to.
type Router struct {
	// AS number to use for the local end of the session.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=4294967295
	ASN uint32 `json:"asn"`
	// BGP router ID
	// +optional
	ID string `json:"id,omitempty"`
	// The host VRF used to establish sessions from this router.
	// +optional
	VRF string `json:"vrf,omitempty"`
	// The list of neighbors we want to establish BGP sessions with.
	// +optional
	Neighbors []Neighbor `json:"neighbors,omitempty"`
	// The list of prefixes we want to advertise from this router instance.
	// +optional
	Prefixes []string `json:"prefixes,omitempty"`
}

type Neighbor struct {
	// AS number to use for the local end of the session.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=4294967295
	ASN uint32 `json:"asn"`

	// The IP address to establish the session with.
	Address string `json:"address"`

	// Port to dial when establishing the session.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=16384
	// +kubebuilder:default:=179
	Port uint16 `json:"port,omitempty"`

	// passwordSecret is name of the authentication secret for the neighbor.
	// the secret must be of type "kubernetes.io/basic-auth", and created in the
	// same namespace as the frr-k8s daemon. The password is stored in the
	// secret as the key "password".
	// +optional
	PasswordSecret v1.SecretReference `json:"password,omitempty"`

	// Requested BGP hold time, per RFC4271.
	// +kubebuilder:default:="90s"
	// +optional
	HoldTime metav1.Duration `json:"holdTime,omitempty"`

	// Requested BGP keepalive time, per RFC4271.
	// +kubebuilder:default:="30s"
	// +optional
	KeepaliveTime metav1.Duration `json:"keepaliveTime,omitempty"`

	// To set if the BGPPeer is multi-hops away.
	// +optional
	EBGPMultiHop bool `json:"ebgpMultiHop,omitempty"`

	// The name of the BFD Profile to be used for the BFD session associated
	// to the BGP session. If not set, the BFD session won't be set up.
	// +optional
	BFDProfile string `json:"bfdProfile,omitempty"`

	// ToAdvertise represents the list of prefixes to advertise to the given neighbor
	// and the associated properties.
	// +optional
	ToAdvertise Advertise `json:"toAdvertise,omitempty"`

	// Receive represents the list of prefixes to receive from the given neighbor.
	// +optional
	ToReceive Receive `json:"toReceive,omitempty"`
}

type Advertise struct {
	// Prefixes is the list of prefixes allowed to be propagated to
	// this neighbor. They must match the prefixes defined in the router.
	Allowed AllowedOutPrefixes `json:"allowed,omitempty"`

	// PrefixesWithLocalPref is a list of prefixes that are associated to a local
	// preference when being advertised. The prefixes associated to a given local pref
	// must be in the prefixes allowed to be advertised.
	// +optional
	PrefixesWithLocalPref []LocalPrefPrefixes `json:"withLocalPref,omitempty"`

	// PrefixesWithCommunity is a list of prefixes that are associated to a
	// bgp community when being advertised. The prefixes associated to a given local pref
	// must be in the prefixes allowed to be advertised.
	// +optional
	PrefixesWithCommunity []CommunityPrefixes `json:"withCommunity,omitempty"`
}

type Receive struct {
	// Prefixes is the list of prefixes allowed to be received from
	// this neighbor.
	// +optional
	Allowed AllowedInPrefixes `json:"allowed,omitempty"`
}

type PrefixSelector struct {
	// +kubebuilder:validation:Format="cidr"
	Prefix string `json:"prefix,omitempty"`
	// The prefix length modifier. This selector accepts any matching prefix with length
	// less or equal the given value.
	// +kubebuilder:validation:Maximum:=128
	// +kubebuilder:validation:Minimum:=1
	LE uint32 `json:"le,omitempty"`
	// The prefix length modifier. This selector accepts any matching prefix with length
	// greater or equal the given value.
	// +kubebuilder:validation:Maximum:=128
	// +kubebuilder:validation:Minimum:=1
	GE uint32 `json:"ge,omitempty"`
}

type AllowedInPrefixes struct {
	Prefixes []PrefixSelector `json:"prefixes,omitempty"`
	// Mode is the mode to use when handling the prefixes.
	// When set to "filtered", only the prefixes in the given list will be allowed.
	// When set to "all", all the prefixes configured on the router will be allowed.
	// +kubebuilder:default:=filtered
	Mode AllowMode `json:"mode,omitempty"`
}

type AllowedOutPrefixes struct {
	Prefixes []string `json:"prefixes,omitempty"`
	// Mode is the mode to use when handling the prefixes.
	// When set to "filtered", only the prefixes in the given list will be allowed.
	// When set to "all", all the prefixes configured on the router will be allowed.
	// +kubebuilder:default:=filtered
	Mode AllowMode `json:"mode,omitempty"`
}

type LocalPrefPrefixes struct {
	// Prefixes is the list of prefixes associated to the local preference.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:Format="cidr"
	Prefixes  []string `json:"prefixes,omitempty"`
	LocalPref uint32   `json:"localPref,omitempty"`
}

type CommunityPrefixes struct {
	// Prefixes is the list of prefixes associated to the community.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:Format="cidr"
	Prefixes  []string `json:"prefixes,omitempty"`
	Community string   `json:"community,omitempty"`
}

type BFDProfile struct {
	// The name of the BFD Profile to be referenced in other parts
	// of the configuration.
	Name string `json:"name"`

	// The minimum interval that this system is capable of
	// receiving control packets in milliseconds.
	// Defaults to 300ms.
	// +kubebuilder:validation:Maximum:=60000
	// +kubebuilder:validation:Minimum:=10
	// +kubebuilder:default:=300
	// +optional
	ReceiveInterval uint32 `json:"receiveInterval,omitempty"`

	// The minimum transmission interval (less jitter)
	// that this system wants to use to send BFD control packets in
	// milliseconds. Defaults to 300ms
	// +kubebuilder:validation:Maximum:=60000
	// +kubebuilder:validation:Minimum:=10
	// +kubebuilder:default:=300
	// +optional
	TransmitInterval uint32 `json:"transmitInterval,omitempty"`

	// Configures the detection multiplier to determine
	// packet loss. The remote transmission interval will be multiplied
	// by this value to determine the connection loss detection timer.
	// +kubebuilder:validation:Maximum:=255
	// +kubebuilder:validation:Minimum:=2
	// +kubebuilder:default:=3
	// +optional
	DetectMultiplier uint32 `json:"detectMultiplier,omitempty"`

	// Configures the minimal echo receive transmission
	// interval that this system is capable of handling in milliseconds.
	// Defaults to 50ms
	// +kubebuilder:validation:Maximum:=60000
	// +kubebuilder:validation:Minimum:=10
	// +kubebuilder:default:=50
	// +optional
	EchoInterval uint32 `json:"echoInterval,omitempty"`

	// Enables or disables the echo transmission mode.
	// This mode is disabled by default, and not supported on multi
	// hops setups.
	// +optional
	EchoMode bool `json:"echoMode,omitempty"`

	// Mark session as passive: a passive session will not
	// attempt to start the connection and will wait for control packets
	// from peer before it begins replying.
	// +optional
	PassiveMode bool `json:"passiveMode,omitempty"`

	// For multi hop sessions only: configure the minimum
	// expected TTL for an incoming BFD control packet.
	// +kubebuilder:validation:Maximum:=254
	// +kubebuilder:validation:Minimum:=1
	// +kubebuilder:default:=254
	// +optional
	MinimumTTL uint32 `json:"minimumTtl,omitempty"`
}

// FRRConfigurationStatus defines the observed state of FRRConfiguration.
type FRRConfigurationStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// FRRConfiguration is the Schema for the frrconfigurations API.
type FRRConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FRRConfigurationSpec   `json:"spec,omitempty"`
	Status FRRConfigurationStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// FRRConfigurationList contains a list of FRRConfiguration.
type FRRConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FRRConfiguration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FRRConfiguration{}, &FRRConfigurationList{})
}

// +kubebuilder:validation:Enum=all;filtered
type AllowMode string

const (
	AllowAll        AllowMode = "all"
	AllowRestricted AllowMode = "filtered"
)
