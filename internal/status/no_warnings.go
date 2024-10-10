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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	mqv1beta1 "github.com/rabbitmq/cluster-operator/v2/api/v1beta1"
)

func NoWarningsCondition(
	resources []runtime.Object,
	oldCondition *mqv1beta1.RabbitmqClusterCondition) mqv1beta1.RabbitmqClusterCondition {
	condition := newRabbitmqClusterCondition(mqv1beta1.NoWarnings)
	if oldCondition != nil {
		condition.LastTransitionTime = oldCondition.LastTransitionTime
	}
	defer func() {
		if oldCondition != nil {
			if oldCondition.Status != condition.Status ||
				oldCondition.Reason != condition.Reason ||
				oldCondition.Message != condition.Message {
				condition.LastTransitionTime = metav1.Time{Time: time.Now()}
			}
		}
	}()

	for _, res := range resources {
		switch resource := res.(type) {
		case *appsv1.StatefulSet:
			if resource == nil {
				condition.Status = corev1.ConditionUnknown
				condition.Reason = "MissingStatefulSet"
				condition.Message = "Could not find StatefulSet"
				continue
			}

			if !equality.Semantic.DeepEqual(resource.Spec.Template.Spec.Containers[0].Resources.Limits["memory"], resource.Spec.Template.Spec.Containers[0].Resources.Requests["memory"]) {
				condition.Status = corev1.ConditionFalse
				condition.Reason = "MemoryRequestAndLimitDifferent"
				condition.Message = "RabbitMQ container memory resource request and limit must be equal"
				continue
			}

			condition.Status = corev1.ConditionTrue
			condition.Reason = "NoWarnings"
		}
	}

	return condition
}
