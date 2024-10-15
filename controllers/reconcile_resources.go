package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"gomodules.xyz/jsonpatch/v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	clientretry "k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	rabbitmqv1beta1 "github.com/rabbitmq/cluster-operator/v2/api/v1beta1"
	"github.com/rabbitmq/cluster-operator/v2/controllers/result"
	"github.com/rabbitmq/cluster-operator/v2/internal/resource"
	"github.com/rabbitmq/cluster-operator/v2/internal/status"
)

func (r *RabbitmqClusterReconciler) reconcileResources(
	ctx context.Context, rmq *rabbitmqv1beta1.RabbitmqCluster) result.ReconcileResult {
	logger := ctrl.LoggerFrom(ctx)
	res := r.waitResourceReady(ctx, rmq)
	if res.Completed() {
		logger.Info("resource not ready, requeue")
		return res
	}

	builders := resource.RabbitmqResourceBuilder{
		Scheme:   r.Scheme,
		Instance: rmq,
		Replicas: int(*rmq.Spec.Replicas),
	}

	for _, builder := range builders.ResourceBuilders() {
		object, err := builder.Build()
		if err != nil {
			return result.Error(err)
		}

		switch object.(type) {
		case *appsv1.StatefulSet:
			sts := object.(*appsv1.StatefulSet)
			// The PVCs for the StatefulSet may require expanding
			if res := r.reconcilePVC(ctx, rmq, sts); res.Completed() {
				return res
			}
		}

		var operationResult controllerutil.OperationResult
		err = clientretry.RetryOnConflict(
			clientretry.DefaultRetry,
			func() error {
				var apiError error
				operationResult, apiError = CreateOrUpdate(
					ctx, r.Client, object,
					func() error { return builder.Update(object) }, logger)
				return apiError
			},
		)
		r.logAndRecordOperationResult(ctx, rmq, object, operationResult, err)
		if err != nil {
			status.SetCondition(&rmq.Status, rabbitmqv1beta1.ReconcileSuccess, corev1.ConditionFalse, "Error", err.Error())
			return result.Error(err)
		}

		if res := r.annotateIfNeeded(ctx, rmq, builder, operationResult); res.Completed() {
			return res
		}

		switch operationResult {
		case controllerutil.OperationResultCreated, controllerutil.OperationResultUpdated:
			return result.Done()
		}
	}

	return result.Continue()
}

func (r *RabbitmqClusterReconciler) waitResourceReady(
	ctx context.Context, rmq *rabbitmqv1beta1.RabbitmqCluster) result.ReconcileResult {
	logger := ctrl.LoggerFrom(ctx)

	statefulSets, err := r.statefulSets(ctx, rmq)
	if err != nil {
		return result.Error(err)
	}

	if allReplicasReadyAndUpdated(statefulSets) {
		return result.Continue()
	}

	logger.Info("wait resource ready")
	return result.RequeueSoon(5 * time.Second)
}

// Adds an arbitrary annotation to the sts PodTemplate to trigger a sts restart.
// It compares annotation "rabbitmq.com/serverConfUpdatedAt" from server-conf configMap and annotation "rabbitmq.com/lastRestartAt" from sts
// to determine whether to restart sts.
func (r *RabbitmqClusterReconciler) restartStatefulSetIfNeeded(
	ctx context.Context, rmq *rabbitmqv1beta1.RabbitmqCluster) result.ReconcileResult {
	logger := ctrl.LoggerFrom(ctx)

	serverConf, err := r.configMapServer(ctx, rmq)
	if err != nil {
		if errors.IsNotFound(err) {
			return result.Continue()
		}
		// requeue request after 10s if unable to find server-conf configmap, else return the error
		return result.RequeueSoonError(10*time.Second, err)
	}

	serverConfigUpdatedAt, ok := serverConf.Annotations[serverConfAnnotation]
	if !ok {
		// server-conf configmap hasn't been updated; no need to restart sts
		return result.Continue()
	}

	statefulsets, err := r.statefulSets(ctx, rmq)
	if err != nil {
		// requeue request after 10s if unable to find sts, else return the error
		return result.RequeueSoonError(10*time.Second, err)
	}

	for _, sts := range statefulsets {
		stsRestartedAt, ok := sts.Spec.Template.ObjectMeta.Annotations[stsRestartAnnotation]
		if ok && stsRestartedAt > serverConfigUpdatedAt {
			// sts was updated after the last server-conf configmap update; no need to restart sts
			continue
		}

		err = clientretry.RetryOnConflict(
			clientretry.DefaultRetry,
			func() error {
				if err := r.Get(
					ctx,
					types.NamespacedName{Name: sts.Name, Namespace: sts.Namespace}, &sts); err != nil {
					return err
				}

				if sts.Spec.Template.ObjectMeta.Annotations == nil {
					sts.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
				}
				sts.Spec.Template.ObjectMeta.Annotations[stsRestartAnnotation] = time.Now().Format(time.RFC3339)
				return r.Update(ctx, &sts)
			},
		)
		if err == nil {
			msg := fmt.Sprintf("restarted StatefulSet %s", sts.Name)
			logger.Info(msg)
			r.Recorder.Event(rmq, corev1.EventTypeNormal, "SuccessfulUpdate", msg)
			return result.Done()
		}

		msg := fmt.Sprintf("failed to restart StatefulSet %s; rabbitmq.conf configuration may be outdated", sts.Name)
		logger.Error(err, msg)
		r.Recorder.Event(rmq, corev1.EventTypeWarning, "FailedUpdate", msg)
		// failed to restart sts; return error to requeue request
		return result.Error(err)
	}

	return result.Continue()
}

