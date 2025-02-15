package v1

import (
	"k8s.io/api/admission/v1beta1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GenerateRequest is a request to process generate rule.
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Policy",type="string",JSONPath=".spec.policy"
// +kubebuilder:printcolumn:name="ResourceKind",type="string",JSONPath=".spec.resource.kind"
// +kubebuilder:printcolumn:name="ResourceName",type="string",JSONPath=".spec.resource.name"
// +kubebuilder:printcolumn:name="ResourceNamespace",type="string",JSONPath=".spec.resource.namespace"
// +kubebuilder:printcolumn:name="status",type="string",JSONPath=".status.state"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:shortName=gr
type GenerateRequest struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	// Spec is the information to identify the generate request.
	Spec GenerateRequestSpec `json:"spec" yaml:"spec"`

	// Status contains statistics related to generate request.
	// +optional
	Status GenerateRequestStatus `json:"status" yaml:"status"`
}

// GenerateRequestSpec stores the request specification.
type GenerateRequestSpec struct {
	// Specifies the name of the policy.
	Policy string `json:"policy" yaml:"policy"`

	// ResourceSpec is the information to identify the generate request.
	Resource ResourceSpec `json:"resource" yaml:"resource"`

	// Context ...
	Context GenerateRequestContext `json:"context" yaml:"context"`
}

// GenerateRequestContext stores the context to be shared.
type GenerateRequestContext struct {
	// +optional
	UserRequestInfo RequestInfo `json:"userInfo,omitempty" yaml:"userInfo,omitempty"`
	// +optional
	AdmissionRequestInfo AdmissionRequestInfoObject `json:"admissionRequestInfo,omitempty" yaml:"admissionRequestInfo,omitempty"`
}

type AdmissionRequestInfoObject struct {
	// +optional
	AdmissionRequest string `json:"admissionRequest,omitempty" yaml:"admissionRequest,omitempty"`
	// +optional
	Operation v1beta1.Operation `json:"operation,omitempty" yaml:"operation,omitempty"`
}

// RequestInfo contains permission info carried in an admission request.
type RequestInfo struct {
	// Roles is a list of possible role send the request.
	// +nullable
	// +optional
	Roles []string `json:"roles" yaml:"roles"`

	// ClusterRoles is a list of possible clusterRoles send the request.
	// +nullable
	// +optional
	ClusterRoles []string `json:"clusterRoles" yaml:"clusterRoles"`

	// UserInfo is the userInfo carried in the admission request.
	// +optional
	AdmissionUserInfo authenticationv1.UserInfo `json:"userInfo" yaml:"userInfo"`
}

// GenerateRequestStatus stores the status of generated request.
type GenerateRequestStatus struct {
	// State represents state of the generate request.
	State GenerateRequestState `json:"state" yaml:"state"`

	// Specifies request status message.
	// +optional
	Message string `json:"message,omitempty" yaml:"message,omitempty"`

	// This will track the resources that are generated by the generate Policy.
	// Will be used during clean up resources.
	GeneratedResources []ResourceSpec `json:"generatedResources,omitempty" yaml:"generatedResources,omitempty"`
}

// GenerateRequestState defines the state of request.
type GenerateRequestState string

const (
	// Pending - the Request is yet to be processed or resource has not been created.
	Pending GenerateRequestState = "Pending"

	// Failed - the Generate Request Controller failed to process the rules.
	Failed GenerateRequestState = "Failed"

	// Completed - the Generate Request Controller created resources defined in the policy.
	Completed GenerateRequestState = "Completed"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GenerateRequestList stores the list of generate requests.
type GenerateRequestList struct {
	metav1.TypeMeta `json:",inline" yaml:",inline"`
	metav1.ListMeta `json:"metadata" yaml:"metadata"`
	Items           []GenerateRequest `json:"items" yaml:"items"`
}
