package controllers

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rabbitmqv1beta1 "github.com/rabbitmq/cluster-operator/v2/api/v1beta1"
	"github.com/rabbitmq/cluster-operator/v2/controllers/result"
	"github.com/rabbitmq/cluster-operator/v2/internal/constant"
	"github.com/rabbitmq/cluster-operator/v2/internal/scaling"
	"github.com/rabbitmq/cluster-operator/v2/internal/status"
)

func (r *RabbitmqClusterReconciler) reconcilePVC(
	ctx context.Context, rmq *rabbitmqv1beta1.RabbitmqCluster, desiredSts *appsv1.StatefulSet) result.ReconcileResult {
	logger := ctrl.LoggerFrom(ctx)

	desiredCapacity := persistenceStorageCapacity(
		constant.PVCName, desiredSts.Spec.VolumeClaimTemplates)

	err := scaling.
		NewPersistenceScaler(r.Clientset).
		Scale(ctx, *rmq, constant.PVCName, client.ObjectKeyFromObject(desiredSts), desiredCapacity)
	if err == nil {
		return result.Continue()
	}

	msg := fmt.Sprintf("failed to scale PVCs: %s", err.Error())
	logger.Error(fmt.Errorf("hit an error while scaling PVC capacity: %w", err), msg)
	r.Recorder.Event(rmq, corev1.EventTypeWarning, "FailedReconcilePersistence", msg)
	status.SetCondition(&rmq.Status, rabbitmqv1beta1.ReconcileSuccess, corev1.ConditionFalse, "FailedReconcilePVC", err.Error())
	return result.Error(err)
}
