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
	"os"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	tailscalev1alpha1 "github.com/the1andonlymanojos/tailgate-controller/api/v1alpha1"
	"github.com/the1andonlymanojos/tailgate-controller/internal/cloudflare"
	"github.com/the1andonlymanojos/tailgate-controller/internal/tailscale"
)

// TSIngressReconciler reconciles a TSIngress object
type TSIngressReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	TokenCache *tailscale.TokenCache
}

// +kubebuilder:rbac:groups=tailscale.tailgate.run,resources=tsingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tailscale.tailgate.run,resources=tsingresses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tailscale.tailgate.run,resources=tsingresses/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the TSIngress object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *TSIngressReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	logger := logf.FromContext(ctx)

	logger.Info("Function called")

	// 1. Fetch the TSIngress resource instance
	var tsIngress tailscalev1alpha1.TSIngress
	err := r.Get(ctx, req.NamespacedName, &tsIngress)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			// 2. CR not found => it was deleted, cleanup already done by finalizer logic
			logger.Info("TSIngress resource deleted", "name", req.NamespacedName)
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	// 3. FINALIZER MANAGEMENT
	finalizerName := "tailscale.tailgate.run/finalizer"

	// Check if object is marked for deletion
	if tsIngress.ObjectMeta.DeletionTimestamp.IsZero() {
		// Object NOT being deleted, ensure finalizer present
		if !containsString(tsIngress.Finalizers, finalizerName) {
			tsIngress.Finalizers = append(tsIngress.Finalizers, finalizerName)
			if err := r.Update(ctx, &tsIngress); err != nil {
				return ctrl.Result{}, err
			}
			// Return to requeue with finalizer added
			return ctrl.Result{Requeue: true}, nil
		}
	} else {
		// Object IS being deleted
		if containsString(tsIngress.Finalizers, finalizerName) {

			// TODO: Cleanup logic here — delete pods, devices, auth keys, etc.
			logger.Info("Running cleanup before deleting TSIngress", "name", tsIngress.Name)

			logger.Info("Tailnet name", "tailnet", tsIngress.Spec.TailnetName)

			//delete the device from the tailnet
			devices, err := tailscale.GetAllDevices(ctx, r.TokenCache, tsIngress.Spec.TailnetName)

			logger.Info("Devices", "devices", devices)

			if err != nil {
				logger.Error(err, "Failed to get devices")
				return ctrl.Result{}, err
			}
			for _, device := range devices {
				fmt.Println(device.Hostname)
				if device.Hostname == tsIngress.Spec.Hostname[0] {
					//delete the device
					err = tailscale.DeleteDevice(ctx, r.TokenCache, device.NodeID)
					if err != nil {
						logger.Error(err, "Failed to delete device")
						return ctrl.Result{}, err
					} else {
						logger.Info("Deleted device", "name", device.Hostname)
					}
				}
			}
			proxyDeploymentName := tsIngress.Name + "-proxy"

			//remove configmap, pvc, deployment
			err = r.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      proxyDeploymentName + "-config",
					Namespace: tsIngress.Namespace,
				},
			})

			if err != nil {
				logger.Error(err, "Failed to delete configmap")
				return ctrl.Result{}, err
			} else {
				logger.Info("Deleted configmap", "name", proxyDeploymentName+"-config")
			}

			err = r.Delete(ctx, &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      proxyDeploymentName + "-state",
					Namespace: tsIngress.Namespace,
				},
			})

			if err != nil {
				logger.Error(err, "Failed to delete pvc")
				return ctrl.Result{}, err
			} else {
				logger.Info("Deleted pvc", "name", proxyDeploymentName+"-state")
			}

			err = r.Delete(ctx, &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      proxyDeploymentName,
					Namespace: tsIngress.Namespace,
				},
			})

			if err != nil {
				logger.Error(err, "Failed to delete deployment")
				return ctrl.Result{}, err
			} else {
				logger.Info("Deleted deployment", "name", proxyDeploymentName)
			}

			//check dns flag
			if tsIngress.Spec.UpdateDNS {
				//delete dns
				err = cloudflare.DeletDNS(ctx, tsIngress.Spec.DNSName, tsIngress.Spec.Domain, tsIngress.Spec.Hostname[0], tsIngress.Spec.TailnetName)
				if err != nil {
					logger.Error(err, "Failed to delete dns")
				}
			}

			// After cleanup, remove finalizer and update
			tsIngress.Finalizers = removeString(tsIngress.Finalizers, finalizerName)
			if err := r.Update(ctx, &tsIngress); err != nil {
				return ctrl.Result{}, err
			}

			// Stop reconciliation as object is being deleted
			return ctrl.Result{}, nil
		}
	}

	// 4. Handle create or update
	logger.Info("Reconciling TSIngress", "name", tsIngress.Name)

	proxyDeploymentName := tsIngress.Name + "-proxy"

	var deploy appsv1.Deployment

	err = r.Get(ctx, types.NamespacedName{Name: proxyDeploymentName, Namespace: tsIngress.Namespace}, &deploy)

	if err != nil && apierrors.IsNotFound(err) {
		logger.Info("Proxy deployment not found, creating", "name", proxyDeploymentName)

		//check if tsIngress.Spec.Hostname[0] is already in the tailnet
		devices, err := tailscale.GetAllDevices(ctx, r.TokenCache, tsIngress.Spec.TailnetName)
		if err != nil {
			logger.Error(err, "Failed to get devices")
			return ctrl.Result{}, err
		}
		for _, device := range devices {
			fmt.Println(device.Hostname)
			fmt.Println(tsIngress.Spec.Hostname[0])
			if device.Hostname == tsIngress.Spec.Hostname[0] {
				//delete the device
				err = tailscale.DeleteDevice(ctx, r.TokenCache, device.NodeID)
				if err != nil {
					logger.Error(err, "Failed to delete device")
					return ctrl.Result{}, err
				}
			}
		}

		labels := map[string]string{"app": proxyDeploymentName}

		//delete old configmap if exists
		configMap := &corev1.ConfigMap{}
		err = r.Get(ctx, types.NamespacedName{Name: proxyDeploymentName + "-config", Namespace: tsIngress.Namespace}, configMap)
		if err == nil {
			if err := r.Delete(ctx, configMap); err != nil {
				logger.Error(err, "Failed to delete configmap")
				return ctrl.Result{}, err
			}
		}

		authKey, err := tailscale.MintAuthKey(ctx, r.TokenCache, "stoat-toad.ts.net", tsIngress.Spec.Hostname[0])
		if err != nil {
			logger.Error(err, "Failed to mint auth key")
			return ctrl.Result{}, err
		}
		configData, err := GenerateConfigFromTSIngress(tsIngress, authKey)
		if err != nil {
			logger.Error(err, "Failed to generate config")
			return ctrl.Result{}, err
		}

		//check if services exist
		backendService := &corev1.Service{}
		err = r.Get(ctx, types.NamespacedName{Name: tsIngress.Spec.BackendService, Namespace: tsIngress.Namespace}, backendService)
		if err != nil {
			logger.Error(err, "Failed to get backend service")
			return ctrl.Result{}, err
		}
		//validate ports
		if len(tsIngress.Spec.Ports) == 0 {
			logger.Error(err, "No ports specified")
			return ctrl.Result{}, err
		}
		if len(tsIngress.Spec.ListenPorts) == 0 {
			logger.Error(err, "No listen ports specified")
		}
		if len(tsIngress.Spec.ListenPorts) != len(tsIngress.Spec.Ports) {
			logger.Error(err, "listenPorts and ports must have the same length")
			return ctrl.Result{}, err
		}

		//check ports on backend service
		if backendService.Spec.Ports == nil {
			logger.Error(err, "No ports specified on backend service")
			return ctrl.Result{}, err
		}
		for _, port := range tsIngress.Spec.Ports {
			if !containsPort(backendService.Spec.Ports, port) {
				logger.Error(err, "Port not found on backend service")
				return ctrl.Result{}, err
			}
		}

		configMap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      proxyDeploymentName + "-config",
				Namespace: tsIngress.Namespace,
			},
			Data: map[string]string{
				"tailgate.yaml": configData,
			},
		}

		// Set ownership
		if err := controllerutil.SetControllerReference(&tsIngress, configMap, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}

		if err := r.Create(ctx, configMap); err != nil {
			return ctrl.Result{}, err
		}
		existingPVC := &corev1.PersistentVolumeClaim{}
		err = r.Get(ctx, types.NamespacedName{
			Name:      proxyDeploymentName + "-state",
			Namespace: tsIngress.Namespace,
		}, existingPVC)
		logger.Info("Existing PVC", "existingPVC", existingPVC)
		if err != nil && apierrors.IsNotFound(err) {
			// Doesn't exist, so create it
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      proxyDeploymentName + "-state",
					Namespace: tsIngress.Namespace,
					Labels: map[string]string{
						"app": proxyDeploymentName,
					},
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("100Mi"),
						},
					},
				},
			}

			if err := controllerutil.SetControllerReference(&tsIngress, pvc, r.Scheme); err != nil {
				return ctrl.Result{}, err
			}

			if err := r.Create(ctx, pvc); err != nil {
				return ctrl.Result{}, err
			}
		} else if err != nil {
			// Real error
			return ctrl.Result{}, err
		}

		deploy = appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      proxyDeploymentName,
				Namespace: tsIngress.Namespace,
				Labels:    labels,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: ptrToInt32(1), // helper to return *int32(1)
				Selector: &metav1.LabelSelector{
					MatchLabels: labels,
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: labels,
					},
					Spec: corev1.PodSpec{
						Volumes: []corev1.Volume{
							{
								Name: "config",
								VolumeSource: corev1.VolumeSource{
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: proxyDeploymentName + "-config",
										},
									},
								},
							},
							{
								Name: "state",
								VolumeSource: corev1.VolumeSource{
									PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
										ClaimName: proxyDeploymentName + "-state",
									},
								},
							},
						},
						Containers: []corev1.Container{
							{
								Name:  "proxy",
								Image: "docker.io/manojthedonut/tailgate:0.1.1",
								VolumeMounts: []corev1.VolumeMount{
									{
										Name:      "config",
										MountPath: "/app/tailgate.yaml",
										SubPath:   "tailgate.yaml",
									},
									{
										Name:      "state",
										MountPath: "/var/lib/tailgate",
									},
								},
							},
						},
					},
				},
			},
		}
		// Set owner reference so deployment is deleted with TSIngress
		if err := controllerutil.SetControllerReference(&tsIngress, &deploy, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}

		if err := r.Create(ctx, &deploy); err != nil {
			return ctrl.Result{}, err
		}

		//check dns flag
		if tsIngress.Spec.UpdateDNS {
			//update dns
			fmt.Println("Updating dns")
			fmt.Println(os.Getenv("CLOUDFLARE_API_KEY"))
			fmt.Println(os.Getenv("CLOUDFLARE_ZONE_ID"))
			fmt.Println("args: ", tsIngress.Spec.DNSName, tsIngress.Spec.Domain, tsIngress.Spec.Hostname[0], tsIngress.Spec.TailnetName)
			err = cloudflare.UpdateDNS(ctx, tsIngress.Spec.DNSName, tsIngress.Spec.Domain, tsIngress.Spec.Hostname[0], tsIngress.Spec.TailnetName)
			if err != nil {
				logger.Error(err, "Failed to update dns")
				return ctrl.Result{}, err
			}
		}

		// Created successfully, requeue to check status later
		return ctrl.Result{Requeue: true, RequeueAfter: 10 * time.Second}, nil
	} else if err != nil {
		// Other errors on Get
		return ctrl.Result{}, err
	}

	// TODO: Add your core logic here:
	// - Check if pods are running and healthy
	// - Check tailscale device status
	// - Create or update any resources to match desired state

	// 5. Update status subresource if needed
	// e.g.,
	// tsIngress.Status.SomeCondition = "Ready"
	// err = r.Status().Update(ctx, &tsIngress)
	// if err != nil {
	//     return ctrl.Result{}, err
	// }

	// 6. Return and requeue if needed
	return ctrl.Result{}, nil
}

