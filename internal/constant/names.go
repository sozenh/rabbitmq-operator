package constant

import (
	"strconv"
	"strings"
)

const (
	PVCName               = "persistence"
	VolumeNamePersistence = "persistence"
	VolumeNamePodInfo     = "pod-info"

	VolumeNameRabbitmqTLS          = "rabbitmq-tls"
	VolumeNameRabbitmqConfd        = "rabbitmq-confd"
	VolumeNameRabbitmqServerConf   = "server-conf"
	VolumeNameRabbitmqPlugins      = "rabbitmq-plugins"
	VolumeNameRabbitmqErlangCookie = "rabbitmq-erlang-cookie"

	VolumeNamePluginsConf        = "plugins-conf"
	VolumeNameErlangCookieSecret = "erlang-cookie-secret"
)

const (
	ContainerNameSetup                 = "setup-container"
	ContainerNameRabbitMQ              = "rabbitmq"
	ContainerNameUserCredentialUpdater = "default-user-credential-updater"
)

const (
	ResourceStatefulsetSuffix = "server"

	ResourceClientServiceSuffix   = ""
	ResourceHeadlessServiceSuffix = "nodes"

	ResourceRoleBindingSuffix    = "server"
	ResourceServiceAccountSuffix = "server"
	ResourceRoleSuffix           = "peer-discovery"

	ResourceErlangCookieSuffix    = "erlang-cookie"
	ResourceServerConfigMapSuffix = "server-conf"
	ResourcePluginConfigMapSuffix = "plugins-conf"
)

const (
	PluginNameMqtt    = "rabbitmq_mqtt"
	PluginNameWebMqtt = "rabbitmq_web_mqtt"

	PluginNameStomp    = "rabbitmq_stomp"
	PluginNameWebStomp = "rabbitmq_web_stomp"

	PluginNamePrometheus = "rabbitmq_prometheus"
	PluginNameManagement = "rabbitmq_management"
	PluginNameKubernetes = "rabbitmq_peer_discovery_k8s"

	PluginNameStream                   = "rabbitmq_stream"
	PluginNameStreamManagement         = "rabbitmq_stream_management"
	PluginNameStreamMultiDCReplication = "rabbitmq_multi_dc_replication"
)

func GetPVCName(name string, replica int) string {
	return strings.Join([]string{PVCName, name, ResourceStatefulsetSuffix, strconv.Itoa(replica)}, "-")
}
