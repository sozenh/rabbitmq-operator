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

	"github.com/cloudflare/cfssl/log"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/apimachinery/pkg/util/intstr"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	rabbitmqv1beta1 "github.com/rabbitmq/cluster-operator/v2/api/v1beta1"
	"github.com/rabbitmq/cluster-operator/v2/internal/constant"
	"github.com/rabbitmq/cluster-operator/v2/internal/metadata"
)

const (
	initContainerCPU    string = "100m"
	initContainerMemory string = "500Mi"
	DeletionMarker      string = "skipPreStopChecks"
)

const (
	dirTmp          = "/tmp/"
	dirPodInfo      = "/etc/pod-info/"
	dirRabbitmqLib  = "/var/lib/rabbitmq/"
	dirRabbitmqData = dirRabbitmqLib + "mnesia/"

	dirRabbitmqTls  = "/etc/rabbitmq-tls/"
	fileNameCaCert  = "ca.crt"
	filePathCaCert  = dirRabbitmqTls + fileNameCaCert
	fileNameTlsCert = "tls.crt"
	filePathTlsCert = dirRabbitmqTls + fileNameTlsCert
	fileNameTlsKey  = "tls.key"
	filePathTlsKey  = dirRabbitmqTls + fileNameTlsKey

	dirErlangCookieTmp      = dirTmp + "erlang-cookie-secret/"
	fileNameErlangCookie    = ".erlang.cookie"
	filePathErlangCookie    = dirRabbitmqLib + fileNameErlangCookie
	filePathErlangCookieTmp = dirErlangCookieTmp + fileNameErlangCookie

	dirRabbitmqPlugins         = "/operator/"
	dirRabbitmqPluginsTmp      = dirTmp + "rabbitmq-plugins/"
	fileNameRabbitmqPlugins    = "enabled_plugins"
	filePathRabbitmqPlugins    = dirRabbitmqPlugins + fileNameRabbitmqPlugins
	filePathRabbitmqPluginsTmp = dirRabbitmqPluginsTmp + fileNameRabbitmqPlugins

	fileNameRabbitmqAdminConf = ".rabbitmqadmin.conf"
	filePathRabbitmqAdminConf = dirRabbitmqLib + fileNameRabbitmqAdminConf

	dirRabbitmqConf                = "/etc/rabbitmq/"
	fileNameRabbitmqConfEnv        = "rabbitmq-env.conf"
	filePathRabbitmqConfEnv        = dirRabbitmqConf + fileNameRabbitmqConfEnv
	fileNameRabbitmqConfAdvanced   = "advanced.config"
	filePathRabbitmqConfAdvanced   = dirRabbitmqConf + fileNameRabbitmqConfAdvanced
	fileNameRabbitmqConfErlangInet = "erl_inetrc"
	filePathRabbitmqConfErlangInet = dirRabbitmqConf + fileNameRabbitmqConfErlangInet

	dirRabbitmqConfd                    = "/etc/rabbitmq/conf.d/"
	fileNameRabbitmqConfdDefault        = "operatorDefaults.conf"
	filePathRabbitmqConfdDefault        = dirRabbitmqConfd + "10-" + fileNameRabbitmqConfdDefault
	fileNameRabbitmqConfdDefaultUser    = "default_user.conf"
	filePathRabbitmqConfdDefaultUserTmp = dirTmp + fileNameRabbitmqConfdDefaultUser
	filePathRabbitmqConfdDefaultUser    = dirRabbitmqConfd + "11-" + fileNameRabbitmqConfdDefaultUser
	fileNameRabbitmqConfdUserDefined    = "userDefinedConfiguration.conf"
	filePathRabbitmqConfdUserDefined    = dirRabbitmqConfd + "90-" + fileNameRabbitmqConfdUserDefined
)

type StatefulSetBuilder struct {
	*RabbitmqResourceBuilder
}

func (builder *RabbitmqResourceBuilder) StatefulSet() *StatefulSetBuilder {
	return &StatefulSetBuilder{builder}
}

