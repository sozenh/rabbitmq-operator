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
	"strconv"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"gopkg.in/ini.v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/rabbitmq/cluster-operator/v2/internal/constant"
	"github.com/rabbitmq/cluster-operator/v2/internal/metadata"
)

const (
	disableNonTLSListeners = "none"

	defaultRabbitmqConf = `
log.file = /var/log/rabbitmq/rabbitmq.log
queue_master_locator = min-masters
disk_free_limit.absolute = 2GB
cluster_partition_handling = pause_minority
cluster_formation.peer_discovery_backend = rabbit_peer_discovery_k8s
cluster_formation.k8s.host = kubernetes.default
cluster_formation.k8s.address_type = hostname`

	defaultTLSConf = `
ssl_options.certfile = /etc/rabbitmq-tls/tls.crt
ssl_options.keyfile = /etc/rabbitmq-tls/tls.key
listeners.ssl.default = 5671

management.ssl.certfile   = /etc/rabbitmq-tls/tls.crt
management.ssl.keyfile    = /etc/rabbitmq-tls/tls.key
management.ssl.port       = 15671

prometheus.ssl.certfile  = /etc/rabbitmq-tls/tls.crt
prometheus.ssl.keyfile   = /etc/rabbitmq-tls/tls.key
prometheus.ssl.port      = 15691
`
)

type ServerConfigMapBuilder struct {
	*RabbitmqResourceBuilder
	UpdateRequiresStsRestart bool
}

func (builder *RabbitmqResourceBuilder) ServerConfigMap() *ServerConfigMapBuilder {
	return &ServerConfigMapBuilder{builder, true}
}

func (builder *ServerConfigMapBuilder) Build() (client.Object, error) {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        builder.Instance.ChildResourceName(constant.ResourceServerConfigMapSuffix),
			Namespace:   builder.Instance.Namespace,
			Labels:      metadata.GetLabels(builder.Instance.Name, builder.Instance.Labels),
			Annotations: metadata.ReconcileAndFilterAnnotations(nil, builder.Instance.Annotations),
		},
	}, nil
}

func (builder *ServerConfigMapBuilder) UpdateMayRequireStsRecreate() bool {
	return false
}

