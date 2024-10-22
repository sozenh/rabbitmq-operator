package controllers

import (
	"context"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientretry "k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"

	rabbitmqv1beta1 "github.com/rabbitmq/cluster-operator/v2/api/v1beta1"
	"github.com/rabbitmq/cluster-operator/v2/controllers/result"
	"github.com/rabbitmq/cluster-operator/v2/internal/constant"
	"github.com/rabbitmq/cluster-operator/v2/internal/metadata"
	"github.com/rabbitmq/cluster-operator/v2/internal/status"
)

// reconcileStatus sets status.defaultUser (secret and service reference) and status.binding.
// when vault is used as secret backend for default user, no user secret object is created
// therefore only status.defaultUser.serviceReference is set.
// status.binding exposes the default user secret which contains the binding
// information for this RabbitmqCluster.
// Default user secret implements the service binding Provisioned Service
// See: https://k8s-service-bindings.github.io/spec/#provisioned-service
func (r *RabbitmqClusterReconciler) reconcileStatus(
	ctx context.Context, rmq *rabbitmqv1beta1.RabbitmqCluster) result.ReconcileResult {
	old := rmq.Status.DeepCopy()

	if *rmq.Spec.Replicas > rmq.Status.Replicas {
		rmq.Status.Replicas = *rmq.Spec.Replicas
	}
	rmq.Status.DefaultUser =
		&rabbitmqv1beta1.RabbitmqClusterDefaultUser{
			ServiceReference: &rabbitmqv1beta1.RabbitmqClusterServiceReference{
				Namespace: rmq.Namespace,
				Name:      rmq.ChildResourceName(constant.ResourceClientServiceSuffix),
			},
		}

	if !rmq.VaultDefaultUserSecretEnabled() {
		rmq.Status.DefaultUser.SecretReference =
			&rabbitmqv1beta1.RabbitmqClusterSecretReference{
				Name:      rmq.ChildResourceName(constant.ResourceDefaultUserSuffix),
				Namespace: rmq.Namespace,
				Keys:      map[string]string{"username": "username", "password": "password"},
			}

		if rmq.ExternalSecretEnabled() {
			rmq.Status.Binding = &corev1.LocalObjectReference{Name: rmq.Spec.SecretBackend.ExternalSecret.Name}
		} else {
			rmq.Status.Binding = &corev1.LocalObjectReference{Name: rmq.ChildResourceName(constant.ResourceDefaultUserSuffix)}
		}
	}

	if old.Replicas == rmq.Status.Replicas &&
		reflect.DeepEqual(old.Binding, rmq.Status.Binding) &&
		reflect.DeepEqual(old.DefaultUser, rmq.Status.DefaultUser) {
		// no need to update
		return result.Continue()
	}

	return r.updateStatus(ctx, rmq)
}

func (r *RabbitmqClusterReconciler) reconcileStatusConditions(
	ctx context.Context, rmq *rabbitmqv1beta1.RabbitmqCluster) result.ReconcileResult {
	childResources, err := r.getChildResources(ctx, rmq)
	if err != nil {
		return result.Error(err)
	}

	old := rmq.Status.DeepCopy()
	status.SetConditions(childResources, &rmq.Status)

	if reflect.DeepEqual(old.Conditions, rmq.Status.Conditions) {
		// no need to update
		return result.Continue()
	}

	return r.updateStatus(ctx, rmq)
}

func (r *RabbitmqClusterReconciler) updateStatus(
	ctx context.Context, rmq *rabbitmqv1beta1.RabbitmqCluster) result.ReconcileResult {
	logger := ctrl.LoggerFrom(ctx)

	logger.Info("update RabbitmqCluster status")
	err := clientretry.RetryOnConflict(
		clientretry.DefaultRetry,
		func() error { return r.Status().Update(ctx, rmq) },
	)

	if err == nil {
		return result.Done()
	}
	logger.Error(err, "failed to update RabbitmqCluster status")
	return result.Error(err)
}

func (r *RabbitmqClusterReconciler) getChildResources(ctx context.Context, rmq *rabbitmqv1beta1.RabbitmqCluster) ([]runtime.Object, error) {
	endPoints := &corev1.Endpoints{}
	statefulSetList := &appsv1.StatefulSetList{}

	if err := client.IgnoreNotFound(
		r.Client.List(
			ctx, statefulSetList,
			client.InNamespace(rmq.Namespace),
			client.MatchingLabels(metadata.LabelSelector(rmq.Name)),
		),
	); err != nil {
		return nil, err
	}

	if err := r.Client.Get(ctx,
		types.NamespacedName{Name: rmq.ChildResourceName(constant.ResourceClientServiceSuffix), Namespace: rmq.Namespace},
		endPoints); err != nil && !k8serrors.IsNotFound(err) {
		return nil, err
	} else if k8serrors.IsNotFound(err) {
		endPoints = nil
	}

	var res []runtime.Object
	var sts *appsv1.StatefulSet
	for i := int32(0); i < *rmq.Spec.Replicas; i++ {
		res = append(res, sts)
	}
	res = append(res, endPoints)
	for i := range statefulSetList.Items {
		res[i] = &statefulSetList.Items[i]
	}

	return res, nil
}
