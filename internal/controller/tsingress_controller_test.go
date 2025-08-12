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
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	intstr "k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tailscalev1alpha1 "github.com/the1andonlymanojos/tailgate-controller/api/v1alpha1"
	"github.com/the1andonlymanojos/tailgate-controller/internal/tailscale"
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

var _ = Describe("TSIngress Reconcile Behaviors", func() {
	ctx := context.Background()

	makeService := func(name string, namespace string, ports ...int32) *corev1.Service {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{},
			},
		}
		for i, p := range ports {
			svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{
				Name:       fmt.Sprintf("port-%d-%d", i, p),
				Port:       p,
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.FromInt(int(p)),
			})
		}
		return svc
	}

	It("adds a finalizer on first reconcile", func() {
		name := types.NamespacedName{Name: "finalizer-test", Namespace: "default"}

		// Create CR with minimal spec
		cr := &tailscalev1alpha1.TSIngress{
			ObjectMeta: metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace},
			Spec:       tailscalev1alpha1.TSIngressSpec{},
		}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())

		r := &TSIngressReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}

		// First reconcile adds finalizer
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		var updated tailscalev1alpha1.TSIngress
		Expect(k8sClient.Get(ctx, name, &updated)).To(Succeed())
		Expect(containsString(updated.Finalizers, "tailscale.tailgate.run/finalizer")).To(BeTrue())
	})

	It("creates ConfigMap, PVC, and Deployment when missing", func() {
		name := types.NamespacedName{Name: "create-test", Namespace: "default"}

		// Backend service with required ports
		svc := makeService("backend-svc", name.Namespace, 8080, 9090)
		Expect(k8sClient.Create(ctx, svc)).To(Succeed())

		// Stub external calls
		originalMint := mintAuthKeyFn
		originalGetAll := getAllDevicesFn
		originalDeleteDevice := deleteDeviceFn
		originalUpdateDNS := updateDNSFn

		mintAuthKeyFn = func(ctx context.Context, _ *tailscale.TokenCache, _ string, _ string) (string, error) {
			return "test-auth-key", nil
		}
		getAllDevicesFn = func(ctx context.Context, _ *tailscale.TokenCache, _ string) ([]tailscale.Device, error) {
			return []tailscale.Device{}, nil
		}
		deleteDeviceFn = func(ctx context.Context, _ *tailscale.TokenCache, _ string) error { return nil }
		updateDNSFn = func(ctx context.Context, _, _, _, _ string) error { return nil }
		defer func() {
			mintAuthKeyFn = originalMint
			getAllDevicesFn = originalGetAll
			deleteDeviceFn = originalDeleteDevice
			updateDNSFn = originalUpdateDNS
		}()

		cr := &tailscalev1alpha1.TSIngress{
			ObjectMeta: metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace},
			Spec: tailscalev1alpha1.TSIngressSpec{
				BackendService: svc.Name,
				Hostname:       []string{"create-test-host"},
				TailnetName:    "example.ts.net",
				UpdateDNS:      false,
				ProxyRules: []tailscalev1alpha1.ProxyRule{
					{Protocol: "tcp", ListenPort: 443, BackendPort: 8080, Funnel: true},
					{Protocol: "tcp", ListenPort: 80, BackendPort: 9090, Funnel: false},
				},
			},
		}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())

		r := &TSIngressReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}

		// First reconcile adds finalizer
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())
		// Second reconcile creates resources
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		// Assert ConfigMap
		var cm corev1.ConfigMap
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.Name + "-proxy-config", Namespace: name.Namespace}, &cm)).To(Succeed())
		Expect(cm.Data).To(HaveKey("tailgate.yaml"))
		Expect(cm.Data["tailgate.yaml"]).To(ContainSubstring("auth_key: \"test-auth-key\""))

		// Assert PVC
		var pvc corev1.PersistentVolumeClaim
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.Name + "-proxy-state", Namespace: name.Namespace}, &pvc)).To(Succeed())

		// Assert Deployment exists
		var deploy appsv1.Deployment
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.Name + "-proxy", Namespace: name.Namespace}, &deploy)).To(Succeed())
	})

	It("fails when backend port is not present on the Service", func() {
		name := types.NamespacedName{Name: "port-validate-test", Namespace: "default"}

		// Service only exposes 8080
		svc := makeService("backend-svc-port", name.Namespace, 8080)
		Expect(k8sClient.Create(ctx, svc)).To(Succeed())

		// Stub external calls (won't be reached if validation fails but keep safe)
		originalMint := mintAuthKeyFn
		originalGetAll := getAllDevicesFn
		mintAuthKeyFn = func(ctx context.Context, _ *tailscale.TokenCache, _ string, _ string) (string, error) { return "", nil }
		getAllDevicesFn = func(ctx context.Context, _ *tailscale.TokenCache, _ string) ([]tailscale.Device, error) {
			return nil, nil
		}
		defer func() {
			mintAuthKeyFn = originalMint
			getAllDevicesFn = originalGetAll
		}()

		cr := &tailscalev1alpha1.TSIngress{
			ObjectMeta: metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace},
			Spec: tailscalev1alpha1.TSIngressSpec{
				BackendService: svc.Name,
				Hostname:       []string{"port-validate-host"},
				TailnetName:    "example.ts.net",
				ProxyRules: []tailscalev1alpha1.ProxyRule{
					{Protocol: "tcp", ListenPort: 443, BackendPort: 9090, Funnel: true}, // 9090 missing on service
				},
			},
		}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())

		r := &TSIngressReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}

		// First reconcile adds finalizer
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())
		// Second reconcile should error due to invalid backend port
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).To(HaveOccurred())
	})

	It("cleans up resources and removes finalizer on delete", func() {
		name := types.NamespacedName{Name: "delete-test", Namespace: "default"}

		svc := makeService("backend-svc-del", name.Namespace, 8080)
		Expect(k8sClient.Create(ctx, svc)).To(Succeed())

		// Stub external calls
		originalMint := mintAuthKeyFn
		originalGetAll := getAllDevicesFn
		originalDelete := deleteDeviceFn
		originalDeleteDNS := deleteDNSFn
		originalUpdateDNS := updateDNSFn
		var deletedIDs []string
		var deleteDNSCalled bool

		mintAuthKeyFn = func(ctx context.Context, _ *tailscale.TokenCache, _ string, _ string) (string, error) {
			return "auth", nil
		}
		getAllDevicesFn = func(ctx context.Context, _ *tailscale.TokenCache, _ string) ([]tailscale.Device, error) {
			return []tailscale.Device{{NodeID: "node-1", Hostname: "delete-host"}}, nil
		}
		deleteDeviceFn = func(ctx context.Context, _ *tailscale.TokenCache, deviceID string) error {
			deletedIDs = append(deletedIDs, deviceID)
			return nil
		}
		deleteDNSFn = func(ctx context.Context, _, _, _, _ string) error {
			deleteDNSCalled = true
			return nil
		}
		updateDNSFn = func(ctx context.Context, _, _, _, _ string) error { return nil }
		defer func() {
			mintAuthKeyFn = originalMint
			getAllDevicesFn = originalGetAll
			deleteDeviceFn = originalDelete
			deleteDNSFn = originalDeleteDNS
			updateDNSFn = originalUpdateDNS
		}()

		cr := &tailscalev1alpha1.TSIngress{
			ObjectMeta: metav1.ObjectMeta{Name: name.Name, Namespace: name.Namespace},
			Spec: tailscalev1alpha1.TSIngressSpec{
				BackendService: svc.Name,
				Hostname:       []string{"delete-host"},
				TailnetName:    "example.ts.net",
				UpdateDNS:      true,
				ProxyRules:     []tailscalev1alpha1.ProxyRule{{Protocol: "tcp", ListenPort: 443, BackendPort: 8080, Funnel: true}},
			},
		}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())

		r := &TSIngressReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}

		// First to add finalizer, second to create resources
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		// Delete CR to trigger finalizer
		var fetched tailscalev1alpha1.TSIngress
		Expect(k8sClient.Get(ctx, name, &fetched)).To(Succeed())
		Expect(k8sClient.Delete(ctx, &fetched)).To(Succeed())

		// Run reconcile on deletion path (may require more than once)
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())
		// Trigger another reconcile to let deletion settle
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: name})
		Expect(err).NotTo(HaveOccurred())

		// External cleanup expected
		Expect(deletedIDs).To(ContainElement("node-1"))
		Expect(deleteDNSCalled).To(BeTrue())

		// CR should eventually be deleted or at least have the finalizer removed
		Eventually(func() bool {
			var obj tailscalev1alpha1.TSIngress
			err := k8sClient.Get(ctx, name, &obj)
			if errors.IsNotFound(err) {
				return true
			}
			if err != nil {
				return false
			}
			return !containsString(obj.Finalizers, "tailscale.tailgate.run/finalizer")
		}, 15*time.Second, 200*time.Millisecond).Should(BeTrue())
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
