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
	"bytes"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"gopkg.in/ini.v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/rabbitmq/cluster-operator/v2/internal/constant"
	"github.com/rabbitmq/cluster-operator/v2/internal/metadata"

	"github.com/rabbitmq/cluster-operator/v2/api/v1beta1"
)

const (
	DefaultUserSecretName = "default-user"
	bindingProvider       = "rabbitmq"
	bindingType           = "rabbitmq"
	usernamePrefix        = "default_user_"
)

type DefaultUserSecretBuilder struct {
	*RabbitmqResourceBuilder
}

func (builder *RabbitmqResourceBuilder) DefaultUserSecret() *DefaultUserSecretBuilder {
	return &DefaultUserSecretBuilder{builder}
}

func (builder *DefaultUserSecretBuilder) Build() (client.Object, error) {
	username, err := generateUsername(24)
	if err != nil {
		return nil, err
	}

	password, err := randomEncodedString(24)
	if err != nil {
		return nil, err
	}

	defaultUserConf, err := generateDefaultUserConf(username, password)
	if err != nil {
		return nil, err
	}

	// Default user secret implements the service binding Provisioned Service
	// See: https://k8s-service-bindings.github.io/spec/#provisioned-service
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        builder.Instance.ChildResourceName(DefaultUserSecretName),
			Namespace:   builder.Instance.Namespace,
			Labels:      metadata.GetLabels(builder.Instance.Name, builder.Instance.Labels),
			Annotations: metadata.ReconcileAndFilterAnnotations(nil, builder.Instance.Annotations),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"username":                       []byte(username),
			"password":                       []byte(password),
			fileNameRabbitmqConfdDefaultUser: defaultUserConf,
			"type":                           []byte(bindingType),
			"provider":                       []byte(bindingProvider),
			"host":                           []byte(builder.Instance.ServiceSubDomain()),
		},
	}
	builder.updatePorts(secret)
	builder.updateConnectionString(secret)

	return secret, nil
}

func (builder *DefaultUserSecretBuilder) UpdateMayRequireStsRecreate() bool {
	return false
}

func (builder *DefaultUserSecretBuilder) Update(object client.Object) error {
	secret := object.(*corev1.Secret)
	secret.Labels = metadata.GetLabels(builder.Instance.Name, builder.Instance.Labels)
	secret.Annotations = metadata.ReconcileAndFilterAnnotations(secret.GetAnnotations(), builder.Instance.Annotations)

	builder.updatePorts(secret)
	builder.updateConnectionString(secret)

	if err := controllerutil.SetControllerReference(builder.Instance, secret, builder.Scheme); err != nil {
		return fmt.Errorf("failed setting controller reference: %w", err)
	}

	return nil
}

func (builder *DefaultUserSecretBuilder) updatePorts(secret *corev1.Secret) {
	AMQPPort := fmt.Sprintf("%d", constant.DefaultPortAmqp)
	AMQPSPort := fmt.Sprintf("%d", constant.DefaultPortAmqps)

	portNames := map[v1beta1.Plugin]string{
		constant.PluginNameMqtt:     "mqtt-port",
		constant.PluginNameStomp:    "stomp-port",
		constant.PluginNameStream:   "stream-port",
		constant.PluginNameWebMqtt:  "web-mqtt-port",
		constant.PluginNameWebStomp: "web-stomp-port",
	}
	port := map[string]string{
		"mqtt-port":      fmt.Sprintf("%d", constant.DefaultPortMqtt),
		"stomp-port":     fmt.Sprintf("%d", constant.DefaultPortStomp),
		"stream-port":    fmt.Sprintf("%d", constant.DefaultPortStream),
		"web-mqtt-port":  fmt.Sprintf("%d", constant.DefaultPortWebMqtt),
		"web-stomp-port": fmt.Sprintf("%d", constant.DefaultPortWebStomp),
	}
	TLSPort := map[string]string{
		"mqtt-port":      fmt.Sprintf("%d", constant.DefaultPortMqtts),
		"stomp-port":     fmt.Sprintf("%d", constant.DefaultPortStomps),
		"stream-port":    fmt.Sprintf("%d", constant.DefaultPortStreams),
		"web-mqtt-port":  fmt.Sprintf("%d", constant.DefaultPortWebMqttTls),
		"web-stomp-port": fmt.Sprintf("%d", constant.DefaultPortWebStompTls),
	}

	if builder.Instance.SecretTLSEnabled() {
		secret.Data["port"] = []byte(AMQPSPort)

		for plugin, portName := range portNames {
			if builder.Instance.AdditionalPluginEnabled(plugin) {
				secret.Data[portName] = []byte(TLSPort[portName])
			} else {
				delete(secret.Data, portName)
			}
		}
	} else {
		secret.Data["port"] = []byte(AMQPPort)

		for plugin, portName := range portNames {
			if builder.Instance.AdditionalPluginEnabled(plugin) {
				secret.Data[portName] = []byte(port[portName])
			} else {
				delete(secret.Data, portName)
			}
		}
	}
}

func (builder *DefaultUserSecretBuilder) updateConnectionString(secret *corev1.Secret) {
	if builder.Instance.SecretTLSEnabled() {
		secret.Data["connection_string"] = []byte(fmt.Sprintf("amqps://%s:%s@%s:%s/", secret.Data["username"], secret.Data["password"], secret.Data["host"], secret.Data["port"]))
	} else {
		secret.Data["connection_string"] = []byte(fmt.Sprintf("amqp://%s:%s@%s:%s/", secret.Data["username"], secret.Data["password"], secret.Data["host"], secret.Data["port"]))
	}
}

// generateUsername returns a base64 string that has "default_user_" as prefix
// returned string has length 'l' when base64 decoded
func generateUsername(l int) (string, error) {
	encoded, err := randomEncodedString(l)
	if err != nil {
		return "", err
	}

	encodedSlice := []byte(encoded)
	return string(append([]byte(usernamePrefix), encodedSlice[0:len(encodedSlice)-len(usernamePrefix)]...)), nil
}

func generateDefaultUserConf(username, password string) ([]byte, error) {
	ini.PrettySection = false // Remove trailing new line because default_user.conf has only a default section.
	cfg, err := ini.Load([]byte{})
	if err != nil {
		return nil, err
	}
	defaultSection := cfg.Section("")

	if _, err := defaultSection.NewKey("default_user", username); err != nil {
		return nil, err
	}

	if _, err := defaultSection.NewKey("default_pass", password); err != nil {
		return nil, err
	}

	var userConfBuffer bytes.Buffer
	if _, err := cfg.WriteTo(&userConfBuffer); err != nil {
		return nil, err
	}

	return userConfBuffer.Bytes(), nil
}