func (builder *StatefulSetBuilder) Build() (client.Object, error) {
	// PVC, ServiceName & Selector: can't be updated without deleting the statefulset

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        builder.Instance.ChildResourceName(constant.ResourceStatefulsetSuffix),
			Namespace:   builder.Instance.Namespace,
			Labels:      metadata.GetLabels(builder.Instance.Name, builder.Instance.Labels),
			Annotations: metadata.ReconcileAndFilterAnnotations(nil, builder.Instance.Annotations),
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: metadata.LabelSelector(builder.Instance.Name),
			},
			PodManagementPolicy:  appsv1.ParallelPodManagement,
			VolumeClaimTemplates: builder.persistentVolumeClaims([]corev1.PersistentVolumeClaim{}),
			ServiceName:          builder.Instance.ChildResourceName(constant.ResourceHeadlessServiceSuffix),
		},
	}

	// StatefulSet Override
	// override is applied to PVC, ServiceName & Selector
	// other fields are handled in Update()
	overrideSts := builder.Instance.Spec.Override.StatefulSet
	if overrideSts != nil && overrideSts.Spec != nil {
		if overrideSts.Spec.Selector != nil {
			sts.Spec.Selector = overrideSts.Spec.Selector
		}

		if overrideSts.Spec.ServiceName != "" {
			sts.Spec.ServiceName = overrideSts.Spec.ServiceName
		}

	}

	return sts, nil
}

func (builder *StatefulSetBuilder) UpdateMayRequireStsRecreate() bool {
	return true
}

func (builder *StatefulSetBuilder) Update(object client.Object) error {
	sts := object.(*appsv1.StatefulSet)
	//Labels
	sts.Labels = metadata.GetLabels(builder.Instance.Name, builder.Instance.Labels)
	//Annotations
	sts.Annotations = metadata.ReconcileAndFilterAnnotations(sts.Annotations, builder.Instance.Annotations)

	//Replicas
	sts.Spec.Replicas = builder.Instance.Spec.Replicas

	// pod template
	sts.Spec.Template = builder.podTemplateSpec(sts.Spec.Template)

	// PVC storage capacity
	sts.Spec.VolumeClaimTemplates = builder.persistentVolumeClaims(sts.Spec.VolumeClaimTemplates)

	//Update Strategy
	sts.Spec.UpdateStrategy = appsv1.StatefulSetUpdateStrategy{
		Type:          appsv1.RollingUpdateStatefulSetStrategyType,
		RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{Partition: ptr.To(int32(0))},
	}

	if builder.Instance.Spec.Override.StatefulSet != nil {
		if err := applyStsOverride(builder.Instance, builder.Scheme, sts, builder.Instance.Spec.Override.StatefulSet); err != nil {
			return fmt.Errorf("failed applying StatefulSet override: %w", err)
		}
	}

	if !sts.Spec.Template.Spec.Containers[0].Resources.Limits.Memory().Equal(*sts.Spec.Template.Spec.Containers[0].Resources.Requests.Memory()) {
		logger := ctrl.Log.WithName("statefulset").WithName("RabbitmqCluster")
		logger.Info(fmt.Sprintf("Warning: Memory request and limit are not equal for \"%s\". It is recommended that they be set to the same value", sts.GetName()))
	}

	if err := controllerutil.SetControllerReference(builder.Instance, sts, builder.Scheme); err != nil {
		return fmt.Errorf("failed setting controller reference: %w", err)
	}
	return nil
}

