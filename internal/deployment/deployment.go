package deployment

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateDeployment creates a new deployment with the given specifications
func CreateDeployment(ctx context.Context, c client.Client, name, namespace string, replicas int32, containerSpec corev1.Container) error {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{containerSpec},
				},
			},
		},
	}

	return c.Create(ctx, deployment)
}

// GetDeployment retrieves a deployment by name and namespace
func GetDeployment(ctx context.Context, c client.Client, name, namespace string) (*appsv1.Deployment, error) {
	deployment := &appsv1.Deployment{}
	err := c.Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}, deployment)
	if err != nil {
		return nil, err
	}
	return deployment, nil
}

// IsDeploymentReady checks if a deployment is ready (all replicas are available)
func IsDeploymentReady(deployment *appsv1.Deployment) bool {
	return deployment.Status.AvailableReplicas == *deployment.Spec.Replicas
}

// UpdateDeployment updates an existing deployment
func UpdateDeployment(ctx context.Context, c client.Client, deployment *appsv1.Deployment) error {
	return c.Update(ctx, deployment)
}

// DeleteDeployment deletes a deployment by name and namespace
func DeleteDeployment(ctx context.Context, c client.Client, name, namespace string) error {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	return c.Delete(ctx, deployment)
}

// EnsureDeployment ensures a deployment exists with the given specifications
func EnsureDeployment(ctx context.Context, c client.Client, name, namespace string, replicas int32, containerSpec corev1.Container) error {
	existing, err := GetDeployment(ctx, c, name, namespace)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Deployment doesn't exist, create it
			return CreateDeployment(ctx, c, name, namespace, replicas, containerSpec)
		}
		return err
	}

	// Update existing deployment if needed
	existing.Spec.Replicas = &replicas
	existing.Spec.Template.Spec.Containers = []corev1.Container{containerSpec}
	return UpdateDeployment(ctx, c, existing)
}
