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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ImageRegistry represents an image registry as well as the
// necessary credentials to access with.
// Note: Postpone define the credentials to the next release.
type ImageRegistry struct {
	// Registry is the image registry hostname, like:
	// - docker.io
	// - fictional.registry.example
	// +kubebuilder:validation:Required
	Registry string `json:"registry"`
}

// Image allows to customize the image used for components.
type Image struct {
	// ImageRepository sets the container registry to pull images from.
	// if not set, the ImageRepository defined in KarmadaSpec will be used instead.
	// +kubebuilder:validation:Optional
	ImageRepository string `json:"imageRepository,omitempty"`

	// ImageTag allows to specify a tag for the image.
	// In case this value is set, operator does not change automatically the version
	// of the above components during upgrades.
	// +kubebuilder:validation:Optional
	ImageTag string `json:"imageTag,omitempty"`
}

type CommonSettings struct {
	// Image allows to customize the image used for the component.
	Image `json:",inline"`

	// Number of desired pods. This is a pointer to distinguish between explicit
	// zero and not specified. Defaults to 1.
	// +kubebuilder:validation:Optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Map of string keys and values that can be used to organize and categorize
	// (scope and select) objects. May match selectors of replication controllers
	// and services.
	// More info: http://kubernetes.io/docs/user-guide/labels
	// +kubebuilder:validation:Optional
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations is an unstructured key value map stored with a resource that may be
	// set by external tools to store and retrieve arbitrary metadata. They are not
	// queryable and should be preserved when modifying objects.
	// More info: http://kubernetes.io/docs/user-guide/annotations
	// +kubebuilder:validation:Optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// Compute Resources required by this component.
	// More info: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// +kubebuilder:validation:Optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// ObserverJaeger define the jaeger component setting
type ObserverJaeger struct {
	// CommonSettings holds common settings to kubernetes api server.
	CommonSettings `json:",inline"`

	// ServiceType represents the service type of Jaeger
	// it is NodePort by default.
	// +kubebuilder:validation:Optional
	ServiceType corev1.ServiceType `json:"serviceType,omitempty"`

	// Name define the jaeger name
	// it is Observer's jaeger by default
	// +kubebuilder:validation:Optional
	Name string `json:"name,omitempty"`

	// Namespace define the jaeger namespace
	// it is Observer's namespace by default
	// +kubebuilder:validation:Optional
	Namespace string `json:"namespace,omitempty"`
}

// ResourceSelector the resources will be selected.
type ResourceSelector struct {
	// APIVersion represents the API version of the target resources.
	// +kubebuilder:validation:Required
	APIVersion string `json:"apiVersion"`

	// Kind represents the Kind of the target resources.
	// +kubebuilder:validation:Required
	Kind string `json:"kind"`

	// Namespace of the target resource.
	// Default is empty, which means inherit from the parent object scope.
	// +kubebuilder:validation:Optional
	Namespace string `json:"namespace,omitempty"`

	// Name of the target resource.
	// Default is empty, which means selecting all resources.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
}

// Launcher define the initContainer launcher setting
type Launcher struct {
	// Image allows to customize the image used for the component.
	Image `json:",inline"`
}

// Agent define the agent setting
type Agent struct {
	// Image allows to customize the image used for the component.
	Image `json:",inline"`

	// Endpoint define the report target
	// it is jaeger component endpoint by default
	// +kubebuilder:validation:Optional
	Endpoint string `json:"endpoint,omitempty"`
}

// ObserverSpec defines the desired state of Observer
type ObserverSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Jaeger define the jaeger component setting
	// +kubebuilder:validation:Optional
	Jaeger *ObserverJaeger `json:"jaeger,omitempty"`

	// ResourceSelectors used to select resources.
	// Nil or empty selector is not allowed and doesn't mean match all kinds
	// of resources for security concerns that sensitive resources(like Secret)
	// might be accidentally propagated.
	// +required
	// +kubebuilder:validation:MinItems=1
	ResourceSelectors []ResourceSelector `json:"resourceSelectors"`

	// Launcher define the initContainer launcher setting
	// it is keyval/launcher:v0.1 by default
	// +kubebuilder:validation:Optional
	Launcher *Launcher `json:"launcher,omitempty"`

	// Agent define the agent setting
	// it is keyval/otel-go-agent:v0.6.0 by default
	// +kubebuilder:validation:Optional
	Agent *Agent `json:"agent,omitempty"`
}

// ObserverStatus defines the observed state of Observer
type ObserverStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope="Namespaced",singular="observer",path="observers"
//+kubebuilder:printcolumn:name="Ready",type=string,description="Report the puller ready status",JSONPath=`.status.conditions[?(@.type=="Ready")].status`,priority=0
//+kubebuilder:printcolumn:name="Age",type=date,description="The creation date",JSONPath=`.metadata.creationTimestamp`,priority=0

// Observer is the Schema for the observers API
type Observer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ObserverSpec   `json:"spec,omitempty"`
	Status ObserverStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ObserverList contains a list of Observer
type ObserverList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Observer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Observer{}, &ObserverList{})
}
