package constant

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	mqv1beta1 "github.com/rabbitmq/cluster-operator/v2/api/v1beta1"
)

const (
	PortNameEpmd          = "epmd"
	DefaultPortEpmd int32 = 4369

	PortNameRPC          = "cluster-rpc"
	DefaultPortRPC int32 = 25672

	PortNameAmqp           = "amqp"
	DefaultPortAmqp  int32 = 5672
	PortNameAmqps          = "amqps"
	DefaultPortAmqps int32 = 5671

	PortNameStream           = "stream"
	DefaultPortStream  int32 = 5552
	PortNameStreams          = "streams"
	DefaultPortStreams int32 = 5551

	PortNameManagement             = "management"
	DefaultPortManagement    int32 = 15672
	PortNameManagementTls          = "management-tls"
	DefaultPortManagementTls int32 = 15671

	PortNameMqtt                = "mqtt"
	DefaultPortMqtt       int32 = 1883
	PortNameMqtts               = "mqtts"
	DefaultPortMqtts      int32 = 8883
	PortNameWebMqtt             = "web-mqtt"
	DefaultPortWebMqtt    int32 = 15675
	PortNameWebMqttTls          = "web-mqtt-tls"
	DefaultPortWebMqttTls int32 = 15676

	PortNameStomp                = "stomp"
	DefaultPortStomp       int32 = 61613
	PortNameStomps               = "stomps"
	DefaultPortStomps      int32 = 61614
	PortNameWebStomp             = "web-stomp"
	DefaultPortWebStomp    int32 = 15674
	PortNameWebStompTls          = "web-stomp-tls"
	DefaultPortWebStompTls int32 = 15673

	PortNamePrometheus             = "prometheus"
	DefaultPortPrometheus    int32 = 15692
	PortNamePrometheusTls          = "prometheus-tls"
	DefaultPortPrometheusTls int32 = 15691
)

type PortManager struct {
	EnableTls       bool
	DisableNonTls   bool
	EnableMutualTls bool
	Plugins         []string
}

func NewPortManager(rmq *mqv1beta1.RabbitmqCluster) *PortManager {
	portManager := PortManager{
		Plugins:         []string{},
		EnableTls:       rmq.TLSEnabled(),
		DisableNonTls:   rmq.DisableNonTLSListeners(),
		EnableMutualTls: rmq.MutualTLSEnabled(),
	}
	for _, plugin := range rmq.Spec.Rabbitmq.AdditionalPlugins {
		portManager.Plugins = append(portManager.Plugins, string(plugin))
	}

	return &portManager
}

func (m *PortManager) RabbitMQPorts() []corev1.ServicePort {
	return MergePorts(
		m.ClientServicePorts(),
		m.HeadlessServicePorts(),
	)
}

func (m *PortManager) LBServicePorts() []corev1.ServicePort {
	return MergePorts(
		m.clientAmqpPorts(),
		m.clientHttpPorts(),
		m.clientMqttPorts(),
		m.clientWebMqttPorts(),
		m.clientStompPorts(),
		m.clientWebStompPorts(),
		m.clientStreamPorts(),
	)
}

func (m *PortManager) ClientServicePorts() []corev1.ServicePort {
	return MergePorts(
		m.clientAmqpPorts(),
		m.clientHttpPorts(),
		m.clientMqttPorts(),
		m.clientWebMqttPorts(),
		m.clientStompPorts(),
		m.clientWebStompPorts(),
		m.clientStreamPorts(),
		m.clientPrometheusPorts(),
	)
}

func (m *PortManager) HeadlessServicePorts() []corev1.ServicePort {
	return []corev1.ServicePort{
		{
			Protocol:   corev1.ProtocolTCP,
			Name:       PortNameEpmd,
			Port:       DefaultPortEpmd,
			TargetPort: intstr.FromInt32(DefaultPortEpmd),
		},
		{
			Protocol:   corev1.ProtocolTCP,
			Name:       PortNameRPC, // aka distribution port
			Port:       DefaultPortRPC,
			TargetPort: intstr.FromInt32(DefaultPortRPC),
		},
	}
}

