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

package controller

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tailscalev1alpha1 "github.com/the1andonlymanojos/tailgate-controller/api/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("TSIngress Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		tsingress := &tailscalev1alpha1.TSIngress{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind TSIngress")
			err := k8sClient.Get(ctx, typeNamespacedName, tsingress)
			if err != nil && errors.IsNotFound(err) {
				resource := &tailscalev1alpha1.TSIngress{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					// TODO(user): Specify other spec details if needed.
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &tailscalev1alpha1.TSIngress{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance TSIngress")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &TSIngressReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})
})

func TestGenerateConfigFromTSIngress(t *testing.T) {
	tsIngress := tailscalev1alpha1.TSIngress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-ingress",
			Namespace: "test-namespace",
		},
		Spec: tailscalev1alpha1.TSIngressSpec{
			BackendService: "my-backend",
			Hostname:       []string{"my-ingress"},
			ProxyRules: []tailscalev1alpha1.ProxyRule{
				{
					Protocol:    "tcp",
					ListenPort:  443,
					BackendPort: 8080,
					Funnel:      true,
				},
				{
					Protocol:    "tcp",
					ListenPort:  80,
					BackendPort: 9090,
					Funnel:      false,
				},
			},
		},
	}

	authKey := "tskey-auth-fakekey"
	got, err := GenerateConfigFromTSIngress(tsIngress, authKey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := `tailscale:
  auth_key: "tskey-auth-fakekey"
  hostname: "my-ingress"
  state_dir: "/var/lib/tailgate"

proxies:
- protocol: "tcp"
  listen_addr: ":443"
  backend_addr: "my-backend.test-namespace.svc.cluster.local:8080"
  funnel: true
- protocol: "tcp"
  listen_addr: ":80"
  backend_addr: "my-backend.test-namespace.svc.cluster.local:9090"
  funnel: false
`

	if got != expected {
		t.Errorf("GenerateConfigFromTSIngress() = %v, want %v", got, expected)
	}
}

// Add test for validation
func TestTSIngressSpecValidation(t *testing.T) {
	tests := []struct {
		name    string
		spec    tailscalev1alpha1.TSIngressSpec
		wantErr bool
	}{
		{
			name: "valid configuration",
			spec: tailscalev1alpha1.TSIngressSpec{
				ProxyRules: []tailscalev1alpha1.ProxyRule{
					{
						Protocol:    "tcp",
						ListenPort:  443,
						BackendPort: 8080,
						Funnel:      true,
					},
					{
						Protocol:    "tcp",
						ListenPort:  80,
						BackendPort: 9090,
						Funnel:      false,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "duplicate listen ports",
			spec: tailscalev1alpha1.TSIngressSpec{
				ProxyRules: []tailscalev1alpha1.ProxyRule{
					{
						Protocol:    "tcp",
						ListenPort:  443,
						BackendPort: 8080,
						Funnel:      true,
					},
					{
						Protocol:    "tcp",
						ListenPort:  443,
						BackendPort: 9090,
						Funnel:      false,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid funnel port",
			spec: tailscalev1alpha1.TSIngressSpec{
				ProxyRules: []tailscalev1alpha1.ProxyRule{
					{
						Protocol:    "tcp",
						ListenPort:  8080,
						BackendPort: 8080,
						Funnel:      true,
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.spec.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("TSIngressSpec.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
