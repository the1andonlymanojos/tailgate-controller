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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	tailscalev1alpha1 "github.com/the1andonlymanojos/tailgate-controller/api/v1alpha1"
	"github.com/the1andonlymanojos/tailgate-controller/internal/cloudflare"
	"github.com/the1andonlymanojos/tailgate-controller/internal/tailscale"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// Function variables to allow stubbing in tests
var (
	mintAuthKeyFn   = tailscale.MintAuthKey
	getAllDevicesFn = tailscale.GetAllDevices
	deleteDeviceFn  = tailscale.DeleteDevice
	updateDNSFn     = cloudflare.UpdateDNS
	deleteDNSFn     = cloudflare.DeletDNS
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
	logger := logf.FromContext(ctx).WithValues("controller", "tsingress", "tsingress", req.NamespacedName)
	logger.Info("reconcile start")

	// 1. Fetch the TSIngress resource instance
	var tsIngress tailscalev1alpha1.TSIngress
	err := r.Get(ctx, req.NamespacedName, &tsIngress)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			logger.Info("resource deleted (not found)")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Validate the TSIngress spec
	if err := tsIngress.Spec.Validate(); err != nil {
		logger.Error(err, "Invalid TSIngress spec")
		r.setStatus(ctx, &tsIngress, "Degraded", metav1.ConditionFalse, "SpecInvalid", err.Error())
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
			logger.Info("finalizer added", "finalizer", finalizerName)
			return ctrl.Result{Requeue: true}, nil
		}
	} else {
		// Object IS being deleted
		if containsString(tsIngress.Finalizers, finalizerName) {
			// Mark deleting state
			r.setStatus(ctx, &tsIngress, "Deleting", metav1.ConditionFalse, "Deleting", "Object is being deleted")

			// TODO: Cleanup logic here — delete pods, devices, auth keys, etc.
			logger.Info("cleanup on delete", "tailnet", tsIngress.Spec.TailnetName)

			//delete the device from the tailnet
			devices, err := getAllDevicesFn(ctx, r.TokenCache, tsIngress.Spec.TailnetName)

			logger.Info("Devices", "devices", devices)

			if err != nil {
				logger.Error(err, "Failed to get devices")
				return ctrl.Result{}, err
			}
			for _, device := range devices {
				fmt.Println(device.Hostname)
				if device.Hostname == tsIngress.Spec.Hostname[0] {
					//delete the device
					err = deleteDeviceFn(ctx, r.TokenCache, device.NodeID)
					if err != nil {
						logger.Error(err, "Failed to delete device")
						return ctrl.Result{}, err
					} else {
						logger.Info("Deleted device", "name", device.Hostname)
					}
				}
			}
			proxyDeploymentName := tsIngress.Name + "-proxy"

			// Check and delete configmap
			configMap := &corev1.ConfigMap{}
			err = r.Get(ctx, types.NamespacedName{
				Name:      proxyDeploymentName + "-config",
				Namespace: tsIngress.Namespace,
			}, configMap)
			if err == nil {
				if err := r.Delete(ctx, configMap); err != nil {
					logger.Error(err, "Failed to delete configmap")
					r.setStatus(ctx, &tsIngress, "Degraded", metav1.ConditionFalse, "DeleteFailed", "Failed to delete ConfigMap")
					return ctrl.Result{}, err
				}
				logger.Info("deleted configmap", "name", proxyDeploymentName+"-config")
			} else if !apierrors.IsNotFound(err) {
				logger.Error(err, "Failed to get configmap")
				return ctrl.Result{}, err
			}

			// Check and delete PVC
			pvc := &corev1.PersistentVolumeClaim{}
			err = r.Get(ctx, types.NamespacedName{
				Name:      proxyDeploymentName + "-state",
				Namespace: tsIngress.Namespace,
			}, pvc)
			if err == nil {
				if err := r.Delete(ctx, pvc); err != nil {
					logger.Error(err, "Failed to delete pvc")
					r.setStatus(ctx, &tsIngress, "Degraded", metav1.ConditionFalse, "DeleteFailed", "Failed to delete PVC")
					return ctrl.Result{}, err
				}
				logger.Info("deleted pvc", "name", proxyDeploymentName+"-state")
			} else if !apierrors.IsNotFound(err) {
				logger.Error(err, "Failed to get pvc")
				return ctrl.Result{}, err
			}

			// Check and delete deployment
			deployment := &appsv1.Deployment{}
			err = r.Get(ctx, types.NamespacedName{
				Name:      proxyDeploymentName,
				Namespace: tsIngress.Namespace,
			}, deployment)
			if err == nil {
				if err := r.Delete(ctx, deployment); err != nil {
					logger.Error(err, "Failed to delete deployment")
					r.setStatus(ctx, &tsIngress, "Degraded", metav1.ConditionFalse, "DeleteFailed", "Failed to delete Deployment")
					return ctrl.Result{}, err
				}
				logger.Info("deleted deployment", "name", proxyDeploymentName)
			} else if !apierrors.IsNotFound(err) {
				logger.Error(err, "Failed to get deployment")
				return ctrl.Result{}, err
			}

			// Check and delete DNS if enabled
			if tsIngress.Spec.UpdateDNS {
				//delete dns
				err = deleteDNSFn(ctx, tsIngress.Spec.DNSName, tsIngress.Spec.Domain, tsIngress.Spec.Hostname[0], tsIngress.Spec.TailnetName)
				if err != nil {
					logger.Error(err, "Failed to delete dns")
					// Don't return error here as DNS deletion is not critical
				}
			}

			// After cleanup, remove finalizer and update
			tsIngress.Finalizers = removeString(tsIngress.Finalizers, finalizerName)
			if err := r.Update(ctx, &tsIngress); err != nil {
				return ctrl.Result{}, err
			}

			logger.Info("finalizer removed, deletion complete")
			return ctrl.Result{}, nil
		}
	}

	// 4. Handle create or update (idempotent)
	logger.Info("reconciling object")
	// Set progressing status early
	r.setStatus(ctx, &tsIngress, "Progressing", metav1.ConditionFalse, "Reconciling", "Reconciling resources")

	proxyDeploymentName := tsIngress.Name + "-proxy"
	labels := map[string]string{"app": proxyDeploymentName}

	// Fetch existing ConfigMap (to reuse auth key and detect config changes)
	existingCM := &corev1.ConfigMap{}
	cmKey := types.NamespacedName{Name: proxyDeploymentName + "-config", Namespace: tsIngress.Namespace}
	cmErr := r.Get(ctx, cmKey, existingCM)

	// Fetch existing Deployment to decide first-create vs update
	existingDeploy := &appsv1.Deployment{}
	depKey := types.NamespacedName{Name: proxyDeploymentName, Namespace: tsIngress.Namespace}
	depErr := r.Get(ctx, depKey, existingDeploy)

	// Ensure backend Service exists and validate ports
	backendService := &corev1.Service{}
	if err := r.Get(ctx, types.NamespacedName{Name: tsIngress.Spec.BackendService, Namespace: tsIngress.Namespace}, backendService); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Error(err, "backend service not found", "service", tsIngress.Spec.BackendService)
		}
		r.setStatus(ctx, &tsIngress, "Degraded", metav1.ConditionFalse, "BackendServiceMissing", err.Error())
		return ctrl.Result{}, err
	}
	for _, rule := range tsIngress.Spec.ProxyRules {
		if !containsPort(backendService.Spec.Ports, rule.BackendPort) {
			err := fmt.Errorf("backend port %d not found on service %s", rule.BackendPort, tsIngress.Spec.BackendService)
			logger.Error(err, "invalid backend port")
			r.setStatus(ctx, &tsIngress, "Degraded", metav1.ConditionFalse, "InvalidBackendPort", err.Error())
			return ctrl.Result{}, err
		}
	}

	// Determine auth key: reuse from existing config if present, otherwise mint
	existingAuthKey := ""
	existingHostname := ""
	if cmErr == nil {
		if data, ok := existingCM.Data["tailgate.yaml"]; ok {
			existingAuthKey = extractYAMLScalar(data, "auth_key")
			existingHostname = extractYAMLScalar(data, "hostname")
		}
	}
	desiredHostname := tsIngress.Spec.Hostname[0]
	hostnameChanged := existingHostname != "" && existingHostname != desiredHostname
	var authKey string
	if existingAuthKey != "" && !hostnameChanged {
		authKey = existingAuthKey
	} else {
		// First-time mint OR hostname change (mint a fresh key)
		minted, mErr := mintAuthKeyFn(ctx, r.TokenCache, tsIngress.Spec.TailnetName, desiredHostname)
		if mErr != nil {
			logger.Error(mErr, "failed to mint auth key")
			r.setStatus(ctx, &tsIngress, "Degraded", metav1.ConditionFalse, "AuthKeyMintFailed", mErr.Error())
			return ctrl.Result{}, mErr
		}
		authKey = minted
	}

	// If first create OR hostname changed, ensure no conflicting device exists
	if apierrors.IsNotFound(depErr) || hostnameChanged {
		devices, derr := getAllDevicesFn(ctx, r.TokenCache, tsIngress.Spec.TailnetName)
		if derr != nil {
			logger.Error(derr, "failed to get devices")
			r.setStatus(ctx, &tsIngress, "Degraded", metav1.ConditionFalse, "DeviceListFailed", derr.Error())
			return ctrl.Result{}, derr
		}
		for _, device := range devices {
			if device.Hostname == desiredHostname {
				if err := deleteDeviceFn(ctx, r.TokenCache, device.NodeID); err != nil {
					logger.Error(err, "failed to delete conflicting device", "hostname", desiredHostname)
					r.setStatus(ctx, &tsIngress, "Degraded", metav1.ConditionFalse, "DeviceDeleteFailed", err.Error())
					return ctrl.Result{}, err
				}
			}
		}
	}

	// Generate desired config
	configData, err := GenerateConfigFromTSIngress(tsIngress, authKey)
	if err != nil {
		logger.Error(err, "failed to generate config")
		r.setStatus(ctx, &tsIngress, "Degraded", metav1.ConditionFalse, "ConfigRenderFailed", err.Error())
		return ctrl.Result{}, err
	}

	// Create or Update ConfigMap
	desiredCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmKey.Name,
			Namespace: cmKey.Namespace,
		},
		Data: map[string]string{"tailgate.yaml": configData},
	}
	if err := controllerutil.SetControllerReference(&tsIngress, desiredCM, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	if cmErr != nil && apierrors.IsNotFound(cmErr) {
		if err := r.Create(ctx, desiredCM); err != nil {
			r.setStatus(ctx, &tsIngress, "Degraded", metav1.ConditionFalse, "ConfigMapCreateFailed", err.Error())
			return ctrl.Result{}, err
		}
	} else if cmErr == nil {
		// Update only if changed
		if existingCM.Data == nil || existingCM.Data["tailgate.yaml"] != configData {
			existingCM.Data = map[string]string{"tailgate.yaml": configData}
			if err := r.Update(ctx, existingCM); err != nil {
				r.setStatus(ctx, &tsIngress, "Degraded", metav1.ConditionFalse, "ConfigMapUpdateFailed", err.Error())
				return ctrl.Result{}, err
			}
		}
	} else {
		r.setStatus(ctx, &tsIngress, "Degraded", metav1.ConditionFalse, "ConfigMapFetchFailed", cmErr.Error())
		return ctrl.Result{}, cmErr
	}

	// Ensure PVC exists (create if missing)
	existingPVC := &corev1.PersistentVolumeClaim{}
	pvcKey := types.NamespacedName{Name: proxyDeploymentName + "-state", Namespace: tsIngress.Namespace}
	pvcErr := r.Get(ctx, pvcKey, existingPVC)
	if pvcErr != nil && apierrors.IsNotFound(pvcErr) {
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pvcKey.Name,
				Namespace: pvcKey.Namespace,
				Labels:    labels,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("100Mi")},
				},
			},
		}
		if err := controllerutil.SetControllerReference(&tsIngress, pvc, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, pvc); err != nil {
			r.setStatus(ctx, &tsIngress, "Degraded", metav1.ConditionFalse, "PVCCreateFailed", err.Error())
			return ctrl.Result{}, err
		}
	} else if pvcErr != nil {
		r.setStatus(ctx, &tsIngress, "Degraded", metav1.ConditionFalse, "PVCFetchFailed", pvcErr.Error())
		return ctrl.Result{}, pvcErr
	}

	// Create or Update Deployment; roll on config changes via checksum annotation
	configChecksum := sha256Hex(configData)
	desiredDeploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      depKey.Name,
			Namespace: depKey.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptrToInt32(1),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
					Annotations: map[string]string{
						"tailscale.tailgate.run/config-checksum": configChecksum,
					},
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: cmKey.Name}},
							},
						},
						{
							Name:         "state",
							VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvcKey.Name}},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "proxy",
							Image: "docker.io/manojthedonut/tailgate:0.1.1",
							VolumeMounts: []corev1.VolumeMount{
								{Name: "config", MountPath: "/app/tailgate.yaml", SubPath: "tailgate.yaml"},
								{Name: "state", MountPath: "/var/lib/tailgate"},
							},
						},
					},
				},
			},
		},
	}
	if err := controllerutil.SetControllerReference(&tsIngress, desiredDeploy, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	if depErr != nil && apierrors.IsNotFound(depErr) {
		if err := r.Create(ctx, desiredDeploy); err != nil {
			r.setStatus(ctx, &tsIngress, "Degraded", metav1.ConditionFalse, "DeploymentCreateFailed", err.Error())
			return ctrl.Result{}, err
		}
	} else if depErr == nil {
		// Update existing deployment spec and checksum annotation to trigger rollout when config changes
		existingDeploy.Spec = desiredDeploy.Spec
		// Preserve existing labels on metadata
		if existingDeploy.Labels == nil {
			existingDeploy.Labels = map[string]string{}
		}
		for k, v := range labels {
			existingDeploy.Labels[k] = v
		}
		if err := r.Update(ctx, existingDeploy); err != nil {
			r.setStatus(ctx, &tsIngress, "Degraded", metav1.ConditionFalse, "DeploymentUpdateFailed", err.Error())
			return ctrl.Result{}, err
		}
	} else {
		r.setStatus(ctx, &tsIngress, "Degraded", metav1.ConditionFalse, "DeploymentFetchFailed", depErr.Error())
		return ctrl.Result{}, depErr
	}

	// DNS update: only when enabled and inputs changed (tracked via annotation on CR)
	if tsIngress.Spec.UpdateDNS {
		dnsSig := fmt.Sprintf("%s|%s|%s|%s", tsIngress.Spec.DNSName, tsIngress.Spec.Domain, desiredHostname, tsIngress.Spec.TailnetName)
		dnsHash := sha256Hex(dnsSig)
		if tsIngress.Annotations == nil {
			tsIngress.Annotations = map[string]string{}
		}
		if tsIngress.Annotations["tailscale.tailgate.run/dns-hash"] != dnsHash {
			if err := updateDNSFn(ctx, tsIngress.Spec.DNSName, tsIngress.Spec.Domain, desiredHostname, tsIngress.Spec.TailnetName); err != nil {
				logger.Error(err, "failed to update dns")
				r.setStatus(ctx, &tsIngress, "Degraded", metav1.ConditionFalse, "DNSUpdateFailed", err.Error())
				return ctrl.Result{}, err
			}
			tsIngress.Annotations["tailscale.tailgate.run/dns-hash"] = dnsHash
			if err := r.Update(ctx, &tsIngress); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	// Status and conditions
	setCondition := func(condType string, status metav1.ConditionStatus, reason, message string) {
		meta.SetStatusCondition(&tsIngress.Status.Conditions, metav1.Condition{
			Type:               condType,
			Status:             status,
			Reason:             reason,
			Message:            message,
			ObservedGeneration: tsIngress.GetGeneration(),
		})
	}
	// Use a status patch to avoid conflicts
	statusBase := tsIngress.DeepCopy()
	tsIngress.Status.Initialized = true
	tsIngress.Status.State = "Ready"
	setCondition("Ready", metav1.ConditionTrue, "ReconcileSuccess", "Resources are in desired state")
	if err := r.Status().Patch(ctx, &tsIngress, client.MergeFrom(statusBase)); err != nil {
		logger.Error(err, "failed to update status")
		// Do not fail reconciliation solely on status update failure
	}

	logger.Info("reconcile success")
	return ctrl.Result{}, nil
}

// if initialised, check if deployment is healthy, PVC is fine, basically everything else.

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
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Named("tsingress").
		Complete(r)
}

func ptrToInt32(i int32) *int32 {
	return &i
}

func GenerateConfigFromTSIngress(tsIngress tailscalev1alpha1.TSIngress, authKey string) (string, error) {
	hostname := tsIngress.Spec.Hostname[0]
	backendAddr := fmt.Sprintf("%s.%s.svc.cluster.local", tsIngress.Spec.BackendService, tsIngress.Namespace)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("tailscale:\n  auth_key: \"%s\"\n  hostname: \"%s\"\n  state_dir: \"/var/lib/tailgate\"\n\nproxies:\n", authKey, hostname))

	for _, rule := range tsIngress.Spec.ProxyRules {
		protocol := rule.Protocol
		if protocol == "" {
			protocol = "tcp"
		}
		sb.WriteString(fmt.Sprintf("- protocol: \"%s\"\n  listen_addr: \":%d\"\n  backend_addr: \"%s:%d\"\n  funnel: %v\n",
			protocol, rule.ListenPort, backendAddr, rule.BackendPort, rule.Funnel))
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

// extractYAMLScalar extracts a simple quoted scalar value (e.g. key: "value") from a minimal YAML string.
// This is a lightweight helper to avoid pulling a YAML parser for small config updates.
func extractYAMLScalar(yaml string, key string) string {
	// Match lines like: key: "value"
	prefix := key + ":"
	for _, line := range strings.Split(yaml, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			rest := strings.TrimSpace(strings.TrimPrefix(line, prefix))
			// Remove surrounding quotes if present
			rest = strings.Trim(rest, "\"")
			return rest
		}
	}
	return ""
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// setStatus updates status.State and the Ready condition defensively.
func (r *TSIngressReconciler) setStatus(ctx context.Context, obj *tailscalev1alpha1.TSIngress, state string, condStatus metav1.ConditionStatus, reason, message string) {
	obj.Status.State = state
	if state == "Ready" {
		obj.Status.Initialized = true
	}
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             condStatus,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: obj.GetGeneration(),
	})
	_ = r.Status().Update(ctx, obj)
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
