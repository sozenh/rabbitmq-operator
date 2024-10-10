package status

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mqv1beta1 "github.com/rabbitmq/cluster-operator/v2/api/v1beta1"
)

func ReconcileSuccessCondition(status corev1.ConditionStatus, reason, message string) mqv1beta1.RabbitmqClusterCondition {
	return mqv1beta1.RabbitmqClusterCondition{
		Type:               mqv1beta1.ReconcileSuccess,
		Status:             status,
		LastTransitionTime: metav1.Time{Time: time.Now()},
		Reason:             reason,
		Message:            message,
	}
}
