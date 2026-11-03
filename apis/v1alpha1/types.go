// SPDX-FileCopyrightText: 2025 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	resource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpv2 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// interface checks to ensure our types conform to the crossplane-runtime interfaces
var (
	_ resource.ProviderConfig           = &ProviderConfig{}
	_ resource.ProviderConfig           = &ClusterProviderConfig{}
	_ resource.TypedProviderConfigUsage = &ProviderConfigUsage{}
	_ resource.ProviderConfigUsageList  = &ProviderConfigUsageList{}
)

// A ProviderConfigStatus is the observed state of a provider config: the Open WebUI
// instance it reaches and the API profile used to talk to it.
type ProviderConfigStatus struct {
	xpv2.ProviderConfigStatus `json:",inline"`

	// Version is the Open WebUI version the instance reports on GET /api/version, e.g.
	// 0.12.4. It is empty until the provider connects.
	// +optional
	Version string `json:"version,omitempty"`

	// Profile is the minor whose API profile the provider uses, e.g. 0.12. With
	// allowUnsupportedVersions, an unsupported version uses the newest profile.
	// +optional
	Profile string `json:"profile,omitempty"`
}

// A LocalSecretKeySelector selects a key of a Secret in the namespace of the
// ProviderConfig.
type LocalSecretKeySelector struct {
	// Name of the Secret.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Key of the Secret's data that holds the value.
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`
}

// A SecretKeySelector selects a key of a Secret in any namespace.
type SecretKeySelector struct {
	// Name of the Secret.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Namespace of the Secret.
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`

	// Key of the Secret's data that holds the value.
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`
}

// ConnectionSettings are the settings of a provider config that do not refer to a Secret.
type ConnectionSettings struct {
	// URL is the root URL of the Open WebUI instance, e.g. https://openwebui.example.com or
	// http://open-webui.open-webui.svc:8080. A path prefix is kept; credentials, a query
	// and a fragment are refused.
	// +kubebuilder:validation:Pattern=`^https?://[^/?#@]+(/[^?#]*)?$`
	URL string `json:"url"`

	// AuthHeader is the HTTP header that carries the API key. Authorization, the default,
	// sends "Bearer <key>"; any other header, e.g. x-api-key behind a proxy that consumes
	// Authorization, sends the bare key.
	// +kubebuilder:default=Authorization
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9-]+$`
	// +optional
	AuthHeader string `json:"authHeader,omitempty"`

	// InsecureSkipVerify disables the verification of the server's TLS certificate. It is
	// off by default and logged as a warning; prefer caCertSecretRef.
	// +optional
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`

	// AllowUnsupportedVersions lets the provider talk to an Open WebUI minor it has no API
	// profile for, with the newest profile and a Warning event. Without it, an unsupported
	// minor makes the provider config unavailable.
	// +optional
	AllowUnsupportedVersions bool `json:"allowUnsupportedVersions,omitempty"`
}

// A CredentialsSource is where the provider reads the API key from.
// +kubebuilder:validation:Enum=Secret
type CredentialsSource string

// CredentialsSourceSecret reads the API key from a key of a Secret.
const CredentialsSourceSecret CredentialsSource = "Secret"

// ProviderCredentials are the credentials of a ProviderConfig: the API key of an Open
// WebUI admin account.
type ProviderCredentials struct {
	// Source of the API key. Secret, the only source, reads it from secretRef.
	Source CredentialsSource `json:"source"`

	// SecretRef selects the Secret key that holds the API key, in the namespace of the
	// ProviderConfig.
	SecretRef LocalSecretKeySelector `json:"secretRef"`
}

// ClusterProviderCredentials are the credentials of a ClusterProviderConfig: the API key
// of an Open WebUI admin account.
type ClusterProviderCredentials struct {
	// Source of the API key. Secret, the only source, reads it from secretRef.
	Source CredentialsSource `json:"source"`

	// SecretRef selects the Secret key that holds the API key, in the namespace it names.
	SecretRef SecretKeySelector `json:"secretRef"`
}

// A ProviderConfigSpec configures the connection to an Open WebUI instance. Its Secrets
// live in the namespace of the ProviderConfig.
type ProviderConfigSpec struct {
	ConnectionSettings `json:",inline"`

	// Credentials select the API key of an Open WebUI admin account, which the provider
	// sends in authHeader on every request. The Secret is read in the ProviderConfig's
	// namespace.
	Credentials ProviderCredentials `json:"credentials"`

	// CACertSecretRef selects the Secret key that holds PEM-encoded CA certificates,
	// trusted in addition to the system's to verify the server's certificate.
	// +optional
	CACertSecretRef *LocalSecretKeySelector `json:"caCertSecretRef,omitempty"`
}

// A ClusterProviderConfigSpec configures the connection to an Open WebUI instance. Its
// Secrets name their namespace.
type ClusterProviderConfigSpec struct {
	ConnectionSettings `json:",inline"`

	// Credentials select the API key of an Open WebUI admin account, which the provider
	// sends in authHeader on every request. The Secret is read in the namespace secretRef
	// names.
	Credentials ClusterProviderCredentials `json:"credentials"`

	// CACertSecretRef selects the Secret key that holds PEM-encoded CA certificates,
	// trusted in addition to the system's to verify the server's certificate.
	// +optional
	CACertSecretRef *SecretKeySelector `json:"caCertSecretRef,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion

// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="VERSION",type="string",JSONPath=".status.version"
// +kubebuilder:printcolumn:name="PROFILE",type="string",JSONPath=".status.profile"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="URL",type="string",JSONPath=".spec.url",priority=1
// +kubebuilder:resource:scope=Namespaced,categories={crossplane,provider,openwebui}
// A ProviderConfig configures how the provider connects to an Open WebUI instance, for
// managed resources in its namespace.
type ProviderConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProviderConfigSpec   `json:"spec"`
	Status ProviderConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProviderConfigList contains a list of Provider
type ProviderConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProviderConfig `json:"items"`
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion

// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="CONFIG-NAME",type="string",JSONPath=".providerConfigRef.name"
// +kubebuilder:printcolumn:name="RESOURCE-KIND",type="string",JSONPath=".resourceRef.kind"
// +kubebuilder:printcolumn:name="RESOURCE-NAME",type="string",JSONPath=".resourceRef.name"
// +kubebuilder:resource:scope=Namespaced,categories={crossplane,provider,openwebui}
// A ProviderConfigUsage indicates that a resource is using a ProviderConfig or a
// ClusterProviderConfig. There is deliberately no cluster scoped usage type, Usages always live in
// the namespace of the MR that created them, and record which kind of config they refer to.
type ProviderConfigUsage struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	xpv2.TypedProviderConfigUsage `json:",inline"`
}

// +kubebuilder:object:root=true

// ProviderConfigUsageList contains a list of ProviderConfigUsage
type ProviderConfigUsageList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProviderConfigUsage `json:"items"`
}

// +kubebuilder:object:root=true

// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="VERSION",type="string",JSONPath=".status.version"
// +kubebuilder:printcolumn:name="PROFILE",type="string",JSONPath=".status.profile"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="URL",type="string",JSONPath=".spec.url",priority=1
// +kubebuilder:resource:scope=Cluster,categories={crossplane,provider,openwebui}
// A ClusterProviderConfig configures how the provider connects to an Open WebUI
// instance, for managed resources in any namespace.
type ClusterProviderConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterProviderConfigSpec `json:"spec"`
	Status ProviderConfigStatus      `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterProviderConfigList contains a list of ProviderConfig.
type ClusterProviderConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterProviderConfig `json:"items"`
}
