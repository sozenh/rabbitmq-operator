package controllers

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	rabbitmqv1beta1 "github.com/rabbitmq/cluster-operator/v2/api/v1beta1"
	"github.com/rabbitmq/cluster-operator/v2/internal/constant"
	"github.com/rabbitmq/cluster-operator/v2/internal/scaling"
)

func (r *RabbitmqClusterReconciler) reconcilePVC(ctx context.Context, rmq *rabbitmqv1beta1.RabbitmqCluster, desiredSts *appsv1.StatefulSet) error {
	logger := ctrl.LoggerFrom(ctx)

	desiredCapacity := persistenceStorageCapacity(
		constant.PVCName, desiredSts.Spec.VolumeClaimTemplates)

	err := scaling.NewPersistenceScaler(r.Clientset).Scale(ctx, *rmq, desiredCapacity)
	if err != nil {
		msg := fmt.Sprintf("Failed to scale PVCs: %s", err.Error())
		logger.Error(fmt.Errorf("hit an error while scaling PVC capacity: %w", err), msg)
		r.Recorder.Event(rmq, corev1.EventTypeWarning, "FailedReconcilePersistence", msg)
	}
	return err
}
