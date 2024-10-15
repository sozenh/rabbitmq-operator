/*
RabbitMQ Cluster Operator

Copyright 2020 VMware, Inc. All Rights Reserved.

This product is licensed to you under the Mozilla Public license, Version 2.0 (the "License").  You may not use this product except in compliance with the Mozilla Public License.

This product may include a number of subcomponents with separate copyright notices and license terms. Your use of these subcomponents is subject to the terms and conditions of the subcomponent's license, as noted in the LICENSE file.
*/

package controllers

import (
	"context"
	"encoding/json"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	clientretry "k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rabbitmqv1beta1 "github.com/rabbitmq/cluster-operator/v2/api/v1beta1"
	"github.com/rabbitmq/cluster-operator/v2/controllers/result"
	"github.com/rabbitmq/cluster-operator/v2/internal/status"
)

var (
	apiGVStr = rabbitmqv1beta1.GroupVersion.String()
)

const (
	ownerKey  = ".metadata.controller"
	ownerKind = "RabbitmqCluster"
)

// RabbitmqClusterReconciler reconciles a RabbitmqCluster object
type RabbitmqClusterReconciler struct {
	client.Client
	APIReader               client.Reader
	Scheme                  *runtime.Scheme
	Namespace               string
	Recorder                record.EventRecorder
	ClusterConfig           *rest.Config
	Clientset               *kubernetes.Clientset
	PodExecutor             PodExecutor
	DefaultRabbitmqImage    string
	DefaultUserUpdaterImage string
	DefaultImagePullSecrets string
	ControlRabbitmqImage    bool
}

// the rbac rule requires an empty row at the end to render
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=create
// +kubebuilder:rbac:groups="",resources=pods,verbs=update;get;list;watch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups="",resources=endpoints,verbs=get;watch;list
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups=rabbitmq.com,resources=rabbitmqclusters,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups=rabbitmq.com,resources=rabbitmqclusters/status,verbs=get;update
// +kubebuilder:rbac:groups=rabbitmq.com,resources=rabbitmqclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=events,verbs=get;create;patch
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=roles,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=rolebindings,verbs=get;list;watch;create;update

func (r *RabbitmqClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)

	rmq, err := r.getRabbitmqCluster(ctx, req.NamespacedName)
	if k8serrors.IsNotFound(err) {
		// No need to requeue if the resource no longer exists
		return ctrl.Result{}, nil
	}
	if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
	}

	before := rmq.DeepCopy()
	var res result.ReconcileResult
	defer func() {
		if statusErr := clientretry.RetryOnConflict(
			clientretry.DefaultRetry,
			func() error {
				if reflect.DeepEqual(before.Status, rmq.Status) {
					return nil
				}
				return r.Status().Update(ctx, rmq)
			},
		); statusErr != nil {
			logger.Error(statusErr, "Failed to update ReconcileSuccess condition state")
		}
	}()

	logger.Info("Start reconciling")
	instanceSpec, err := json.Marshal(rmq.Spec)
	if err != nil {
		logger.Error(err, "Failed to marshal cluster spec")
	}
	logger.V(1).Info("RabbitmqCluster", "spec", string(instanceSpec))

	// Check if the resource has been marked for deletion
	if res = r.reconcileDelete(ctx, rmq); res.Completed() {
		return res.Output()
	}
	// Check if operator is asked to pause reconcile
	if res = r.reconcilePaused(ctx, rmq); res.Completed() {
		return res.Output()
	}
	if res = r.reconcileOperatorDefaults(ctx, rmq); res.Completed() {
		return res.Output()
	}
	if res = r.reconcileStatusConditions(ctx, rmq); res.Completed() {
		return res.Output()
	}
	if res = r.reconcileTLS(ctx, rmq); res.Completed() {
		return res.Output()
	}
	if res = r.reconcileQueueRebalanced(ctx, rmq); res.Completed() {
		return res.Output()
	}
	if res = r.reconcileScaleDown(ctx, rmq); res.Completed() {
		return res.Output()
	}
	if res = r.reconcileResources(ctx, rmq); res.Completed() {
		return res.Output()
	}
	if res = r.restartStatefulSetIfNeeded(ctx, rmq); res.Completed() {
		return res.Output()
	}
	if res = r.reconcileStatus(ctx, rmq); res.Completed() {
		return res.Output()
	}
	// By this point the StatefulSet may have finished deploying. Run any
	// post-deploy steps if so, or requeue until the deployment is finished.
	if res = r.reconcileRabbitmqCLI(ctx, rmq); res.Completed() {
		if res.IsError() {
			status.SetCondition(&rmq.Status,
				rabbitmqv1beta1.ReconcileSuccess, corev1.ConditionFalse, "FailedCLICommand", res.GetError().Error())
		}
		return res.Output()
	}

	// Set ReconcileSuccess to true and update observedGeneration after all reconciliation steps have finished with no error
	rmq.Status.ObservedGeneration = rmq.GetGeneration()
	logger.Info("Finished reconciling")
	status.SetCondition(&rmq.Status, rabbitmqv1beta1.ReconcileSuccess, corev1.ConditionTrue, "Success", "Finish reconciling")

	return ctrl.Result{}, nil
}

func (r *RabbitmqClusterReconciler) getRabbitmqCluster(ctx context.Context, namespacedName types.NamespacedName) (*rabbitmqv1beta1.RabbitmqCluster, error) {
	rabbitmqClusterInstance := &rabbitmqv1beta1.RabbitmqCluster{}
	err := r.Get(ctx, namespacedName, rabbitmqClusterInstance)
	return rabbitmqClusterInstance, err
}

func (r *RabbitmqClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	for _, resource := range []client.Object{&appsv1.StatefulSet{}, &corev1.ConfigMap{}, &corev1.Service{}} {
		if err := mgr.GetFieldIndexer().IndexField(context.Background(), resource, ownerKey, addResourceToIndex); err != nil {
			return err
		}
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&rabbitmqv1beta1.RabbitmqCluster{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.Secret{}).
		Complete(r)
}

func addResourceToIndex(rawObj client.Object) []string {
	switch resourceObject := rawObj.(type) {
	case *appsv1.StatefulSet, *corev1.ConfigMap, *corev1.Service, *rbacv1.Role, *rbacv1.RoleBinding, *corev1.ServiceAccount, *corev1.Secret:
		owner := metav1.GetControllerOf(resourceObject)
		return validateAndGetOwner(owner)
	default:
		return nil
	}
}

func validateAndGetOwner(owner *metav1.OwnerReference) []string {
	if owner == nil {
		return nil
	}
	if owner.APIVersion != apiGVStr || owner.Kind != ownerKind {
		return nil
	}
	return []string{owner.Name}
}