func (builder *StatefulSetBuilder) podTemplateSpec(pod corev1.PodTemplateSpec) corev1.PodTemplateSpec {
	rabbitmqUID := int64(999)
	// default pod annotations
	defaultPodAnnotations := make(map[string]string)

	if builder.Instance.VaultEnabled() {
		defaultPodAnnotations = appendVaultAnnotations(defaultPodAnnotations, builder.Instance)
	}

	pod.Labels = metadata.Label(builder.Instance.Name)
	pod.Annotations = metadata.ReconcileAnnotations(pod.Annotations, defaultPodAnnotations)

	pod.Spec.Volumes = builder.volumes(pod.Spec.Volumes)
	pod.Spec.Containers = builder.containers(pod.Spec.Containers)
	pod.Spec.InitContainers = builder.initContainers(pod.Spec.InitContainers)

	pod.Spec.Affinity = builder.Instance.Spec.Affinity
	pod.Spec.Tolerations = builder.Instance.Spec.Tolerations
	pod.Spec.TopologySpreadConstraints = builder.topologySpreadConstraints(pod.Spec.TopologySpreadConstraints)

	pod.Spec.ImagePullSecrets = builder.Instance.Spec.ImagePullSecrets
	pod.Spec.ServiceAccountName = builder.Instance.ChildResourceName(constant.ResourceServiceAccountSuffix)

	pod.Spec.AutomountServiceAccountToken = ptr.To(true)
	pod.Spec.TerminationGracePeriodSeconds = builder.Instance.Spec.TerminationGracePeriodSeconds
	pod.Spec.SecurityContext = &corev1.PodSecurityContext{FSGroup: ptr.To(int64(0)), RunAsUser: &rabbitmqUID}

	return pod
}

func (builder *StatefulSetBuilder) envs() []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "K8S_SERVICE_NAME",
			Value: builder.Instance.ChildResourceName(constant.ResourceHeadlessServiceSuffix),
		},
		{
			Name: "MY_POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.name"},
			},
		},
		{
			Name: "MY_POD_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.namespace"},
			},
		},
	}
}

func (builder *StatefulSetBuilder) volumeTLS() corev1.Volume {
	volume := corev1.Volume{
		Name: constant.VolumeNameRabbitmqTLS,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				Sources: []corev1.VolumeProjection{
					{
						Secret: &corev1.SecretProjection{
							Optional:             ptr.To(true),
							LocalObjectReference: corev1.LocalObjectReference{Name: builder.Instance.Spec.TLS.SecretName},
						},
					},
				},
				DefaultMode: ptr.To(int32(400)),
			},
		},
	}

	if builder.Instance.MutualTLSEnabled() && !builder.Instance.SingleTLSSecret() {
		caSecretProjection := corev1.VolumeProjection{
			Secret: &corev1.SecretProjection{
				Optional:             ptr.To(true),
				LocalObjectReference: corev1.LocalObjectReference{Name: builder.Instance.Spec.TLS.CaSecretName},
			},
		}
		volume.VolumeSource.Projected.Sources = append(volume.VolumeSource.Projected.Sources, caSecretProjection)
	}

	return volume
}

func (builder *StatefulSetBuilder) volumeConfd() corev1.Volume {
	volume := corev1.Volume{
		Name: constant.VolumeNameRabbitmqConfd,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				Sources: []corev1.VolumeProjection{
					{
						ConfigMap: &corev1.ConfigMapProjection{
							Items: []corev1.KeyToPath{
								{Key: fileNameRabbitmqConfdDefault, Path: fileNameRabbitmqConfdDefault},
								{Key: fileNameRabbitmqConfdUserDefined, Path: fileNameRabbitmqConfdUserDefined},
							},
							LocalObjectReference: corev1.LocalObjectReference{
								Name: builder.Instance.ChildResourceName(constant.ResourceServerConfigMapSuffix),
							},
						},
					},
				},
			},
		},
	}
	if !builder.Instance.VaultDefaultUserSecretEnabled() {
		secretName := ""
		if builder.Instance.ExternalSecretEnabled() {
			secretName = builder.Instance.Spec.SecretBackend.ExternalSecret.Name
		} else {
			secretName = builder.Instance.ChildResourceName(DefaultUserSecretName)
		}

		userSecretProject := corev1.VolumeProjection{
			Secret: &corev1.SecretProjection{
				LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
				Items:                []corev1.KeyToPath{{Key: fileNameRabbitmqConfdDefaultUser, Path: fileNameRabbitmqConfdDefaultUser}},
			},
		}
		volume.VolumeSource.Projected.Sources = append(volume.VolumeSource.Projected.Sources, userSecretProject)
	}

	return volume
}

