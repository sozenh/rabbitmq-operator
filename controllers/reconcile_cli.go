package controllers

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rabbitmqv1beta1 "github.com/rabbitmq/cluster-operator/v2/api/v1beta1"
	"github.com/rabbitmq/cluster-operator/v2/internal/constant"
	"github.com/rabbitmq/cluster-operator/v2/internal/resource"
)

const queueRebalanceAnnotation = "rabbitmq.com/queueRebalanceNeededAt"

func (r *RabbitmqClusterReconciler) runRabbitmqCLICommandsIfAnnotated(
	ctx context.Context, rmq *rabbitmqv1beta1.RabbitmqCluster) (requeueAfter time.Duration, err error) {
	logger := ctrl.LoggerFrom(ctx)

	sts, err := r.statefulSet(ctx, rmq)
	if err != nil {
		return 0, err
	}
	if !allReplicasReadyAndUpdated(sts) {
		logger.V(1).Info("not all replicas ready yet; requeuing request to run RabbitMQ CLI commands")
		return 15 * time.Second, nil
	}

	// Retrieve the plugins config map, if it exists.
	pluginsConfig, err := r.configMap(ctx, rmq, rmq.ChildResourceName(constant.ResourcePluginConfigMapSuffix))
	if client.IgnoreNotFound(err) != nil {
		return 0, err
	}
	updatedRecently, err := pluginsConfigUpdatedRecently(pluginsConfig)
	if err != nil {
		return 0, err
	}
	if updatedRecently {
		// plugins configMap was updated very recently
		// give StatefulSet controller some time to trigger restart of StatefulSet if necessary
		// otherwise, there would be race conditions where we exec into containers losing the connection due to pods being terminated
		logger.V(1).Info("requeuing request to set plugins")
		return 2 * time.Second, nil
	}

	if pluginsConfig.ObjectMeta.Annotations != nil && pluginsConfig.ObjectMeta.Annotations[pluginsUpdateAnnotation] != "" {
		if err = r.runSetPluginsCommand(ctx, rmq, pluginsConfig); err != nil {
			return 0, err
		}
	}

	// If RabbitMQ cluster is newly created, enable all feature flags since some are disabled by default
	if sts.ObjectMeta.Annotations != nil && sts.ObjectMeta.Annotations[stsCreateAnnotation] != "" {
		if err := r.runEnableFeatureFlagsCommand(ctx, rmq, sts); err != nil {
			return 0, err
		}
	}

	// If the cluster has been marked as needing it, run rabbitmq-queues rebalance all
	if rmq.ObjectMeta.Annotations != nil && rmq.ObjectMeta.Annotations[queueRebalanceAnnotation] != "" {
		if err := r.runQueueRebalanceCommand(ctx, rmq); err != nil {
			return 0, err
		}
	}

	return 0, nil
}

func (r *RabbitmqClusterReconciler) runEnableFeatureFlagsCommand(
	ctx context.Context, rmq *rabbitmqv1beta1.RabbitmqCluster, sts *appsv1.StatefulSet) error {
	logger := ctrl.LoggerFrom(ctx)

	cmd := "rabbitmqctl enable_feature_flag all"
	podName := fmt.Sprintf("%s-0", rmq.ChildResourceName(constant.ResourceStatefulsetSuffix))
	stdout, stderr, err := r.exec(rmq.Namespace, podName, constant.ContainerNameRabbitMQ, "bash", "-c", cmd)
	if err != nil {
		msg := "failed to enable all feature flags on pod"
		logger.Error(err, msg, "pod", podName, "command", cmd, "stdout", stdout, "stderr", stderr)
		r.Recorder.Event(rmq, corev1.EventTypeWarning, "FailedReconcile", fmt.Sprintf("%s %s", msg, podName))
		return fmt.Errorf("%s %s: %w", msg, podName, err)
	}

	logger.Info("successfully enabled all feature flags")
	return r.deleteAnnotation(ctx, sts, stsCreateAnnotation)
}

// There are 2 paths how plugins are set:
// 1. When StatefulSet is (re)started, the up-to-date plugins list (ConfigMap copied by the init container) is read by RabbitMQ nodes during node start up.
// 2. When the plugins ConfigMap is changed, 'rabbitmq-plugins set' updates the plugins on every node (without the need to re-start the nodes).
// This method implements the 2nd path.
func (r *RabbitmqClusterReconciler) runSetPluginsCommand(
	ctx context.Context, rmq *rabbitmqv1beta1.RabbitmqCluster, configMap *corev1.ConfigMap) error {
	logger := ctrl.LoggerFrom(ctx)
	plugins := resource.NewRabbitmqPlugins(rmq.Spec.Rabbitmq.AdditionalPlugins)

	for i := int32(0); i < *rmq.Spec.Replicas; i++ {
		cmd := fmt.Sprintf("rabbitmq-plugins set %s", plugins.AsString(" "))
		podName := fmt.Sprintf("%s-%d", rmq.ChildResourceName(constant.ResourceStatefulsetSuffix), i)
		stdout, stderr, err := r.exec(rmq.Namespace, podName, constant.ContainerNameRabbitMQ, "sh", "-c", cmd)
		if err != nil {
			msg := "failed to set plugins on pod"
			logger.Error(err, msg, "pod", podName, "command", cmd, "stdout", stdout, "stderr", stderr)
			r.Recorder.Event(rmq, corev1.EventTypeWarning, "FailedReconcile", fmt.Sprintf("%s %s", msg, podName))
			return fmt.Errorf("%s %s: %w", msg, podName, err)
		}
	}

	logger.Info("successfully set plugins")
	return r.deleteAnnotation(ctx, configMap, pluginsUpdateAnnotation)
}

func (r *RabbitmqClusterReconciler) runQueueRebalanceCommand(ctx context.Context, rmq *rabbitmqv1beta1.RabbitmqCluster) error {
	logger := ctrl.LoggerFrom(ctx)

	cmd := "rabbitmq-queues rebalance all"
	podName := fmt.Sprintf("%s-0", rmq.ChildResourceName(constant.ResourceStatefulsetSuffix))
	stdout, stderr, err := r.exec(rmq.Namespace, podName, constant.ContainerNameRabbitMQ, "sh", "-c", cmd)
	if err != nil {
		msg := "failed to run queue rebalance on pod"
		logger.Error(err, msg, "pod", podName, "command", cmd, "stdout", stdout, "stderr", stderr)
		r.Recorder.Event(rmq, corev1.EventTypeWarning, "FailedReconcile", fmt.Sprintf("%s %s", msg, podName))
		return fmt.Errorf("%s %s: %w", msg, podName, err)
	}
	logger.Info("successfully rebalanced queues")
	return r.deleteAnnotation(ctx, rmq, queueRebalanceAnnotation)
}
