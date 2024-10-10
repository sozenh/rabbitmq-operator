// RabbitMQ Cluster Operator
//
// Copyright 2020 VMware, Inc. All Rights Reserved.
//
// This product is licensed to you under the Mozilla Public license, Version 2.0 (the "License").  You may not use this product except in compliance with the Mozilla Public License.
//
// This product may include a number of subcomponents with separate copyright notices and license terms. Your use of these subcomponents is subject to the terms and conditions of the subcomponent's license, as noted in the LICENSE file.
//

package resource

import (
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/rabbitmq/cluster-operator/v2/internal/constant"
	"github.com/rabbitmq/cluster-operator/v2/internal/metadata"
)

type ServiceBuilder struct {
	*RabbitmqResourceBuilder
}

func (builder *RabbitmqResourceBuilder) Service() *ServiceBuilder {
	return &ServiceBuilder{builder}
}

func (builder *ServiceBuilder) Build() (client.Object, error) {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        builder.Instance.ChildResourceName(constant.ResourceClientServiceSuffix),
			Namespace:   builder.Instance.Namespace,
			Labels:      metadata.GetLabels(builder.Instance.Name, builder.Instance.Labels),
			Annotations: metadata.ReconcileAndFilterAnnotations(nil, builder.Instance.Annotations),
		},
	}, nil
}

func (builder *ServiceBuilder) UpdateMayRequireStsRecreate() bool {
	return false
}

func (builder *ServiceBuilder) Update(object client.Object) error {
	service := object.(*corev1.Service)
	service.Labels = metadata.GetLabels(builder.Instance.Name, builder.Instance.Labels)
	service.Annotations = metadata.ReconcileAndFilterAnnotations(service.Annotations, builder.Instance.Annotations)
	if builder.Instance.Spec.Service.Annotations != nil {
		service.Annotations = metadata.ReconcileAnnotations(service.Annotations, builder.Instance.Spec.Service.Annotations)
	}

	service.Spec.Ports = builder.Ports(service.Spec.Ports)

	service.Spec.Type = builder.Instance.Spec.Service.Type
	service.Spec.Selector = metadata.LabelSelector(builder.Instance.Name)
	service.Spec.IPFamilyPolicy = builder.Instance.Spec.Service.IPFamilyPolicy

	if builder.Instance.Spec.Service.Type == "ClusterIP" || builder.Instance.Spec.Service.Type == "" {
		for i := range service.Spec.Ports {
			service.Spec.Ports[i].NodePort = int32(0)
		}
	}

	if builder.Instance.Spec.Override.Service != nil {
		if err := applySvcOverride(service, builder.Instance.Spec.Override.Service); err != nil {
			return fmt.Errorf("failed applying Service override: %w", err)
		}
	}

	if err := controllerutil.SetControllerReference(builder.Instance, service, builder.Scheme); err != nil {
		return fmt.Errorf("failed setting controller reference: %w", err)
	}

	return nil
}

func (builder *ServiceBuilder) Ports(currPorts []corev1.ServicePort) []corev1.ServicePort {
	return mergePorts(currPorts, constant.NewPortManager(builder.Instance).ClientServicePorts())
}
