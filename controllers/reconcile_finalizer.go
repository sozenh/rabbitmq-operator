package controllers

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientretry "k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	rabbitmqv1beta1 "github.com/rabbitmq/cluster-operator/v2/api/v1beta1"
	"github.com/rabbitmq/cluster-operator/v2/internal/constant"
	"github.com/rabbitmq/cluster-operator/v2/internal/metadata"
	"github.com/rabbitmq/cluster-operator/v2/internal/resource"
)

const deletionFinalizer = "deletion.finalizers.rabbitmqclusters.rabbitmq.com"

// addFinalizer adds a deletion finalizer if the RabbitmqCluster does not have one yet and is not marked for deletion
func (r *RabbitmqClusterReconciler) addFinalizer(ctx context.Context, rabbitmqCluster *rabbitmqv1beta1.RabbitmqCluster) error {
	if !rabbitmqCluster.DeletionTimestamp.IsZero() {
		return nil
	}

	update := controllerutil.AddFinalizer(rabbitmqCluster, deletionFinalizer)

	if !update {
		return nil
	}
	return r.Client.Update(ctx, rabbitmqCluster)
}

func (r *RabbitmqClusterReconciler) removeFinalizer(ctx context.Context, rabbitmqCluster *rabbitmqv1beta1.RabbitmqCluster) error {
	update := controllerutil.RemoveFinalizer(rabbitmqCluster, deletionFinalizer)

	if !update {
		return nil
	}
	return r.Client.Update(ctx, rabbitmqCluster)
}

func (r *RabbitmqClusterReconciler) prepareForDeletion(ctx context.Context, rabbitmqCluster *rabbitmqv1beta1.RabbitmqCluster) error {
	if !controllerutil.ContainsFinalizer(rabbitmqCluster, deletionFinalizer) {
		return nil
	}

	if err := clientretry.RetryOnConflict(clientretry.DefaultRetry, func() error {
		uid, err := r.statefulSetUID(ctx, rabbitmqCluster)
		if err != nil {
			return err
		}
		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rabbitmqCluster.ChildResourceName(constant.ResourceStatefulsetSuffix),
				Namespace: rabbitmqCluster.Namespace,
			},
		}
		// Add label on all Pods to be picked up in pre-stop hook via Downward API
		if err := r.addRabbitmqDeletionLabel(ctx, rabbitmqCluster); err != nil {
			return fmt.Errorf("failed to add deletion markers to RabbitmqCluster Pods: %w", err)
		}
		// Delete StatefulSet immediately after changing pod labels to minimize risk of them respawning.
		// There is a window where the StatefulSet could respawn Pods without the deletion label in this order.
		// But we can't delete it before because the DownwardAPI doesn't update once a Pod enters Terminating.
		// Addressing #648: if both rabbitmqCluster and the statefulSet returned by r.Get() are stale (and match each other),
		// setting the stale statefulSet's uid in the precondition can avoid mis-deleting any currently running statefulSet sharing the same name.
		if err := r.Client.Delete(ctx, sts, client.Preconditions{UID: &uid}); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("cannot delete StatefulSet: %w", err)
		}

		return nil
	}); err != nil {
		ctrl.LoggerFrom(ctx).Error(err, "RabbitmqCluster deletion")
	}

	if err := r.removeFinalizer(ctx, rabbitmqCluster); err != nil {
		ctrl.LoggerFrom(ctx).Error(err, "Failed to remove finalizer for deletion")
		return err
	}

	return nil
}

func (r *RabbitmqClusterReconciler) addRabbitmqDeletionLabel(ctx context.Context, rabbitmqCluster *rabbitmqv1beta1.RabbitmqCluster) error {
	pods := &corev1.PodList{}
	if err := r.Client.List(
		ctx, pods,
		client.MatchingLabels(metadata.LabelSelector(rabbitmqCluster.Name))); err != nil {
		return err
	}

	for i := 0; i < len(pods.Items); i++ {
		pod := &pods.Items[i]
		pod.Labels[resource.DeletionMarker] = "true"
		if err := r.Client.Update(ctx, pod); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("cannot Update Pod %s in Namespace %s: %w", pod.Name, pod.Namespace, err)
		}
	}

	return nil
}
