/*
Copyright 2020 Noah Kantrowitz

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
	"github.com/coderanger/controller-utils/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MigratorSpec defines the desired state of Migrator
type MigratorSpec struct {
	Selector         *metav1.LabelSelector `json:"selector"`
	TemplateSelector *metav1.LabelSelector `json:"templateSelector,omitempty"`
	Command          *[]string             `json:"command,omitempty"`
	Image            string                `json:"image,omitempty"`
	Args             *[]string             `json:"args,omitempty"`
	Container        string                `json:"container,omitempty"`
	Labels           map[string]string     `json:"labels,omitempty"`
}

// MigratorStatus defines the observed state of Migrator
type MigratorStatus struct {
	// Represents the observations of a RabbitUsers's current state.
	// Known .status.conditions.type are: Ready, UserReady, PermissionsReady
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions              []conditions.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
	LastSuccessfulMigration string                 `json:"lastSuccessfulMigration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Migrator is the Schema for the migrators API
type Migrator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MigratorSpec   `json:"spec,omitempty"`
	Status MigratorStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MigratorList contains a list of Migrator
type MigratorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Migrator `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Migrator{}, &MigratorList{})
}

// TODO code generator for this.
func (o *Migrator) GetConditions() *[]conditions.Condition {
	return &o.Status.Conditions
}
