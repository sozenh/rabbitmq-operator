package controllers

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	rabbitmqv1beta1 "github.com/rabbitmq/cluster-operator/v2/api/v1beta1"
	"github.com/rabbitmq/cluster-operator/v2/controllers/result"
	"github.com/rabbitmq/cluster-operator/v2/internal/status"
)

const pauseReconciliationLabel = "rabbitmq.com/pauseReconciliation"

func (r *RabbitmqClusterReconciler) reconcilePaused(
	ctx context.Context, rmq *rabbitmqv1beta1.RabbitmqCluster) result.ReconcileResult {
	logger := ctrl.LoggerFrom(ctx)

	v, ok := rmq.Labels[pauseReconciliationLabel]

	if !ok {
		return result.Continue()
	}
	if v != "true" {
		return result.Continue()
	}

	logger.Info("not reconciling RabbitmqCluster")
	r.Recorder.Event(
		rmq, corev1.EventTypeWarning,
		"PausedReconciliation", fmt.Sprintf("label '%s' is set to true", pauseReconciliationLabel))
	status.SetCondition(&rmq.Status, rabbitmqv1beta1.NoWarnings, corev1.ConditionFalse, "reconciliation paused")
	return result.Done()
}
