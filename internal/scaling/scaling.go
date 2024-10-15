package scaling

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"

	rabbitmqv1beta1 "github.com/rabbitmq/cluster-operator/v2/api/v1beta1"
)

type PersistenceScaler struct {
	Client kubernetes.Interface
}

func NewPersistenceScaler(client kubernetes.Interface) PersistenceScaler {
	return PersistenceScaler{Client: client}
}

func (p PersistenceScaler) Scale(
	ctx context.Context,
	rmq rabbitmqv1beta1.RabbitmqCluster,
	pvcName string, statefulsetKey types.NamespacedName, desiredCapacity k8sresource.Quantity) error {
	logger := ctrl.LoggerFrom(ctx)
	existingCapacity, err := p.existingCapacity(ctx, pvcName, statefulsetKey)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to determine existing STS capactiy: %w", err)
	}

	if existingCapacity.Cmp(desiredCapacity) == 0 {
		return nil
	}

	// desired storage capacity is smaller than the current capacity; we can't proceed lest we lose data
	if existingCapacity.Cmp(desiredCapacity) == 1 {
		return errors.New("shrinking persistent volumes is not supported")
	}

	// don't allow going from 0 (no PVC) to anything else
	if existingCapacity.Cmp(k8sresource.MustParse("0Gi")) == 0 {
		return errors.New("changing from ephemeral to persistent storage is not supported")
	}

	existingPVCs, err := p.getPVCs(ctx, pvcName, statefulsetKey)
	if err != nil {
		return fmt.Errorf("failed to retrieve the existing PVC %s for %s", pvcName, statefulsetKey)
	}

	pvcsToBeScaled := p.pvcsNeedingScaling(existingPVCs, desiredCapacity)
	if len(pvcsToBeScaled) == 0 {
		return nil
	}
	logger.Info("scaling up PVCs", "RabbitmqCluster", rmq.Name, "pvcsToBeScaled", pvcsToBeScaled)

	if err := p.deleteStatefulset(ctx, statefulsetKey); err != nil {
		return fmt.Errorf("failed to delete Statefulset from Kubernetes API: %w", err)
	}

	return p.scaleUpPVCs(ctx, statefulsetKey, pvcsToBeScaled, desiredCapacity)
}

func (p PersistenceScaler) getPVCs(
	ctx context.Context, pvcName string, statefulsetKey types.NamespacedName) ([]*corev1.PersistentVolumeClaim, error) {
	logger := ctrl.LoggerFrom(ctx)

	var pvcs []*corev1.PersistentVolumeClaim

	statefulset, err := p.getStatefulset(ctx, statefulsetKey)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get sts from Kubernetes API: %w", err)
	}

	var i int32
	for i = 0; i < *statefulset.Spec.Replicas; i++ {
		pvc, err := p.Client.
			CoreV1().
			PersistentVolumeClaims(statefulsetKey.Namespace).
			Get(ctx, fmt.Sprintf("%s-%s-%d", pvcName, statefulset.Name, i), metav1.GetOptions{})

		if err == nil {
			pvcs = append(pvcs, pvc)
			continue
		}

		if k8serrors.IsNotFound(err) {
			continue
		}
		return nil, fmt.Errorf("failed to get sts from Kubernetes API: %w", err)
	}
	if len(pvcs) > 0 {
		logger.V(1).Info("found existing PVCs", "pvcList", pvcs)
	}
	return pvcs, nil
}

func (p PersistenceScaler) pvcsNeedingScaling(existingPVCs []*corev1.PersistentVolumeClaim, desiredCapacity k8sresource.Quantity) []*corev1.PersistentVolumeClaim {
	var pvcs []*corev1.PersistentVolumeClaim

	for _, pvc := range existingPVCs {
		existingCapacity := pvc.Spec.Resources.Requests[corev1.ResourceStorage]

		// desired storage capacity is larger than the current capacity; PVC needs expansion
		if existingCapacity.Cmp(desiredCapacity) == -1 {
			pvcs = append(pvcs, pvc)
		}
	}
	return pvcs
}

