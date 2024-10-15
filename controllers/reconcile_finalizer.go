package controllers

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	clientretry "k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	rabbitmqv1beta1 "github.com/rabbitmq/cluster-operator/v2/api/v1beta1"
	"github.com/rabbitmq/cluster-operator/v2/controllers/result"
	"github.com/rabbitmq/cluster-operator/v2/internal/resource"
)

const deletionFinalizer = "deletion.finalizers.rabbitmqclusters.rabbitmq.com"

// addFinalizer adds a deletion finalizer if the RabbitmqCluster does not have one yet and is not marked for deletion
func (r *RabbitmqClusterReconciler) addFinalizer(
	ctx context.Context, rmq *rabbitmqv1beta1.RabbitmqCluster) result.ReconcileResult {
	if !rmq.DeletionTimestamp.IsZero() {
		return result.Continue()
	}

	logger := ctrl.LoggerFrom(ctx)
	update := controllerutil.AddFinalizer(rmq, deletionFinalizer)

	if !update {
		return result.Continue()
	}
	logger.Info("add finalizer for deletion")
	err := r.Client.Update(ctx, rmq)
	if err == nil {
		return result.Done()
	}

	logger.Error(err, "failed to add finalizer for deletion")
	return result.Error(err)
}

func (r *RabbitmqClusterReconciler) removeFinalizer(
	ctx context.Context, rmq *rabbitmqv1beta1.RabbitmqCluster) result.ReconcileResult {
	logger := ctrl.LoggerFrom(ctx)
	update := controllerutil.RemoveFinalizer(rmq, deletionFinalizer)

	if !update {
		return result.Continue()
	}
	logger.Info("remove finalizer for deletion")
	err := r.Client.Update(ctx, rmq)
	if err == nil {
		return result.Done()
	}

	logger.Error(err, "failed to remove finalizer for deletion")
	return result.Error(err)
}

func (r *RabbitmqClusterReconciler) reconcileDelete(
	ctx context.Context, rmq *rabbitmqv1beta1.RabbitmqCluster) result.ReconcileResult {
	logger := ctrl.LoggerFrom(ctx)

	if rmq.DeletionTimestamp.IsZero() {
		return r.addFinalizer(ctx, rmq)
	}

	logger.Info("deleting")
	statefulsets, err := r.statefulSets(ctx, rmq)
	if err != nil {
		return result.Error(fmt.Errorf("list statefulsets failed: %v", err))
	}

	for index := range statefulsets {
		sts := &statefulsets[index]
		err = clientretry.RetryOnConflict(clientretry.DefaultRetry, func() error {
			uid, err := r.statefulSetUID(ctx, sts, rmq)

			// Add label on all Pods to be picked up in pre-stop hook via Downward API
			if err == nil {
				err = r.addRabbitmqDeletionLabel(ctx, sts)
			}
			// Delete StatefulSet immediately after changing pod labels to minimize risk of them respawning.
			// There is a window where the StatefulSet could respawn Pods without the deletion label in this order.
			// But we can't delete it before because the DownwardAPI doesn't update once a Pod enters Terminating.
			// Addressing #648: if both rabbitmqCluster and the statefulSet returned by r.Get() are stale (and match each other),
			// setting the stale statefulSet's uid in the precondition can avoid mis-deleting any currently running statefulSet sharing the same name.
			if err == nil {
				err = client.IgnoreNotFound(r.Client.Delete(ctx, sts, client.Preconditions{UID: &uid}))
			}

			return err
		})
		if err != nil {
			ctrl.LoggerFrom(ctx).Error(err, "RabbitmqCluster deletion")
		}
	}

	return r.removeFinalizer(ctx, rmq)
}

func (r *RabbitmqClusterReconciler) addRabbitmqDeletionLabel(ctx context.Context, sts *appsv1.StatefulSet) error {
	pods := &corev1.PodList{}
	if err := r.Client.List(
		ctx, pods,
		client.MatchingLabels(sts.Spec.Selector.MatchLabels)); err != nil {
		return fmt.Errorf("list pods for sts %s failed: %w", sts.Name, err)
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
