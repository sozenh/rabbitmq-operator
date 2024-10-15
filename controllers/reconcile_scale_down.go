package controllers

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/rabbitmq/cluster-operator/v2/api/v1beta1"
	"github.com/rabbitmq/cluster-operator/v2/controllers/result"
	"github.com/rabbitmq/cluster-operator/v2/internal/status"
)

// cluster scale down not supported
// log error, publish warning event, and set ReconcileSuccess to false when scale down request detected
func (r *RabbitmqClusterReconciler) reconcileScaleDown(
	ctx context.Context, rmq *v1beta1.RabbitmqCluster) result.ReconcileResult {
	logger := ctrl.LoggerFrom(ctx)

	next := *rmq.Spec.Replicas
	curr := rmq.Status.Replicas
	if rmq.Status.Replicas <= *rmq.Spec.Replicas {
		return result.Continue()
	}

	reason := "UnsupportedOperation"
	msg := fmt.Sprintf("cluster Scale down not supported; tried to scale cluster from %d nodes to %d nodes", curr, next)

	logger.Error(errors.New(reason), msg)
	r.Recorder.Event(rmq, corev1.EventTypeWarning, reason, msg)
	status.SetCondition(&rmq.Status, v1beta1.ReconcileSuccess, corev1.ConditionFalse, reason, msg)

	return result.Done()
}