func (builder *StatefulSetBuilder) volumes(currVolumes []corev1.Volume) []corev1.Volume {
	volumes := []corev1.Volume{
		builder.volumeConfd(),
		{
			Name: constant.VolumeNameRabbitmqPlugins,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: constant.VolumeNamePluginsConf,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: builder.Instance.ChildResourceName(constant.ResourcePluginConfigMapSuffix),
					},
				},
			},
		},
		{
			Name: constant.VolumeNameRabbitmqErlangCookie,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: constant.VolumeNameErlangCookieSecret,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: builder.Instance.ChildResourceName(constant.ResourceErlangCookieSuffix),
				},
			},
		},
		{
			Name: constant.VolumeNamePodInfo,
			VolumeSource: corev1.VolumeSource{
				DownwardAPI: &corev1.DownwardAPIVolumeSource{
					Items: []corev1.DownwardAPIVolumeFile{
						{
							Path: DeletionMarker,
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: fmt.Sprintf("metadata.labels['%s']", DeletionMarker),
							},
						},
					},
				},
			},
		},
	}

	if builder.Instance.SecretTLSEnabled() {
		volumes = append(volumes, builder.volumeTLS())
	}

	zero := k8sresource.MustParse("0Gi")
	if builder.Instance.Spec.Persistence.Storage.Cmp(zero) == 0 {
		volumes = append(volumes, corev1.Volume{
			Name:         constant.VolumeNamePersistence,
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		})
	}

	if builder.rabbitmqConfigurationIsSet() {
		volumes = append(volumes, corev1.Volume{
			Name: constant.VolumeNameRabbitmqServerConf,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: builder.Instance.ChildResourceName(constant.ResourceServerConfigMapSuffix),
					},
				},
			},
		})
	}

	return mergeVolumes(currVolumes, volumes)
}

func (builder *StatefulSetBuilder) containerSetup() corev1.Container {
	setup := corev1.Container{
		Name:  constant.ContainerNameSetup,
		Image: builder.Instance.Spec.Image,
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    k8sresource.MustParse(initContainerCPU),
				corev1.ResourceMemory: k8sresource.MustParse(initContainerMemory),
			},
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    k8sresource.MustParse(initContainerCPU),
				corev1.ResourceMemory: k8sresource.MustParse(initContainerMemory),
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: constant.VolumeNamePersistence, MountPath: dirRabbitmqData},
			{Name: constant.VolumeNamePluginsConf, MountPath: dirRabbitmqPluginsTmp},
			{Name: constant.VolumeNameRabbitmqPlugins, MountPath: dirRabbitmqPlugins},
			{Name: constant.VolumeNameErlangCookieSecret, MountPath: dirErlangCookieTmp},
			{Name: constant.VolumeNameRabbitmqErlangCookie, MountPath: dirRabbitmqLib},
		},
	}

	command := "" +
		fmt.Sprintf("cp %s %s ", filePathErlangCookieTmp, filePathErlangCookie) +
		fmt.Sprintf("&& chmod 600 %s ; ", filePathErlangCookie) +
		fmt.Sprintf("cp %s %s ; ", filePathRabbitmqPluginsTmp, filePathRabbitmqPlugins) +
		fmt.Sprintf("echo '[default]' > %s ", filePathRabbitmqAdminConf) +
		fmt.Sprintf("&& sed -e 's/default_user/username/' -e 's/default_pass/password/' %%s >> %s ", filePathRabbitmqAdminConf) +
		fmt.Sprintf("&& chmod 600 %s ; ", filePathRabbitmqAdminConf) +
		"sleep " + strconv.Itoa(int(ptr.Deref(builder.Instance.Spec.DelayStartSeconds, 30)))

	if builder.Instance.VaultDefaultUserSecretEnabled() {
		// Vault annotation automatically mounts the volume
		command = fmt.Sprintf(command, filePathRabbitmqConfdDefaultUser)
	} else {
		command = fmt.Sprintf(command, filePathRabbitmqConfdDefaultUserTmp)
		setup.VolumeMounts = append(setup.VolumeMounts,
			corev1.VolumeMount{Name: constant.VolumeNameRabbitmqConfd, MountPath: filePathRabbitmqConfdDefaultUserTmp, SubPath: fileNameRabbitmqConfdDefaultUser})
	}

	setup.Command = []string{"sh", "-c", command}

	return setup
}

