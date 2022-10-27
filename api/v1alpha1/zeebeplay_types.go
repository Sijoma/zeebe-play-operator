/*
Copyright 2022.

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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ZeebePlaySpec defines the desired state of ZeebePlay
type ZeebePlaySpec struct {
	DeathDate metav1.Time `json:"deathDate,omitempty"`
}

// ZeebePlayStatus defines the observed state of ZeebePlay
type ZeebePlayStatus struct {
	// Represents the observations of a ZeebePlay's current state.
	// ZeebePlay.status.conditions.type are: "Available", "Progressing", and "Degraded"
	// ZeebePlay.status.conditions.status are one of True, False, Unknown.
	// ZeebePlay.status.conditions.reason the value should be a CamelCase string and producers of specific
	// condition types may define expected values and meanings for this field, and whether the values
	// are considered a guaranteed API.
	// ZeebePlay.status.conditions.Message is a human readable message indicating details about the transition.
	// For further information see: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	HttpEndpoint string `json:"httpEndpoint,omitempty"`
	GrpcEndpoint string `json:"grpcEndpoint,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster,path=zeebeplays,shortName=zp

// ZeebePlay is the Schema for the zeebeplays API
type ZeebePlay struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ZeebePlaySpec   `json:"spec,omitempty"`
	Status ZeebePlayStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ZeebePlayList contains a list of ZeebePlay
type ZeebePlayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ZeebePlay `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ZeebePlay{}, &ZeebePlayList{})
}