func (m *PortManager) pluginEnabled(plugin string) bool {
	for _, p := range m.Plugins {
		if p == plugin {
			return true
		}
	}
	return false
}

func (m *PortManager) clientAmqpPorts() []corev1.ServicePort {
	var ports []corev1.ServicePort

	if !m.DisableNonTls {
		ports = append(ports,
			corev1.ServicePort{
				Name:       PortNameAmqp,
				Port:       DefaultPortAmqp,
				TargetPort: intstr.FromInt32(DefaultPortAmqp),

				Protocol:    corev1.ProtocolTCP,
				AppProtocol: ptr.To("amqp"),
			},
		)
	}

	if m.EnableTls {
		ports = append(ports,
			corev1.ServicePort{
				Name:       PortNameAmqps,
				Port:       DefaultPortAmqps,
				TargetPort: intstr.FromInt32(DefaultPortAmqps),

				Protocol:    corev1.ProtocolTCP,
				AppProtocol: ptr.To("amqps"),
			},
		)
	}

	return ports
}

func (m *PortManager) clientHttpPorts() []corev1.ServicePort {
	var ports []corev1.ServicePort

	if !m.DisableNonTls {
		ports = append(ports,
			corev1.ServicePort{
				Name:       PortNameManagement,
				Port:       DefaultPortManagement,
				TargetPort: intstr.FromInt32(DefaultPortManagement),

				Protocol:    corev1.ProtocolTCP,
				AppProtocol: ptr.To("http"),
			},
		)
	}

	if m.EnableTls {
		ports = append(ports,
			corev1.ServicePort{
				Name:       PortNameManagementTls,
				Port:       DefaultPortManagementTls,
				TargetPort: intstr.FromInt32(DefaultPortManagementTls),

				Protocol:    corev1.ProtocolTCP,
				AppProtocol: ptr.To("https"),
			},
		)
	}

	return ports
}

func (m *PortManager) clientMqttPorts() []corev1.ServicePort {
	var ports []corev1.ServicePort

	if !m.pluginEnabled(PluginNameMqtt) {
		return ports
	}

	if !m.DisableNonTls {
		ports = append(ports,
			corev1.ServicePort{
				Name:       PortNameMqtt,
				Port:       DefaultPortMqtt,
				TargetPort: intstr.FromInt32(DefaultPortMqtt),

				Protocol:    corev1.ProtocolTCP,
				AppProtocol: ptr.To("mqtt"),
			},
		)
	}
	if m.EnableTls {
		ports = append(ports,
			corev1.ServicePort{
				Name:       PortNameMqtts,
				Port:       DefaultPortMqtts,
				TargetPort: intstr.FromInt32(DefaultPortMqtts),

				Protocol:    corev1.ProtocolTCP,
				AppProtocol: ptr.To("mqtts"),
			},
		)
	}

	return ports
}

func (m *PortManager) clientWebMqttPorts() []corev1.ServicePort {
	var ports []corev1.ServicePort

	if !m.pluginEnabled(PluginNameWebMqtt) {
		return ports
	}

	if !m.DisableNonTls {
		ports = append(ports,
			corev1.ServicePort{
				Name:       PortNameWebMqtt,
				Port:       DefaultPortWebMqtt,
				TargetPort: intstr.FromInt32(DefaultPortWebMqtt),

				Protocol:    corev1.ProtocolTCP,
				AppProtocol: ptr.To("http"),
			},
		)
	}
	if m.EnableMutualTls {
		ports = append(ports,
			corev1.ServicePort{
				Name:       PortNameWebMqttTls,
				Port:       DefaultPortWebMqttTls,
				TargetPort: intstr.FromInt32(DefaultPortWebMqttTls),

				Protocol:    corev1.ProtocolTCP,
				AppProtocol: ptr.To("https"),
			},
		)
	}

	return ports
}

