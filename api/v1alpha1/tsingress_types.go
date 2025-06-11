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

package v1alpha1

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ProxyRule defines a single proxy forwarding rule
type ProxyRule struct {
	Protocol    string `json:"protocol,omitempty"`
	ListenPort  int    `json:"listenPort,omitempty"`
	BackendPort int    `json:"backendPort,omitempty"`
	Funnel      bool   `json:"funnel,omitempty"`
}

// TSIngressSpec defines the desired state of TSIngress.
type TSIngressSpec struct {
	// Backend configuration
	BackendService string `json:"backendService,omitempty"`

	// Proxy rules configuration
	ProxyRules []ProxyRule `json:"proxyRules,omitempty"`

	// DNS and domain configuration
	Hostname  []string `json:"hostnames,omitempty"`
	Domain    string   `json:"subdomain,omitempty"`
	DNSName   string   `json:"dnsName,omitempty"`
	UpdateDNS bool     `json:"updateDNS,omitempty"`

	// Tailscale configuration
	TailnetName string `json:"tailnetName,omitempty"`
	Ephemeral   bool   `json:"ephemeral,omitempty"`
}

// Validate validates the TSIngressSpec
func (s *TSIngressSpec) Validate() error {
	// Check for unique listen ports
	portMap := make(map[int]bool)
	for _, rule := range s.ProxyRules {
		if portMap[rule.ListenPort] {
			return fmt.Errorf("duplicate listen port: %d", rule.ListenPort)
		}
		portMap[rule.ListenPort] = true

		// Validate funnel ports
		if rule.Funnel {
			validFunnelPorts := map[int]bool{
				443:   true, // HTTPS
				3306:  true, // MySQL
				10000: true, // Custom port
			}
			if !validFunnelPorts[rule.ListenPort] {
				return fmt.Errorf("invalid funnel port: %d. Valid funnel ports are: 443, 3306, 10000", rule.ListenPort)
			}
		}
	}
	return nil
}

// TSIngressStatus defines the observed state of TSIngress.
type TSIngressStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	Initialized bool   `json:"initialized,omitempty"`
	State       string `json:"state,omitempty"`
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// TSIngress is the Schema for the tsingresses API.
type TSIngress struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TSIngressSpec   `json:"spec,omitempty"`
	Status TSIngressStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TSIngressList contains a list of TSIngress.
type TSIngressList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TSIngress `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TSIngress{}, &TSIngressList{})
}
