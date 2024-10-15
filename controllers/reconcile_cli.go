package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	rabbitmqv1beta1 "github.com/rabbitmq/cluster-operator/v2/api/v1beta1"
	"github.com/rabbitmq/cluster-operator/v2/controllers/result"
	"github.com/rabbitmq/cluster-operator/v2/internal/constant"
	"github.com/rabbitmq/cluster-operator/v2/internal/resource"
)

func (r *RabbitmqClusterReconciler) reconcileRabbitmqCLI(
	ctx context.Context, rmq *rabbitmqv1beta1.RabbitmqCluster) result.ReconcileResult {
	logger := ctrl.LoggerFrom(ctx)

	statefulsets, err := r.statefulSets(ctx, rmq)
	if err != nil {
		return result.Error(err)
	}
	if !allReplicasReadyAndUpdated(statefulsets) ||
		len(statefulsets) != int(*rmq.Spec.Replicas) {
		logger.Info("not all replicas ready yet; requeue request")
		return result.RequeueSoon(15 * time.Second)
	}

	// Retrieve the plugins config map, if it exists.
	cm, err := r.configMapPlugins(ctx, rmq)
	if err != nil {
		return result.Error(fmt.Errorf("get plugin configmap failed: %v", err))
	}

	if r.pluginsConfigUpdated(cm) {
		if r.pluginsConfigUpdatedRecently(cm) {
			// plugins configMap was updated very recently
			// give StatefulSet controller some time to trigger restart of StatefulSet if necessary
			// otherwise, there would be race conditions where we exec into containers losing the connection due to pods being terminated
			logger.Info("plugin config updated very recently; requeue request")
			return result.RequeueSoon(2 * time.Second)
		}

		if err = r.runSetPluginsCommand(rmq, logger); err != nil {
			return result.Error(fmt.Errorf("runSetPluginsCommand failed: %v", err))
		}
		if err = r.removePluginsConfigUpdatedFlag(ctx, cm); err != nil {
			return result.Error(fmt.Errorf("removePluginsConfigUpdatedFlag failed: %v", err))
		}
	}

	for _, statefulset := range statefulsets {
		// If RabbitMQ cluster is newly created, enable all feature flags since some are disabled by default
		if r.statefulsetCreated(&statefulset) {
			if err = r.runEnableFeatureFlagsCommand(rmq, logger); err != nil {
				return result.Error(fmt.Errorf("runEnableFeatureFlagsCommand failed: %v", err))
			}
			if err = r.removeStatefulsetCreatedFlag(ctx, &statefulset); err != nil {
				return result.Error(fmt.Errorf("removeStatefulsetCreatedFlag failed: %v", err))
			}
		}
	}

	// If the cluster has been marked as needing it, run rabbitmq-queues rebalance all
	if r.needQueueRebalance(rmq) {
		if err = r.runQueueRebalanceCommand(rmq, logger); err != nil {
			return result.Error(fmt.Errorf("runQueueRebalanceCommand failed: %v", err))
		}
		if err = r.removeQueueRebalanceFlag(ctx, rmq); err != nil {
			return result.Error(fmt.Errorf("removeQueueRebalanceFlag failed: %v", err))
		}
	}

	return result.Continue()
}

// There are 2 paths how plugins are set:
// 1. When StatefulSet is (re)started, the up-to-date plugins list (ConfigMap copied by the init container) is read by RabbitMQ nodes during node start up.
// 2. When the plugins ConfigMap is changed, 'rabbitmq-plugins set' updates the plugins on every node (without the need to re-start the nodes).
// This method implements the 2nd path.
func (r *RabbitmqClusterReconciler) runSetPluginsCommand(rmq *rabbitmqv1beta1.RabbitmqCluster, logger logr.Logger) error {
	plugins := resource.NewRabbitmqPlugins(rmq.Spec.Rabbitmq.AdditionalPlugins)

	for i := int32(0); i < *rmq.Spec.Replicas; i++ {
		pod := rmq.PodName(constant.ResourceStatefulsetSuffix, int(i))
		cmd := fmt.Sprintf("rabbitmq-plugins set %s", plugins.AsString(" "))

		logger.Info("set plugins on pod", "pod", pod, "command", cmd)
		stdout, stderr, err := r.exec(rmq.Namespace, pod, constant.ContainerNameRabbitMQ, "sh", "-c", cmd)
		if err != nil {
			msg := "failed to set plugins on pod"
			logger.Error(err, msg, "pod", pod, "command", cmd, "stdout", stdout, "stderr", stderr)
			r.Recorder.Event(rmq, corev1.EventTypeWarning, "FailedReconcile", fmt.Sprintf("%s %s", msg, pod))
			return fmt.Errorf("%s %s: %w", msg, pod, err)
		}
	}

	logger.Info("successfully set plugins")
	return nil
}

func (r *RabbitmqClusterReconciler) runQueueRebalanceCommand(rmq *rabbitmqv1beta1.RabbitmqCluster, logger logr.Logger) error {
	cmd := "rabbitmq-queues rebalance all"
	podName := rmq.PodName(constant.ResourceStatefulsetSuffix, 0)

	logger.Info("run queue rebalance on pod", "pod", podName, "command", cmd)
	stdout, stderr, err := r.exec(rmq.Namespace, podName, constant.ContainerNameRabbitMQ, "sh", "-c", cmd)

	if err == nil {
		logger.Info("successfully rebalanced queues")
		return nil
	}

	msg := "failed to run queue rebalance on pod"
	logger.Error(err, msg, "pod", podName, "command", cmd, "stdout", stdout, "stderr", stderr)
	r.Recorder.Event(rmq, corev1.EventTypeWarning, "FailedReconcile", fmt.Sprintf("%s %s", msg, podName))
	return fmt.Errorf("%s %s: %w", msg, podName, err)
}

func (r *RabbitmqClusterReconciler) runEnableFeatureFlagsCommand(rmq *rabbitmqv1beta1.RabbitmqCluster, logger logr.Logger) error {
	cmd := "rabbitmqctl enable_feature_flag all"
	podName := rmq.PodName(constant.ResourceStatefulsetSuffix, 0)

	logger.Info("enable all feature flags on pod", "pod", podName, "command", cmd)
	stdout, stderr, err := r.exec(rmq.Namespace, podName, constant.ContainerNameRabbitMQ, "sh", "-c", cmd)
	if err == nil {
		logger.Info("successfully enabled all feature flags")
		return nil
	}

	msg := "failed to enable all feature flags on pod"
	logger.Error(err, msg, "pod", podName, "command", cmd, "stdout", stdout, "stderr", stderr)
	r.Recorder.Event(rmq, corev1.EventTypeWarning, "FailedReconcile", fmt.Sprintf("%s %s", msg, podName))
	return fmt.Errorf("%s %s: %w", msg, podName, err)
}