func (builder *ServerConfigMapBuilder) Update(object client.Object) error {
	configMap := object.(*corev1.ConfigMap)
	previousConfigMap := configMap.DeepCopy()

	configMap.Labels = metadata.GetLabels(builder.Instance.Name, builder.Instance.Labels)
	configMap.Annotations = metadata.ReconcileAndFilterAnnotations(configMap.GetAnnotations(), builder.Instance.Annotations)

	ini.PrettySection = false // Remove trailing new line because rabbitmq.conf has only a default section.
	operatorConfiguration, err := ini.Load([]byte(defaultRabbitmqConf))
	if err != nil {
		return err
	}
	defaultSection := operatorConfiguration.Section("")

	if _, err = defaultSection.NewKey(
		"cluster_formation.target_cluster_size_hint",
		strconv.Itoa(int(*builder.Instance.Spec.Replicas))); err != nil {
		return err
	}

	if _, err = defaultSection.NewKey("cluster_name", builder.Instance.Name); err != nil {
		return err
	}

	rmqProperties := builder.Instance.Spec.Rabbitmq
	authMechsConfigured, err := areAuthMechanismsConfigued(rmqProperties.AdditionalConfig)
	if err != nil {
		return err
	}
	// By default, RabbitMQ configures the following SASL mechanisms:
	// auth_mechanisms.1 = PLAIN
	// auth_mechanisms.2 = AMQPLAIN
	// auth_mechanisms.3 = ANONYMOUS
	if !authMechsConfigured {
		// Since the user didn't explicitly configure auth mechanisms, we disable
		// ANONYMOUS logins because they should be disabled in production.
		if _, err = defaultSection.NewKey("auth_mechanisms.1", "PLAIN"); err != nil {
			return err
		}
		if _, err = defaultSection.NewKey("auth_mechanisms.2", "AMQPLAIN"); err != nil {
			return err
		}
	}

	userConfiguration := ini.Empty()
	userConfigurationSection := userConfiguration.Section("")

	if builder.Instance.TLSEnabled() {
		if err = userConfiguration.Append([]byte(defaultTLSConf)); err != nil {
			return err
		}
		if builder.Instance.DisableNonTLSListeners() {
			if _, err = userConfigurationSection.NewKey("listeners.tcp", disableNonTLSListeners); err != nil {
				return err
			}
		} else {
			// management plugin does not have a *.listeners.tcp settings like other plugins
			// management tcp listener can be disabled by setting management.ssl.port without setting management.tcp.port
			// we set management tcp listener only if tls is enabled and disableNonTLSListeners is false
			if _, err = userConfigurationSection.NewKey("management.tcp.port", fmt.Sprintf("%d", constant.DefaultPortManagement)); err != nil {
				return err
			}
			if _, err = userConfigurationSection.NewKey("prometheus.tcp.port", fmt.Sprintf("%d", constant.DefaultPortPrometheus)); err != nil {
				return err
			}
		}
		if builder.Instance.AdditionalPluginEnabled(constant.PluginNameMqtt) {
			if builder.Instance.DisableNonTLSListeners() {
				if _, err = userConfigurationSection.NewKey("mqtt.listeners.tcp", disableNonTLSListeners); err != nil {
					return err
				}
			}
			if _, err = userConfigurationSection.NewKey("mqtt.listeners.ssl.default", fmt.Sprintf("%d", constant.DefaultPortMqtts)); err != nil {
				return err
			}
		}
		if builder.Instance.AdditionalPluginEnabled(constant.PluginNameStomp) {
			if builder.Instance.DisableNonTLSListeners() {
				if _, err = userConfigurationSection.NewKey("stomp.listeners.tcp", disableNonTLSListeners); err != nil {
					return err
				}
			}
			if _, err = userConfigurationSection.NewKey("stomp.listeners.ssl.1", fmt.Sprintf("%d", constant.DefaultPortStomps)); err != nil {
				return err
			}
		}
		if builder.Instance.AdditionalPluginEnabled(constant.PluginNameStream) {
			if builder.Instance.DisableNonTLSListeners() {
				if _, err = userConfigurationSection.NewKey("stream.listeners.tcp", disableNonTLSListeners); err != nil {
					return err
				}
			}
			if _, err = userConfigurationSection.NewKey("stream.listeners.ssl.default", fmt.Sprintf("%d", constant.DefaultPortStreams)); err != nil {
				return err
			}
		}
	}

	if builder.Instance.MutualTLSEnabled() {
		if _, err := userConfigurationSection.NewKey("ssl_options.cacertfile", filePathCaCert); err != nil {
			return err
		}
		if _, err := userConfigurationSection.NewKey("ssl_options.verify", "verify_peer"); err != nil {
			return err
		}

		if _, err := userConfigurationSection.NewKey("management.ssl.cacertfile", filePathCaCert); err != nil {
			return err
		}

		if _, err := userConfigurationSection.NewKey("prometheus.ssl.cacertfile", filePathCaCert); err != nil {
			return err
		}

		if builder.Instance.AdditionalPluginEnabled(constant.PluginNameWebMqtt) {
			if _, err := userConfigurationSection.NewKey("web_mqtt.ssl.cacertfile", filePathCaCert); err != nil {
				return err
			}
			if _, err := userConfigurationSection.NewKey("web_mqtt.ssl.certfile", filePathTlsCert); err != nil {
				return err
			}
			if _, err := userConfigurationSection.NewKey("web_mqtt.ssl.keyfile", filePathTlsKey); err != nil {
				return err
			}

			if builder.Instance.DisableNonTLSListeners() {
				if _, err := userConfigurationSection.NewKey("web_mqtt.tcp.listener", "none"); err != nil {
					return err
				}
			}
			if _, err := userConfigurationSection.NewKey("web_mqtt.ssl.port", fmt.Sprintf("%d", constant.DefaultPortWebMqttTls)); err != nil {
				return err
			}
		}
		if builder.Instance.AdditionalPluginEnabled(constant.PluginNameWebStomp) {
			if _, err := userConfigurationSection.NewKey("web_stomp.ssl.cacertfile", filePathCaCert); err != nil {
				return err
			}
			if _, err := userConfigurationSection.NewKey("web_stomp.ssl.certfile", filePathTlsCert); err != nil {
				return err
			}
			if _, err := userConfigurationSection.NewKey("web_stomp.ssl.keyfile", filePathTlsKey); err != nil {
				return err
			}

			if builder.Instance.DisableNonTLSListeners() {
				if _, err := userConfigurationSection.NewKey("web_stomp.tcp.listener", "none"); err != nil {
					return err
				}
			}
			if _, err := userConfigurationSection.NewKey("web_stomp.ssl.port", fmt.Sprintf("%d", constant.DefaultPortWebStompTls)); err != nil {
				return err
			}
		}
	}

	if builder.Instance.MemoryLimited() {
		if _, err := userConfigurationSection.NewKey("total_memory_available_override_value", fmt.Sprintf("%d", removeHeadroom(builder.Instance.Spec.Resources.Limits.Memory().Value()))); err != nil {
			return err
		}
	}

	var rmqConfBuffer strings.Builder
	if _, err := operatorConfiguration.WriteTo(&rmqConfBuffer); err != nil {
		return err
	}

	if configMap.Data == nil {
		configMap.Data = make(map[string]string)
	}

	configMap.Data[fileNameRabbitmqConfdDefault] = rmqConfBuffer.String()

	rmqConfBuffer.Reset()

	if err := userConfiguration.Append([]byte(rmqProperties.AdditionalConfig)); err != nil {
		return fmt.Errorf("failed to append spec.rabbitmq.additionalConfig: %w", err)
	}

	if _, err := userConfiguration.WriteTo(&rmqConfBuffer); err != nil {
		return err
	}

	configMap.Data[fileNameRabbitmqConfdUserDefined] = rmqConfBuffer.String()

	updateProperty(configMap.Data, fileNameRabbitmqConfAdvanced, rmqProperties.AdvancedConfig)
	updateProperty(configMap.Data, fileNameRabbitmqConfEnv, rmqProperties.EnvConfig)
	updateProperty(configMap.Data, fileNameRabbitmqConfErlangInet, rmqProperties.ErlangInetConfig)

	if err := controllerutil.SetControllerReference(builder.Instance, configMap, builder.Scheme); err != nil {
		return fmt.Errorf("failed setting controller reference: %w", err)
	}

	updatedConfigMap := configMap.DeepCopy()
	if err := removeConfigNotRequiringNodeRestart(previousConfigMap); err != nil {
		return err
	}
	if err := removeConfigNotRequiringNodeRestart(updatedConfigMap); err != nil {
		return err
	}
	if equality.Semantic.DeepEqual(previousConfigMap, updatedConfigMap) {
		builder.UpdateRequiresStsRestart = false
	}

	return nil
}

