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

package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FRROperatorConfigurationSpec defines the desired state of FRROperatorConfiguration.
type FRROperatorConfigurationSpec struct {
	// LogLevel defines the log level for the FRR-K8s controller.
	// Valid values are: all, debug, info, warn, error, none.
	// +kubebuilder:validation:Enum=all;debug;info;warn;error;none
	// +optional
	LogLevel string `json:"logLevel,omitempty"`
}

// FRROperatorConfigurationStatus defines the observed state of FRROperatorConfiguration.
type FRROperatorConfigurationStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+genclient
//+kubebuilder:storageversion

// FRROperatorConfiguration holds the FRR Operator configuration with global
// settings for the Operator and FRR.
type FRROperatorConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FRROperatorConfigurationSpec   `json:"spec,omitempty"`
	Status FRROperatorConfigurationStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// FRROperatorConfigurationList contains a list of FRROperatorConfiguration.
type FRROperatorConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FRROperatorConfiguration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FRROperatorConfiguration{}, &FRROperatorConfigurationList{})
}
