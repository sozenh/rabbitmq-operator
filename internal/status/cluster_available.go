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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	mqv1beta1 "github.com/rabbitmq/cluster-operator/v2/api/v1beta1"
)

func ClusterAvailableCondition(
	resources []runtime.Object,
	oldCondition *mqv1beta1.RabbitmqClusterCondition) mqv1beta1.RabbitmqClusterCondition {

	condition := newRabbitmqClusterCondition(mqv1beta1.ClusterAvailable)
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
		case *corev1.Endpoints:
			if resource == nil {
				condition.Status = corev1.ConditionUnknown
				condition.Reason = "CouldNotRetrieveEndpoints"
				condition.Message = "Could not verify available service endpoints"
				continue
			}

			for _, subset := range resource.Subsets {
				if len(subset.Addresses) > 0 {
					condition.Status = corev1.ConditionTrue
					condition.Reason = "AtLeastOneEndpointAvailable"
					continue
				}
			}

			condition.Status = corev1.ConditionFalse
			condition.Reason = "NoEndpointsAvailable"
			condition.Message = "The service has no endpoints available"
		}
	}

	return condition
}