func (builder *StatefulSetBuilder) containerRabbitmq() corev1.Container {
	readinessProbePort := constant.PortNameAmqp
	if builder.Instance.DisableNonTLSListeners() {
		readinessProbePort = constant.PortNameAmqps
	}

	container := corev1.Container{
		Name:      constant.ContainerNameRabbitMQ,
		Image:     builder.Instance.Spec.Image,
		Resources: *builder.Instance.Spec.Resources,

		// Why using a tcp readiness probe instead of running `rabbitmq-diagnostics check_port_connectivity`?
		// Using rabbitmq-diagnostics command as the probe could cause context deadline exceeded errors
		// Pods could be stuck at terminating at deletion as a result of that
		// More details see issue: https://github.com/rabbitmq/cluster-operator/issues/409
		ReadinessProbe: &corev1.Probe{
			TimeoutSeconds:      5,
			PeriodSeconds:       10,
			SuccessThreshold:    1,
			FailureThreshold:    3,
			InitialDelaySeconds: 10,
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromString(readinessProbePort),
				},
			},
		},
		Lifecycle: &corev1.Lifecycle{
			PreStop: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"/bin/bash", "-c",
						fmt.Sprintf(
							"if [ ! -z \"$(cat %s%s)\" ]; then exit 0; fi;",
							dirPodInfo, DeletionMarker,
						) +
							fmt.Sprintf(""+
								" rabbitmq-upgrade await_online_quorum_plus_one -t %d &&"+
								" rabbitmq-upgrade await_online_synchronized_mirror -t %d &&"+
								" rabbitmq-upgrade drain -t %d",
								*builder.Instance.Spec.TerminationGracePeriodSeconds,
								*builder.Instance.Spec.TerminationGracePeriodSeconds,
								*builder.Instance.Spec.TerminationGracePeriodSeconds),
					},
				},
			},
		},
	}

	container.Ports = []corev1.ContainerPort{}
	for _, port := range constant.RabbitMQPorts(builder.Instance) {
		container.Ports = append(container.Ports,
			corev1.ContainerPort{Name: port.Name, ContainerPort: port.Port, Protocol: port.Protocol},
		)
	}

	container.Env = append(builder.envs(),
		corev1.EnvVar{Name: "RABBITMQ_ENABLED_PLUGINS_FILE", Value: filePathRabbitmqPlugins},
		corev1.EnvVar{Name: "RABBITMQ_USE_LONGNAME", Value: "true"},
		corev1.EnvVar{Name: "RABBITMQ_NODENAME", Value: "rabbit@$(MY_POD_NAME).$(K8S_SERVICE_NAME).$(MY_POD_NAMESPACE)"},
		corev1.EnvVar{Name: "K8S_HOSTNAME_SUFFIX", Value: ".$(K8S_SERVICE_NAME).$(MY_POD_NAMESPACE)"},
	)

	container.VolumeMounts = []corev1.VolumeMount{
		{Name: constant.VolumeNamePodInfo, MountPath: dirPodInfo},
		{Name: constant.VolumeNameRabbitmqPlugins, MountPath: dirRabbitmqPlugins},
		{Name: constant.VolumeNameRabbitmqErlangCookie, MountPath: dirRabbitmqLib},
		{Name: constant.VolumeNamePersistence, MountPath: dirRabbitmqData},
		{Name: constant.VolumeNameRabbitmqConfd, MountPath: filePathRabbitmqConfdDefault, SubPath: fileNameRabbitmqConfdDefault},
		{Name: constant.VolumeNameRabbitmqConfd, MountPath: filePathRabbitmqConfdUserDefined, SubPath: fileNameRabbitmqConfdUserDefined},
	}

	if builder.Instance.SecretTLSEnabled() {
		container.VolumeMounts = append(container.VolumeMounts,
			corev1.VolumeMount{Name: constant.VolumeNameRabbitmqTLS, MountPath: dirRabbitmqTls, ReadOnly: true},
		)
	}
	if !builder.Instance.VaultDefaultUserSecretEnabled() {
		container.VolumeMounts = append(container.VolumeMounts,
			corev1.VolumeMount{Name: constant.VolumeNameRabbitmqConfd, MountPath: filePathRabbitmqConfdDefaultUser, SubPath: fileNameRabbitmqConfdDefaultUser},
		)
	}

	if builder.Instance.Spec.Rabbitmq.EnvConfig != "" {
		container.VolumeMounts = append(container.VolumeMounts,
			corev1.VolumeMount{Name: constant.VolumeNameRabbitmqServerConf, MountPath: filePathRabbitmqConfEnv, SubPath: fileNameRabbitmqConfEnv},
		)
	}
	if builder.Instance.Spec.Rabbitmq.AdvancedConfig != "" {
		container.VolumeMounts = append(container.VolumeMounts,
			corev1.VolumeMount{Name: constant.VolumeNameRabbitmqServerConf, MountPath: filePathRabbitmqConfAdvanced, SubPath: fileNameRabbitmqConfAdvanced},
		)
	}
	if builder.Instance.Spec.Rabbitmq.ErlangInetConfig != "" {
		container.VolumeMounts = append(container.VolumeMounts,
			corev1.VolumeMount{Name: constant.VolumeNameRabbitmqServerConf, MountPath: filePathRabbitmqConfErlangInet, SubPath: fileNameRabbitmqConfErlangInet},
		)
	}

	return container
}

