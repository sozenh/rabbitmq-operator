package controllers

import (
	"context"
	"strings"

	corev1 "k8s.io/api/core/v1"
	clientretry "k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"

	rabbitmqv1beta1 "github.com/rabbitmq/cluster-operator/v2/api/v1beta1"
	"github.com/rabbitmq/cluster-operator/v2/controllers/result"
)

// reconcileOperatorDefaults updates current rabbitmqCluster with operator defaults from the Reconciler
// it handles RabbitMQ image, imagePullSecrets, and user updater image
func (r *RabbitmqClusterReconciler) reconcileOperatorDefaults(
	ctx context.Context,
	rabbitmqCluster *rabbitmqv1beta1.RabbitmqCluster) result.ReconcileResult {
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
		// split the comma separated list of default image pull secrets from
		// the 'DEFAULT_IMAGE_PULL_SECRETS' env var, but ignore empty strings.
		for _, reference := range strings.Split(r.DefaultImagePullSecrets, ",") {
			if len(reference) > 0 {
				updateType += " imagePullSecret "
				rabbitmqCluster.Spec.ImagePullSecrets = append(rabbitmqCluster.Spec.ImagePullSecrets, corev1.LocalObjectReference{Name: reference})
			}
		}
	}

	if updateType == "" {
		return result.Continue()
	}
	return r.updateRabbitmqCluster(ctx, rabbitmqCluster, updateType)
}

// updateRabbitmqCluster updates a RabbitmqCluster with the given definition
// it returns a 2 seconds requeue request if update failed due to conflict error
func (r *RabbitmqClusterReconciler) updateRabbitmqCluster(
	ctx context.Context, rmq *rabbitmqv1beta1.RabbitmqCluster, updateType string) result.ReconcileResult {
	logger := ctrl.LoggerFrom(ctx)

	logger.Info("update RabbitmqCluster", "updateType", updateType)
	if err := clientretry.RetryOnConflict(
		clientretry.DefaultRetry, func() error { return r.Update(ctx, rmq) },
	); err != nil {
		logger.Error(err, "failed to update RabbitmqCluster", "updateType", updateType)
		return result.Error(err)
	}

	return result.Done()
}
