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
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rabbitmqv1beta1 "github.com/rabbitmq/cluster-operator/v2/api/v1beta1"
)

type RabbitmqResourceBuilder struct {
	Scheme   *runtime.Scheme
	Replicas int
	Instance *rabbitmqv1beta1.RabbitmqCluster
}

type ResourceBuilder interface {
	Build() (client.Object, error)
	Update(client.Object) error
	UpdateMayRequireStsRecreate() bool
}

func (builder *RabbitmqResourceBuilder) ResourceBuilders() []ResourceBuilder {

	builders := []ResourceBuilder{
		builder.HeadlessService(),
		builder.Service(),
		builder.ErlangCookie(),
		builder.DefaultUserSecret(),
		builder.RabbitmqPluginsConfigMap(),
		builder.ServerConfigMap(),
		builder.ServiceAccount(),
		builder.Role(),
		builder.RoleBinding(),
	}
	for i := 0; i < builder.Replicas; i++ {
		builders = append(builders, builder.StatefulSet(i))
	}
	if builder.Instance.VaultDefaultUserSecretEnabled() || builder.Instance.ExternalSecretEnabled() {
		// do not create default-user K8s Secret
		builders = append(builders[:3], builders[3+1:]...)
	}
	return builders
}