func (builder *StatefulSetBuilder) containerUserCredentialUpdater() corev1.Container {
	managementURI := "http://127.0.0.1:15672"
	if builder.Instance.TLSEnabled() {
		// RabbitMQ certificate SAN must include this host name.
		// (Alternatively, we could have put 127.0.0.1 in the management URI and put 127.0.0.1 into certificate IP SAN.
		// However, that approach seems to be less common.)
		managementURI = "https://$(HOSTNAME_DOMAIN):15671"
	}
	container := corev1.Container{
		Name:  constant.ContainerNameUserCredentialUpdater,
		Args:  []string{"--management-uri", managementURI, "-v", "4"},
		Image: *builder.Instance.Spec.SecretBackend.Vault.DefaultUserUpdaterImage,

		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    k8sresource.MustParse("500m"),
				corev1.ResourceMemory: k8sresource.MustParse("128Mi"),
			},
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    k8sresource.MustParse("10m"),
				corev1.ResourceMemory: k8sresource.MustParse("512Ki"),
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: constant.VolumeNameRabbitmqErlangCookie, MountPath: dirRabbitmqLib},
			// VolumeMount /etc/rabbitmq/conf.d/11-default-user will be added by Vault injector.
		},
		Env: append(builder.envs(),
			corev1.EnvVar{Name: "HOSTNAME_DOMAIN", Value: "$(MY_POD_NAME).$(K8S_SERVICE_NAME).$(MY_POD_NAMESPACE)"}),
	}

	if builder.Instance.SecretTLSEnabled() {
		container.VolumeMounts = append(
			container.VolumeMounts,
			corev1.VolumeMount{Name: constant.VolumeNameRabbitmqTLS, MountPath: dirRabbitmqTls, ReadOnly: true})
	}
	// If instance.VaultTLSEnabled() volume mount /etc/rabbitmq-tls/ will be added by Vault injector.

	return container
}

func (builder *StatefulSetBuilder) containers(currContainers []corev1.Container) []corev1.Container {
	containers := []corev1.Container{builder.containerRabbitmq()}
	if builder.Instance.VaultDefaultUserSecretEnabled() &&
		builder.Instance.Spec.SecretBackend.Vault.DefaultUserUpdaterImage != nil &&
		*builder.Instance.Spec.SecretBackend.Vault.DefaultUserUpdaterImage != "" {
		containers = append(containers, builder.containerUserCredentialUpdater())
	}

	return mergeContainers(currContainers, containers)
}

