package controllers

import (
	"context"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	rabbitmqv1beta1 "github.com/rabbitmq/cluster-operator/v2/api/v1beta1"
	"github.com/rabbitmq/cluster-operator/v2/controllers/result"
	"github.com/rabbitmq/cluster-operator/v2/internal/constant"
	"github.com/rabbitmq/cluster-operator/v2/internal/resource"
)

const (
	pluginsUpdateAnnotation  = "rabbitmq.com/pluginsUpdatedAt"
	serverConfAnnotation     = "rabbitmq.com/serverConfUpdatedAt"
	stsRestartAnnotation     = "rabbitmq.com/lastRestartAt"
	stsCreateAnnotation      = "rabbitmq.com/createdAt"
	queueRebalanceAnnotation = "rabbitmq.com/queueRebalanceNeededAt"
)

func (r *RabbitmqClusterReconciler) reconcileQueueRebalanced(
	ctx context.Context, rmq *rabbitmqv1beta1.RabbitmqCluster) result.ReconcileResult {
	statefulsets, err := r.statefulSets(ctx, rmq)
	// The StatefulSet may not have been created by this point, so ignore Not Found errors
	if client.IgnoreNotFound(err) != nil {
		return result.Error(err)
	}

	needRebalance := false
	for _, statefulset := range statefulsets {
		if statefulSetNeedsQueueRebalance(&statefulset, rmq) {
			needRebalance = true
			break
		}
	}

	if !needRebalance {
		return result.Continue()
	}
	return r.markForQueueRebalance(ctx, rmq)
}

func (r *RabbitmqClusterReconciler) markForQueueRebalance(
	ctx context.Context, rmq *rabbitmqv1beta1.RabbitmqCluster) result.ReconcileResult {
	if rmq.ObjectMeta.Annotations == nil {
		rmq.ObjectMeta.Annotations = make(map[string]string)
	}

	if len(rmq.ObjectMeta.Annotations[queueRebalanceAnnotation]) > 0 {
		return result.Continue()
	}

	rmq.ObjectMeta.Annotations[queueRebalanceAnnotation] = time.Now().Format(time.RFC3339)

	err := r.Update(ctx, rmq)
	if err == nil {
		return result.Done()
	}
	ctrl.LoggerFrom(ctx).Error(err, "Failed to mark annotation for queue rebalance")
	return result.Error(err)
}

// Annotates an object depending on object type and operationResult.
// These annotations are temporary markers used in later reconcile loops to perform some action (such as restarting the StatefulSet or executing RabbitMQ CLI commands)
func (r *RabbitmqClusterReconciler) annotateIfNeeded(
	ctx context.Context, rmq *rabbitmqv1beta1.RabbitmqCluster, builder resource.ResourceBuilder, operationResult controllerutil.OperationResult) result.ReconcileResult {

	var (
		obj           client.Object
		objName       string
		annotationKey string
		logger        = ctrl.LoggerFrom(ctx)
	)

	switch b := builder.(type) {
	default:
		return result.Continue()
	case *resource.RabbitmqPluginsConfigMapBuilder:
		if operationResult != controllerutil.OperationResultUpdated {
			return result.Continue()
		}
		obj = &corev1.ConfigMap{}
		objName = rmq.ChildResourceName(constant.ResourcePluginConfigMapSuffix)
		annotationKey = pluginsUpdateAnnotation

	case *resource.ServerConfigMapBuilder:
		if operationResult != controllerutil.OperationResultUpdated || !b.UpdateRequiresStsRestart {
			return result.Continue()
		}
		obj = &corev1.ConfigMap{}
		objName = rmq.ChildResourceName(constant.ResourceServerConfigMapSuffix)
		annotationKey = serverConfAnnotation

	case *resource.StatefulSetBuilder:
		if operationResult != controllerutil.OperationResultCreated {
			return result.Continue()
		}
		obj = &appsv1.StatefulSet{}
		objName = rmq.StatefulsetName(constant.ResourceStatefulsetSuffix, b.Index)
		annotationKey = stsCreateAnnotation

	}

	if err := r.updateAnnotation(ctx, obj, rmq.Namespace, objName, annotationKey, time.Now().Format(time.RFC3339)); err != nil {
		msg := "failed to annotate " + objName
		logger.Error(err, msg)
		r.Recorder.Event(rmq, corev1.EventTypeWarning, "FailedUpdate", msg)
		return result.Error(err)
	}

	logger.Info("successfully annotated", "object", objName, "annotate", annotationKey)
	return result.Done()
}

func (r *RabbitmqClusterReconciler) statefulsetCreated(sts *appsv1.StatefulSet) bool {
	if sts.Annotations == nil {
		return false
	}
	if sts.Annotations[stsCreateAnnotation] == "" {
		return false
	}
	return true
}

func (r *RabbitmqClusterReconciler) serverConfigUpdated(cfg *corev1.ConfigMap) bool {
	if cfg.Annotations == nil {
		return false
	}
	if cfg.Annotations[serverConfAnnotation] == "" {
		return false
	}

	return true
}

func (r *RabbitmqClusterReconciler) pluginsConfigUpdated(cfg *corev1.ConfigMap) bool {
	if cfg.Annotations == nil {
		return false
	}
	if cfg.Annotations[pluginsUpdateAnnotation] == "" {
		return false
	}

	return true
}

func (r *RabbitmqClusterReconciler) pluginsConfigUpdatedRecently(cfg *corev1.ConfigMap) bool {
	if !r.pluginsConfigUpdated(cfg) {
		return false
	}
	pluginsUpdatedAt, ok := cfg.Annotations[pluginsUpdateAnnotation]
	if !ok {
		return false // plugins configMap was not updated
	}
	annotationTime, err := time.Parse(time.RFC3339, pluginsUpdatedAt)
	if err != nil {
		return false
	}
	return time.Since(annotationTime).Seconds() < 2
}

func (r *RabbitmqClusterReconciler) needQueueRebalance(rmq *rabbitmqv1beta1.RabbitmqCluster) bool {
	if rmq.Annotations == nil {
		return false
	}
	if rmq.Annotations[queueRebalanceAnnotation] == "" {
		return false
	}
	return true
}

func (r *RabbitmqClusterReconciler) removeStatefulsetCreatedFlag(ctx context.Context, sts *appsv1.StatefulSet) error {
	return r.deleteAnnotation(ctx, sts, stsCreateAnnotation)
}

func (r *RabbitmqClusterReconciler) removeServerConfigUpdatedFlag(ctx context.Context, cfg *corev1.ConfigMap) error {
	return r.deleteAnnotation(ctx, cfg, serverConfAnnotation)
}

func (r *RabbitmqClusterReconciler) removePluginsConfigUpdatedFlag(ctx context.Context, cfg *corev1.ConfigMap) error {
	return r.deleteAnnotation(ctx, cfg, pluginsUpdateAnnotation)
}

func (r *RabbitmqClusterReconciler) removeQueueRebalanceFlag(ctx context.Context, rmq *rabbitmqv1beta1.RabbitmqCluster) error {
	return r.deleteAnnotation(ctx, rmq, queueRebalanceAnnotation)
}
