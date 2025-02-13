// RabbitMQ Cluster Operator
//
// Copyright 2020 VMware, Inc. All Rights Reserved.
//
// This product is licensed to you under the Mozilla Public license, Version 2.0 (the "License").  You may not use this product except in compliance with the Mozilla Public License.
//
// This product may include a number of subcomponents with separate copyright notices and license terms. Your use of these subcomponents is subject to the terms and conditions of the subcomponent's license, as noted in the LICENSE file.
//

package metadata

import (
	"strings"
)

type label map[string]string

func Label(instanceName string) label {
	return label{
		"app.kubernetes.io/name":      instanceName,
		"app.kubernetes.io/component": "rabbitmq",
		"app.kubernetes.io/part-of":   "rabbitmq",
	}
}

func GetLabels(instanceName string, instanceLabels map[string]string) label {
	allLabels := Label(instanceName)

	for label, value := range instanceLabels {
		if !strings.HasPrefix(label, "app.kubernetes.io") {
			allLabels[label] = value
		}
	}

	return allLabels
}

func LabelSelector(instanceName string) label {
	return label{
		"app.kubernetes.io/name": instanceName,
	}
}

func ReconcileLabels(existing map[string]string, defaults ...map[string]string) map[string]string {
	return mergeWithFilter(func(k string) bool { return true }, existing, defaults...)
}

func ReconcileAndFilterLabels(existing map[string]string, defaults ...map[string]string) map[string]string {
	return mergeWithFilter(isNotKubernetesAnnotation, existing, defaults...)
}