func (builder *StatefulSetBuilder) initContainers(currContainers []corev1.Container) []corev1.Container {
	return mergeContainers(currContainers, []corev1.Container{builder.containerSetup()})
}

func (builder *StatefulSetBuilder) persistentVolumeClaims(currPVCs []corev1.PersistentVolumeClaim) []corev1.PersistentVolumeClaim {
	zero := k8sresource.MustParse("0Gi")
	if builder.Instance.Spec.Persistence.Storage.Cmp(zero) == 0 {
		return currPVCs
	}

	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        constant.PVCName,
			Namespace:   builder.Instance.GetNamespace(),
			Labels:      metadata.Label(builder.Instance.Name),
			Annotations: metadata.ReconcileAndFilterAnnotations(map[string]string{}, builder.Instance.Annotations),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: *builder.Instance.Spec.Persistence.Storage,
				},
			},
			StorageClassName: builder.Instance.Spec.Persistence.StorageClassName,
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
	}

	if err := controllerutil.SetControllerReference(builder.Instance, &pvc, builder.Scheme); err != nil {
		log.Errorf("failed setting controller reference: %w", err)
		return currPVCs
	}
	disableBlockOwnerDeletion(pvc)

	return mergePVCs(currPVCs, []corev1.PersistentVolumeClaim{pvc})
}

func (builder *StatefulSetBuilder) topologySpreadConstraints(currTSCs []corev1.TopologySpreadConstraint) []corev1.TopologySpreadConstraint {
	if builder.Instance.DisableDefaultTopologySpreadConstraints() {
		return currTSCs
	}

	return mergeTSCs(currTSCs,
		[]corev1.TopologySpreadConstraint{
			{
				MaxSkew: 1,
				// "topology.kubernetes.io/zone" is a well-known label.
				// It is automatically set by kubelet if the cloud provider provides the zone information.
				// See: https://kubernetes.io/docs/reference/kubernetes-api/labels-annotations-taints/#topologykubernetesiozone
				TopologyKey:       "topology.kubernetes.io/zone",
				WhenUnsatisfiable: corev1.ScheduleAnyway,
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: metadata.LabelSelector(builder.Instance.Name),
				},
			},
		},
	)
}

func (builder *StatefulSetBuilder) rabbitmqConfigurationIsSet() bool {
	return builder.Instance.Spec.Rabbitmq.AdvancedConfig != "" ||
		builder.Instance.Spec.Rabbitmq.EnvConfig != "" ||
		builder.Instance.Spec.Rabbitmq.ErlangInetConfig != ""
}

func podHostNames(instance *rabbitmqv1beta1.RabbitmqCluster) string {
	altNames := ""
	var i int32
	for i = 0; i < ptr.Deref(instance.Spec.Replicas, 1); i++ {
		altNames += fmt.Sprintf(",%s", fmt.Sprintf("%s-%d.%s.%s", instance.ChildResourceName(constant.ResourceStatefulsetSuffix), i, instance.ChildResourceName(constant.ResourceHeadlessServiceSuffix), instance.Namespace))
	}
	return strings.TrimPrefix(altNames, ",")
}