// logAndRecordOperationResult - helper function to log and record events with message and error
// it logs and records 'updated' and 'created' OperationResult, and ignores OperationResult 'unchanged'
func (r *RabbitmqClusterReconciler) logAndRecordOperationResult(
	ctx context.Context, rmq *rabbitmqv1beta1.RabbitmqCluster, resource client.Object, operationResult controllerutil.OperationResult, err error) {
	if operationResult == controllerutil.OperationResultNone {
		return
	}

	var operation string
	if operationResult == controllerutil.OperationResultCreated {
		operation = "create"
	} else if operationResult == controllerutil.OperationResultUpdated {
		operation = "update"
	} else {
		operation = string(operationResult)
	}

	logger := ctrl.LoggerFrom(ctx)
	caser := cases.Title(language.English)
	if err == nil {
		msg := fmt.Sprintf("%sd resource %s of Type %T", operation, resource.GetName(), resource)
		logger.Info(msg)
		r.Recorder.Event(rmq, corev1.EventTypeNormal, fmt.Sprintf("Successful%s", caser.String(operation)), msg)
	}

	if err != nil {
		var msg string
		if operation != "unchanged" {
			msg = fmt.Sprintf("failed to %s resource %s of Type %T: ", operation, resource.GetName(), resource)
		}
		msg = fmt.Sprintf("%s%s", msg, err)
		logger.Error(err, msg)
		r.Recorder.Event(rmq, corev1.EventTypeWarning, fmt.Sprintf("Failed%s", caser.String(operation)), msg)
	}
}

// mutate wraps a MutateFn and applies validation to its result.
func mutate(f controllerutil.MutateFn, key client.ObjectKey, obj client.Object) error {
	if err := f(); err != nil {
		return err
	}
	if newKey := client.ObjectKeyFromObject(obj); key != newKey {
		return fmt.Errorf("MutateFn cannot mutate object name and/or object namespace")
	}
	return nil
}

func PrintChange(prefix string, oldObj, newObj interface{}, logger logr.Logger) (bool, error) {
	oldByte, _ := json.Marshal(oldObj)
	newByte, _ := json.Marshal(newObj)

	patch, err := jsonpatch.CreatePatch(oldByte, newByte)
	if err != nil {
		return false, fmt.Errorf("creating JSON patch failed. %v", err)
	}

	if len(patch) == 0 {
		return false, nil
	}

	logger.Info(fmt.Sprintf("%v change: %+v", prefix, patch))
	return true, nil
}

func CreateOrUpdate(ctx context.Context, c client.Client, obj client.Object, f controllerutil.MutateFn, logger logr.Logger) (controllerutil.OperationResult, error) {
	key := client.ObjectKeyFromObject(obj)
	if err := c.Get(ctx, key, obj); err != nil {
		if !errors.IsNotFound(err) {
			return controllerutil.OperationResultNone, err
		}
		if err := mutate(f, key, obj); err != nil {
			return controllerutil.OperationResultNone, err
		}
		if err := c.Create(ctx, obj); err != nil {
			return controllerutil.OperationResultNone, err
		}
		return controllerutil.OperationResultCreated, nil
	}

	existing := obj.DeepCopyObject()
	if err := mutate(f, key, obj); err != nil {
		return controllerutil.OperationResultNone, err
	}

	if equality.Semantic.DeepEqual(existing, obj) {
		return controllerutil.OperationResultNone, nil
	}

	_, _ = PrintChange("", &existing, &obj, logger)

	if err := c.Update(ctx, obj); err != nil {
		return controllerutil.OperationResultNone, err
	}
	return controllerutil.OperationResultUpdated, nil
}