// removeConfigNotRequiringNodeRestart removes configuration data that does not require a restart of RabbitMQ nodes.
// For example, the target cluster size hint changes after adding nodes to a cluster, but there's no reason
// to restart already running nodes.
func removeConfigNotRequiringNodeRestart(configMap *corev1.ConfigMap) error {
	operatorConf := configMap.Data[fileNameRabbitmqConfdDefault]
	if operatorConf == "" {
		return nil
	}
	conf, err := ini.Load([]byte(operatorConf))
	if err != nil {
		return fmt.Errorf("failed to load operatorDefaults.conf when deciding whether to restart STS: %w", err)
	}
	defaultSection := conf.Section("")
	for _, key := range defaultSection.KeyStrings() {
		if strings.HasPrefix(key, "cluster_formation.target_cluster_size_hint") {
			defaultSection.DeleteKey(key)
		}
	}
	var b strings.Builder
	if _, err := conf.WriteTo(&b); err != nil {
		return fmt.Errorf("failed to write operatorDefaults.conf when deciding whether to restart STS: %w", err)
	}
	configMap.Data[fileNameRabbitmqConfdDefault] = b.String()
	return nil
}

func updateProperty(configMapData map[string]string, key string, value string) {
	if value == "" {
		delete(configMapData, key)
	} else {
		configMapData[key] = value
	}
}

// The Erlang VM needs headroom above Rabbit to avoid being OOM killed
// We set the headroom to be the smaller amount of 20% memory or 2GiB
func removeHeadroom(memLimit int64) int64 {
	const GiB int64 = 1073741824
	if memLimit/5 > 2*GiB {
		return memLimit - 2*GiB
	}
	return memLimit - memLimit/5
}

func areAuthMechanismsConfigued(additionalConfig string) (bool, error) {
	iniFile, err := ini.Load([]byte(additionalConfig))
	if err != nil {
		return false, fmt.Errorf("failed to load spec.rabbitmq.additionalConfig: %w", err)
	}

	section := iniFile.Section("")
	for _, key := range section.KeyStrings() {
		if strings.HasPrefix(key, "auth_mechanisms") {
			return true, nil
		}
	}
	return false, nil
}