func (p PersistenceScaler) getStatefulset(ctx context.Context, statefulsetKey types.NamespacedName) (*appsv1.StatefulSet, error) {
	return p.Client.AppsV1().StatefulSets(statefulsetKey.Namespace).Get(ctx, statefulsetKey.Name, metav1.GetOptions{})
}

func (p PersistenceScaler) existingCapacity(ctx context.Context, pvcName string, statefulsetKey types.NamespacedName) (k8sresource.Quantity, error) {
	sts, err := p.getStatefulset(ctx, statefulsetKey)
	if err != nil {
		return k8sresource.MustParse("0"), err
	}

	for _, t := range sts.Spec.VolumeClaimTemplates {
		if t.Name == pvcName {
			return t.Spec.Resources.Requests[corev1.ResourceStorage], nil
		}
	}
	return k8sresource.MustParse("0"), nil
}

// deleteSts deletes a sts without deleting pods and PVCs
// using DeletePropagationPolicy set to 'Orphan'
func (p PersistenceScaler) deleteStatefulset(ctx context.Context, statefulsetKey types.NamespacedName) error {
	logger := ctrl.LoggerFrom(ctx)
	logger.Info("deleting statefulSet (pods won't be deleted)", "statefulSet", statefulsetKey.Name)

	deletePropagationPolicy := metav1.DeletePropagationOrphan
	if err := p.Client.
		AppsV1().
		StatefulSets(statefulsetKey.Namespace).
		Delete(ctx, statefulsetKey.Name, metav1.DeleteOptions{PropagationPolicy: &deletePropagationPolicy}); err != nil {
		return fmt.Errorf("failed to delete statefulSet %s: %w", statefulsetKey.Name, err)
	}

	err := retryWithInterval(
		logger,
		"delete statefulSet",
		10, 3*time.Second,
		func() bool {
			_, getErr := p.Client.
				AppsV1().
				StatefulSets(statefulsetKey.Namespace).Get(ctx, statefulsetKey.Name, metav1.GetOptions{})
			return k8serrors.IsNotFound(getErr)
		},
	)
	if err != nil {
		return fmt.Errorf("statefulSet not deleting after 30 seconds %s: %w", statefulsetKey.Name, err)
	}

	logger.Info("statefulSet deleted", "statefulSet", statefulsetKey.Name)
	return nil
}

func (p PersistenceScaler) scaleUpPVCs(ctx context.Context, statefulsetKey types.NamespacedName, pvcs []*corev1.PersistentVolumeClaim, desiredCapacity k8sresource.Quantity) error {
	logger := ctrl.LoggerFrom(ctx)

	for _, pvc := range pvcs {
		// To minimise any timing windows, retrieve the latest version of this PVC before updating
		pvc, err := p.Client.
			CoreV1().
			PersistentVolumeClaims(pvc.Namespace).Get(ctx, pvc.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get PVC from Kubernetes API: %w", err)
		}

		pvc.Spec.Resources.Requests[corev1.ResourceStorage] = desiredCapacity
		_, err = p.Client.
			CoreV1().
			PersistentVolumeClaims(statefulsetKey.Namespace).Update(ctx, pvc, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update PersistentVolumeClaim %s: %w", pvc.Name, err)
		}

		logger.Info("successfully scaled up PVC", "PersistentVolumeClaim", pvc.Name, "newCapacity", desiredCapacity)
	}

	return nil
}

func retryWithInterval(logger logr.Logger, msg string, retry int, interval time.Duration, f func() bool) (err error) {
	for i := 0; i < retry; i++ {
		if ok := f(); ok {
			return
		}
		time.Sleep(interval)
		logger.V(1).Info("retrying again", "action", msg, "interval", interval, "attempt", i+1)
	}
	return fmt.Errorf("failed to %s after %d retries", msg, retry)
}