func (m *PortManager) clientStompPorts() []corev1.ServicePort {
	var ports []corev1.ServicePort

	if !m.pluginEnabled(PluginNameStomp) {
		return ports
	}
	if !m.DisableNonTls {
		ports = append(ports,
			corev1.ServicePort{
				Name:       PortNameStomp,
				Port:       DefaultPortStomp,
				TargetPort: intstr.FromInt32(DefaultPortStomp),

				Protocol:    corev1.ProtocolTCP,
				AppProtocol: ptr.To("stomp.github.io/stomp"),
			},
		)
	}
	if m.EnableTls {
		ports = append(ports,
			corev1.ServicePort{
				Name:       PortNameStomps,
				Port:       DefaultPortStomps,
				TargetPort: intstr.FromInt32(DefaultPortStomps),

				Protocol:    corev1.ProtocolTCP,
				AppProtocol: ptr.To("stomp.github.io/stomp-tls"),
			},
		)
	}

	return ports
}

func (m *PortManager) clientWebStompPorts() []corev1.ServicePort {
	var ports []corev1.ServicePort

	if !m.pluginEnabled(PluginNameWebStomp) {
		return ports
	}
	if !m.DisableNonTls {
		ports = append(ports,
			corev1.ServicePort{
				Name:       PortNameWebStomp,
				Port:       DefaultPortWebStomp,
				TargetPort: intstr.FromInt32(DefaultPortWebStomp),

				Protocol:    corev1.ProtocolTCP,
				AppProtocol: ptr.To("http"),
			},
		)
	}
	if m.EnableMutualTls {
		ports = append(ports,
			corev1.ServicePort{
				Name:       PortNameWebStompTls,
				Port:       DefaultPortWebStompTls,
				TargetPort: intstr.FromInt32(DefaultPortWebStompTls),

				Protocol:    corev1.ProtocolTCP,
				AppProtocol: ptr.To("https"),
			},
		)
	}

	return ports
}

func (m *PortManager) clientStreamPorts() []corev1.ServicePort {
	var ports []corev1.ServicePort

	streamNeeded := m.pluginEnabled(PluginNameStream) ||
		m.pluginEnabled(PluginNameStreamManagement) ||
		m.pluginEnabled(PluginNameStreamMultiDCReplication)
	if !streamNeeded {
		return ports
	}
	if !m.DisableNonTls {
		ports = append(ports,
			corev1.ServicePort{
				Name:       PortNameStream,
				Port:       DefaultPortStream,
				TargetPort: intstr.FromInt32(DefaultPortStream),

				Protocol:    corev1.ProtocolTCP,
				AppProtocol: ptr.To("rabbitmq.com/stream"),
			},
		)
	}
	if m.EnableTls {
		ports = append(ports,
			corev1.ServicePort{
				Name:       PortNameStreams,
				Port:       DefaultPortStreams,
				TargetPort: intstr.FromInt32(DefaultPortStreams),

				Protocol:    corev1.ProtocolTCP,
				AppProtocol: ptr.To("rabbitmq.com/stream-tls"),
			},
		)
	}

	return ports
}

func (m *PortManager) clientPrometheusPorts() []corev1.ServicePort {
	var ports []corev1.ServicePort

	// We expose either 15692 or 15691 in the Service, but not both.
	// If we exposed both ports, a ServiceMonitor selecting all RabbitMQ pods and
	// 15692 as well as 15691 ports would end up in scraping the same RabbitMQ node twice
	// doubling the number of nodes showing up in Grafana because the
	// 'instance' label consists of "<host>:<port>".
	if !m.EnableTls {
		ports = append(ports,
			corev1.ServicePort{
				Name:       PortNamePrometheus,
				Port:       DefaultPortPrometheus,
				TargetPort: intstr.FromInt32(DefaultPortPrometheus),

				Protocol:    corev1.ProtocolTCP,
				AppProtocol: ptr.To("prometheus.io/metrics"),
			},
		)
	} else {
		ports = append(ports,
			corev1.ServicePort{
				Name:       PortNamePrometheusTls,
				Port:       DefaultPortPrometheusTls,
				TargetPort: intstr.FromInt32(DefaultPortPrometheusTls),

				Protocol:    corev1.ProtocolTCP,
				AppProtocol: ptr.To("prometheus.io/metric-tls"),
			},
		)
	}

	return ports
}

func MergePorts(ports ...[]corev1.ServicePort) []corev1.ServicePort {

	var result []corev1.ServicePort
	for _, port := range ports {
		result = append(result, port...)
	}

	return result
}
