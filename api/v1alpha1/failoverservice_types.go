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

// FailoverServiceSpec defines the desired state of FailoverService
type FailoverServiceSpec struct {
	// Important: Run "make" to regenerate code after modifying this file

	Service ServiceConfig `json:"service"`
}

// ServiceConfig defines the configuration for the services which are managed by the FailoverService.
type ServiceConfig struct {
	Name   string `json:"name"`
	Suffix string `json:"suffix,omitempty"`
}

// FailoverServiceStatus defines the observed state of FailoverService
type FailoverServiceStatus struct {
	// Important: Run "make" to regenerate code after modifying this file

	ActiveTarget     string             `json:"activeTarget,omitempty"`
	AvailableTargets int                `json:"availableTargets,omitempty"`
	LastTransition   metav1.Time        `json:"lastTransition,omitempty"`
	Conditions       []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:resource:scope="Namespaced",shortName="fos"
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
//+kubebuilder:printcolumn:name="target",type=string,JSONPath=`.status.activeTarget`
//+kubebuilder:printcolumn:name="available",type=string,JSONPath=`.status.availableTargets`
//+kubebuilder:printcolumn:name="transition",type=date,JSONPath=`.status.lastTransition`
//+kubebuilder:printcolumn:name="age",type=date,JSONPath=`.metadata.creationTimestamp`

// FailoverService is the Schema for the failoverservices API
type FailoverService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FailoverServiceSpec   `json:"spec,omitempty"`
	Status FailoverServiceStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// FailoverServiceList contains a list of FailoverService
type FailoverServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FailoverService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FailoverService{}, &FailoverServiceList{})
}
