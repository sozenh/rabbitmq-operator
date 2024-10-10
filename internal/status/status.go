// RabbitMQ Cluster Operator
//
// Copyright 2020 VMware, Inc. All Rights Reserved.
//
// This product is licensed to you under the Mozilla Public license, Version 2.0 (the "License").  You may not use this product except in compliance with the Mozilla Public License.
//
// This product may include a number of subcomponents with separate copyright notices and license terms. Your use of these subcomponents is subject to the terms and conditions of the subcomponent's license, as noted in the LICENSE file.
//

package status

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	mqv1beta1 "github.com/rabbitmq/cluster-operator/v2/api/v1beta1"
)

type RabbitmqClusterCondition mqv1beta1.RabbitmqClusterCondition

func newRabbitmqClusterCondition(
	conditionType mqv1beta1.RabbitmqClusterConditionType) mqv1beta1.RabbitmqClusterCondition {
	return mqv1beta1.RabbitmqClusterCondition{
		Type:               conditionType,
		Status:             corev1.ConditionUnknown,
		LastTransitionTime: metav1.Time{},
	}
}

func SetCondition(
	clusterStatus *mqv1beta1.RabbitmqClusterStatus,
	condType mqv1beta1.RabbitmqClusterConditionType,
	condStatus corev1.ConditionStatus, reason string, messages ...string) {
	for i := range clusterStatus.Conditions {
		if clusterStatus.Conditions[i].Type == condType {
			clusterStatus.Conditions[i].UpdateState(condStatus)
			clusterStatus.Conditions[i].UpdateReason(reason, messages...)
			break
		}
	}
}

func SetConditions(
	resources []runtime.Object, clusterStatus *mqv1beta1.RabbitmqClusterStatus) {

	var oldAllReplicasReadyCondition *mqv1beta1.RabbitmqClusterCondition
	var oldClusterAvailableCondition *mqv1beta1.RabbitmqClusterCondition
	var oldNoWarningsCondition *mqv1beta1.RabbitmqClusterCondition
	var oldReconcileSuccessCondition *mqv1beta1.RabbitmqClusterCondition

	for _, condition := range clusterStatus.Conditions {
		switch condition.Type {
		case mqv1beta1.NoWarnings:
			oldNoWarningsCondition = condition.DeepCopy()
		case mqv1beta1.AllReplicasReady:
			oldAllReplicasReadyCondition = condition.DeepCopy()
		case mqv1beta1.ClusterAvailable:
			oldClusterAvailableCondition = condition.DeepCopy()
		case mqv1beta1.ReconcileSuccess:
			oldReconcileSuccessCondition = condition.DeepCopy()
		}
	}

	noWarningsCond := NoWarningsCondition(resources, oldNoWarningsCondition)
	allReplicasReadyCond := AllReplicasReadyCondition(resources, oldAllReplicasReadyCondition)
	clusterAvailableCond := ClusterAvailableCondition(resources, oldClusterAvailableCondition)

	var reconciledCondition mqv1beta1.RabbitmqClusterCondition
	if oldReconcileSuccessCondition != nil {
		reconciledCondition = *oldReconcileSuccessCondition
	} else {
		reconciledCondition = ReconcileSuccessCondition(corev1.ConditionUnknown, "Initialising", "")
	}

	clusterStatus.Conditions = []mqv1beta1.RabbitmqClusterCondition{
		allReplicasReadyCond,
		clusterAvailableCond,
		noWarningsCond,
		reconciledCondition,
	}
}
