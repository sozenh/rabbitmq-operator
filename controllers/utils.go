package controllers

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rabbitmqv1beta1 "github.com/rabbitmq/cluster-operator/v2/api/v1beta1"
	"github.com/rabbitmq/cluster-operator/v2/internal/constant"
	"github.com/rabbitmq/cluster-operator/v2/internal/metadata"
)

func (r *RabbitmqClusterReconciler) exec(namespace, podName, containerName string, command ...string) (string, string, error) {
	return r.PodExecutor.Exec(r.Clientset, r.ClusterConfig, namespace, podName, containerName, command...)
}

func (r *RabbitmqClusterReconciler) deleteAnnotation(ctx context.Context, obj client.Object, annotation string) error {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return err
	}
	annotations := accessor.GetAnnotations()
	if annotations == nil {
		return nil
	}
	delete(annotations, annotation)
	accessor.SetAnnotations(annotations)
	return r.Update(ctx, obj)
}

func (r *RabbitmqClusterReconciler) updateAnnotation(ctx context.Context, obj client.Object, namespace, objName, key, value string) error {
	return retry.OnError(
		retry.DefaultRetry,
		errorIsConflictOrNotFound, // StatefulSet needs time to be found after it got created
		func() error {
			if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: objName}, obj); err != nil {
				return err
			}
			accessor, err := meta.Accessor(obj)
			if err != nil {
				return err
			}
			annotations := accessor.GetAnnotations()
			if annotations == nil {
				annotations = make(map[string]string)
			}
			annotations[key] = value
			accessor.SetAnnotations(annotations)
			return r.Update(ctx, obj)
		})
}

func errorIsConflictOrNotFound(err error) bool {
	return errors.IsConflict(err) || errors.IsNotFound(err)
}

func (r *RabbitmqClusterReconciler) configMapServer(
	ctx context.Context,
	rmq *rabbitmqv1beta1.RabbitmqCluster) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{}
	return configMap, r.Get(
		ctx,
		types.NamespacedName{
			Namespace: rmq.Namespace,
			Name:      rmq.ChildResourceName(constant.ResourceServerConfigMapSuffix)}, configMap)
}

func (r *RabbitmqClusterReconciler) configMapPlugins(
	ctx context.Context,
	rmq *rabbitmqv1beta1.RabbitmqCluster) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{}
	return configMap, r.Get(
		ctx,
		types.NamespacedName{
			Namespace: rmq.Namespace,
			Name:      rmq.ChildResourceName(constant.ResourcePluginConfigMapSuffix)}, configMap)
}

func (r *RabbitmqClusterReconciler) statefulSet(
	ctx context.Context,
	key types.NamespacedName) (*appsv1.StatefulSet, error) {
	statefulset := &appsv1.StatefulSet{}
	return statefulset, r.Get(ctx, key, statefulset)
}

func (r *RabbitmqClusterReconciler) statefulSets(
	ctx context.Context,
	rmq *rabbitmqv1beta1.RabbitmqCluster) ([]appsv1.StatefulSet, error) {
	statefulsets := &appsv1.StatefulSetList{}
	return statefulsets.Items, r.List(
		ctx, statefulsets,
		client.InNamespace(rmq.Namespace), client.MatchingLabels(metadata.LabelSelector(rmq.Name)))
}

// statefulSetUID only returns the UID successfully when the controller reference uid matches the rmq uid
func (r *RabbitmqClusterReconciler) statefulSetUID(
	ctx context.Context,
	sts *appsv1.StatefulSet, rmq *rabbitmqv1beta1.RabbitmqCluster) (types.UID, error) {
	var ref *metav1.OwnerReference
	if ref = metav1.GetControllerOf(sts); ref == nil {
		return "", fmt.Errorf("failed to get controller reference for statefulSet %s", sts.GetName())
	}
	if string(rmq.GetUID()) != string(ref.UID) {
		return "", fmt.Errorf("statefulSet %s not owned by current RabbitmqCluster %s", sts.GetName(), rmq.GetName())
	}
	return sts.UID, nil
}

func statefulSetBeingUpdated(sts *appsv1.StatefulSet) bool {
	return sts.Status.CurrentRevision != sts.Status.UpdateRevision
}

func allReplicasReadyAndUpdated(statefulsets []appsv1.StatefulSet) bool {
	ready := true
	for _, statefulset := range statefulsets {
		if statefulSetBeingUpdated(&statefulset) {
			ready = false
			break
		}
		if statefulset.Status.ReadyReplicas != *statefulset.Spec.Replicas {
			ready = false
			break
		}
	}

	return ready
}

func statefulSetNeedsQueueRebalance(sts *appsv1.StatefulSet, rmq *rabbitmqv1beta1.RabbitmqCluster) bool {
	return statefulSetBeingUpdated(sts) && !rmq.Spec.SkipPostDeploySteps && *rmq.Spec.Replicas > 1
}

func persistenceStorageCapacity(name string, templates []corev1.PersistentVolumeClaim) k8sresource.Quantity {
	for _, t := range templates {
		if t.Name == name {
			return t.Spec.Resources.Requests[corev1.ResourceStorage]
		}
	}
	return k8sresource.MustParse("0")
}
