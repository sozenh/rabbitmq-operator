package controllers

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"

	rabbitmqv1beta1 "github.com/rabbitmq/cluster-operator/v2/api/v1beta1"
)

// reconcileOperatorDefaults updates current rabbitmqCluster with operator defaults from the Reconciler
// it handles RabbitMQ image, imagePullSecrets, and user updater image
func (r *RabbitmqClusterReconciler) reconcileOperatorDefaults(ctx context.Context, rabbitmqCluster *rabbitmqv1beta1.RabbitmqCluster) (time.Duration, error) {
	updateType := ""
	// image will be updated image isn't set yet or the image controlled by the operator (experimental).
	if r.ControlRabbitmqImage ||
		rabbitmqCluster.Spec.Image == "" {
		updateType += " image "
		rabbitmqCluster.Spec.Image = r.DefaultRabbitmqImage
	}

	if rabbitmqCluster.VaultEnabled() {
		if r.ControlRabbitmqImage ||
			rabbitmqCluster.Spec.SecretBackend.Vault.DefaultUserUpdaterImage == nil {
			updateType += " vaultDefaultUserUpdaterImage "
			rabbitmqCluster.Spec.SecretBackend.Vault.DefaultUserUpdaterImage = &r.DefaultUserUpdaterImage
		}
	}

	if rabbitmqCluster.Spec.ImagePullSecrets == nil {
		updateType += " imagePullSecret "
		// split the comma separated list of default image pull secrets from
		// the 'DEFAULT_IMAGE_PULL_SECRETS' env var, but ignore empty strings.
		for _, reference := range strings.Split(r.DefaultImagePullSecrets, ",") {
			if len(reference) > 0 {
				rabbitmqCluster.Spec.ImagePullSecrets = append(rabbitmqCluster.Spec.ImagePullSecrets, corev1.LocalObjectReference{Name: reference})
			}
		}
	}

	if updateType == "" {
		return 0, nil
	}
	return r.updateRabbitmqCluster(ctx, rabbitmqCluster, updateType)
}

// updateRabbitmqCluster updates a RabbitmqCluster with the given definition
// it returns a 2 seconds requeue request if update failed due to conflict error
func (r *RabbitmqClusterReconciler) updateRabbitmqCluster(ctx context.Context, rabbitmqCluster *rabbitmqv1beta1.RabbitmqCluster, updateType string) (time.Duration, error) {
	logger := ctrl.LoggerFrom(ctx)
	if err := r.Update(ctx, rabbitmqCluster); err != nil {
		if k8serrors.IsConflict(err) {
			logger.Info(
				fmt.Sprintf("failed to update %s because of conflict; requeueing...", updateType),
				"namespace", rabbitmqCluster.Namespace, "name", rabbitmqCluster.Name)
			return 2 * time.Second, nil
		}
		return 0, err
	}
	return 0, nil
}