func appendVaultAnnotations(currentAnnotations map[string]string, instance *rabbitmqv1beta1.RabbitmqCluster) map[string]string {
	vault := instance.Spec.SecretBackend.Vault

	vaultAnnotations := map[string]string{
		"vault.hashicorp.com/agent-inject":     "true",
		"vault.hashicorp.com/agent-init-first": "true",
		"vault.hashicorp.com/role":             vault.Role,
	}

	if vault.DefaultUserSecretEnabled() {
		secretName := "11-default_user.conf"
		vaultAnnotations["vault.hashicorp.com/secret-volume-path-"+secretName] = "/etc/rabbitmq/conf.d"
		vaultAnnotations["vault.hashicorp.com/agent-inject-perms-"+secretName] = "0640"
		vaultAnnotations["vault.hashicorp.com/agent-inject-secret-"+secretName] = vault.DefaultUserPath
		vaultAnnotations["vault.hashicorp.com/agent-inject-template-"+secretName] = fmt.Sprintf(`
{{- with secret "%s" -}}
default_user = {{ .Data.data.username }}
default_pass = {{ .Data.data.password }}
{{- end }}`, vault.DefaultUserPath)
	}

	if vault.TLSEnabled() {
		pathCert := vault.TLS.PKIIssuerPath
		commonName := instance.ServiceSubDomain()
		if vault.TLS.CommonName != "" {
			commonName = vault.TLS.CommonName
		}

		altNames := podHostNames(instance)
		if vault.TLS.AltNames != "" {
			altNames = fmt.Sprintf("%s,%s", altNames, vault.TLS.AltNames)
		}

		certDir := strings.TrimSuffix(dirRabbitmqTls, "/")
		vaultAnnotations["vault.hashicorp.com/secret-volume-path-"+fileNameTlsCert] = certDir
		vaultAnnotations["vault.hashicorp.com/agent-inject-secret-"+fileNameTlsCert] = pathCert
		vaultAnnotations["vault.hashicorp.com/agent-inject-template-"+fileNameTlsCert] = generateVaultTLSCertificateTemplate(commonName, altNames, vault)

		vaultAnnotations["vault.hashicorp.com/secret-volume-path-"+fileNameTlsKey] = certDir
		vaultAnnotations["vault.hashicorp.com/agent-inject-secret-"+fileNameTlsKey] = pathCert
		vaultAnnotations["vault.hashicorp.com/agent-inject-template-"+fileNameTlsKey] = generateVaultTLSTemplate(commonName, altNames, vault.TLS.PKIIssuerPath, vault.TLS.IpSans, "private_key")

		vaultAnnotations["vault.hashicorp.com/secret-volume-path-"+fileNameCaCert] = certDir
		vaultAnnotations["vault.hashicorp.com/agent-inject-secret-"+fileNameCaCert] = pathCert
		vaultAnnotations["vault.hashicorp.com/agent-inject-template-"+fileNameCaCert] = generateVaultCATemplate(commonName, altNames, vault)
	}

	return metadata.ReconcileAnnotations(currentAnnotations, vaultAnnotations, vault.Annotations)
}

func generateVaultTLSTemplate(commonName, altNames string, vaultPath string, ipSans string, tlsAttribute string) string {
	return fmt.Sprintf(`
{{- with secret "%s" "common_name=%s" "alt_names=%s" "ip_sans=%s" -}}
{{ .Data.%s }}
{{- end }}`, vaultPath, commonName, altNames, ipSans, tlsAttribute)
}

func generateVaultCATemplate(commonName, altNames string, vault *rabbitmqv1beta1.VaultSpec) string {
	if vault.TLS.PKIRootPath == "" {
		return generateVaultTLSTemplate(commonName, altNames, vault.TLS.PKIIssuerPath, vault.TLS.IpSans, "issuing_ca")
	} else {
		return fmt.Sprintf(`
{{- with secret "%s" -}}
{{ .Data.certificate }}
{{- end }}`, vault.TLS.PKIRootPath)
	}
}

func generateVaultTLSCertificateTemplate(commonName, altNames string, vault *rabbitmqv1beta1.VaultSpec) string {
	return fmt.Sprintf(`
{{- with secret "%s" "common_name=%s" "alt_names=%s" "ip_sans=%s" -}}
{{ .Data.certificate }}
{{- if .Data.ca_chain -}}
{{- $lastintermediatecertindex := len .Data.ca_chain | subtract 1 -}}
{{ range $index, $cacert := .Data.ca_chain }}
{{ if (lt $index $lastintermediatecertindex) }}
{{ $cacert }}
{{ end }}
{{ end }}
{{- end -}}
{{- end -}}`, vault.TLS.PKIIssuerPath, commonName, altNames, vault.TLS.IpSans)
}