// Utility functions to check/remove strings from slice

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) []string {
	result := []string{}
	for _, item := range slice {
		if item != s {
			result = append(result, item)
		}
	}
	return result
}

// SetupWithManager sets up the controller with the Manager.
func (r *TSIngressReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tailscalev1alpha1.TSIngress{}).
		Named("tsingress").
		Complete(r)
}

func ptrToInt32(i int32) *int32 {
	return &i
}

func GenerateConfigFromTSIngress(tsIngress tailscalev1alpha1.TSIngress, authKey string) (string, error) {
	hostname := tsIngress.Spec.Hostname[0]

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("tailscale:\n  auth_key: \"%s\"\n  hostname: \"%s\"\n  state_dir: \"/var/lib/tailgate\"\n\nproxies:\n", authKey, hostname))

	// If Ports is empty, assume default 443
	ports := tsIngress.Spec.Ports
	listenPorts := tsIngress.Spec.ListenPorts
	if len(listenPorts) != len(ports) {
		return "", fmt.Errorf("listenPorts and ports must have the same length")
	}
	if len(ports) == 0 {
		ports = []int{443}
	}

	protocol := tsIngress.Spec.Protocol
	if protocol == "" {
		protocol = "tcp"
	}

	backendAddr := fmt.Sprintf("%s.%s.svc.cluster.local", tsIngress.Spec.BackendService, tsIngress.Namespace)

	for i, port := range listenPorts {
		sb.WriteString(fmt.Sprintf("- protocol: \"%s\"\n  listen_addr: \":%d\"\n  backend_addr: \"%s:%d\"\n  funnel: false\n", protocol, port, backendAddr, ports[i]))
	}

	return sb.String(), nil
}

func containsPort(ports []corev1.ServicePort, port int) bool {
	for _, p := range ports {
		if p.Port == int32(port) {
			return true
		}
	}
	return false
}

/*

tailscale:
  auth_key: "tskey-auth-k3LQLbbL1U11CNTRL-A4ZrTcryJtJrq47bJub7tJK9NBjJuPd7T"
  hostname: "my-proxy-2"
  state_dir: "/var/lib/tailgate"

proxies:
  - protocol: "tcp"
    listen_addr: ":443"
    backend_addr: "host.docker.internal:8080"
    funnel: true
  - protocol: "tcp"
    listen_addr: ":80"
    backend_addr: "host.docker.internal:8080"

*/
