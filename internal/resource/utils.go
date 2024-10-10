package resource

import (
	"encoding/json"
	"fmt"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	rabbitmqv1beta1 "github.com/rabbitmq/cluster-operator/v2/api/v1beta1"
	"github.com/rabbitmq/cluster-operator/v2/internal/constant"
)

// sortEnvVar ensures that 'MY_POD_NAME', 'MY_POD_NAMESPACE' and 'K8S_SERVICE_NAME' envVars are defined first in the list
// this is to enable other envVars to reference them as variables successfully
func sortEnvVar(envVar []corev1.EnvVar) {
	for i, e := range envVar {
		if e.Name == "MY_POD_NAME" {
			envVar[0], envVar[i] = envVar[i], envVar[0]
			continue
		}
		if e.Name == "MY_POD_NAMESPACE" {
			envVar[1], envVar[i] = envVar[i], envVar[1]
			continue
		}
		if e.Name == "K8S_SERVICE_NAME" {
			envVar[2], envVar[i] = envVar[i], envVar[2]
		}
	}
}

// sortVolumeMounts always returns '/var/lib/rabbitmq/' and '/var/lib/rabbitmq/mnesia/' first in the list.
// this is to ensure '/var/lib/rabbitmq/' always mounts before '/var/lib/rabbitmq/mnesia/' to avoid shadowing
// popular open-sourced container runtimes like docker and containerD will sort mounts in alphabetical order to
// avoid this issue, but there's no guarantee that all container runtime would do so
func sortVolumeMounts(mounts []corev1.VolumeMount) {
	for i, m := range mounts {
		if m.Name == "rabbitmq-erlang-cookie" {
			mounts[0], mounts[i] = mounts[i], mounts[0]
			continue
		}
		if m.Name == constant.PVCName {
			mounts[1], mounts[i] = mounts[i], mounts[1]
		}
	}
}

// required for OpenShift compatibility, see https://github.com/rabbitmq/cluster-operator/issues/234
func disableBlockOwnerDeletion(pvc corev1.PersistentVolumeClaim) {
	refs := pvc.OwnerReferences
	for i := range refs {
		refs[i].BlockOwnerDeletion = ptr.To(false)
	}
}

func mergeMap(base, override map[string]string) map[string]string {
	result := base
	if result == nil {
		result = make(map[string]string)
	}

	for k, v := range override {
		result[k] = v
	}

	return result
}

func mergeVolumes(currVolumes, nextVolumes []corev1.Volume) []corev1.Volume {
	currMap := make(map[string]int)
	for index := range currVolumes {
		currMap[currVolumes[index].Name] = index
	}

	for index := range nextVolumes {
		next := nextVolumes[index]
		currIndex, found := currMap[next.Name]
		if found {
			currVolumes[currIndex].VolumeSource = next.VolumeSource
		} else {
			currMap[next.Name] = len(currVolumes)
			currVolumes = append(currVolumes, nextVolumes[index])
		}
	}

	return currVolumes
}

func mergePorts(currPorts, nextPorts []corev1.ServicePort) []corev1.ServicePort {
	currMap := make(map[string]int)
	for index := range currPorts {
		currMap[currPorts[index].Name] = index
	}

	for index := range nextPorts {
		next := nextPorts[index]
		currIndex, found := currMap[next.Name]
		if found {
			currPorts[currIndex].Port = next.Port
			currPorts[currIndex].NodePort = next.NodePort
			currPorts[currIndex].TargetPort = next.TargetPort
		} else {
			currMap[next.Name] = len(currPorts)
			currPorts = append(currPorts, nextPorts[index])
		}
	}

	return currPorts
}

func mergeContainers(currContainers, nextContainers []corev1.Container) []corev1.Container {
	currMap := make(map[string]int)
	for index := range currContainers {
		currMap[currContainers[index].Name] = index
	}

	for index := range nextContainers {
		next := nextContainers[index]
		currIndex, found := currMap[next.Name]
		if found {
			currContainers[currIndex] = next
		} else {
			currMap[next.Name] = len(currContainers)
			currContainers = append(currContainers, nextContainers[index])
		}
	}

	return currContainers
}

func mergeVolumeMounts(currVolumes, nextVolumes []corev1.VolumeMount) []corev1.VolumeMount {
	currMap := make(map[string]int)
	for index := range currVolumes {
		currMap[currVolumes[index].Name] = index
	}

	for index := range nextVolumes {
		next := nextVolumes[index]
		currIndex, found := currMap[next.Name]
		if found {
			currVolumes[currIndex] = next
		} else {
			currMap[next.Name] = len(currVolumes)
			currVolumes = append(currVolumes, nextVolumes[index])
		}
	}

	return currVolumes
}

func mergePVCs(currPVCs, nextPVCs []corev1.PersistentVolumeClaim) []corev1.PersistentVolumeClaim {
	currMap := make(map[string]int)
	for index := range currPVCs {
		currMap[currPVCs[index].Name] = index
	}

	for index := range nextPVCs {
		next := nextPVCs[index]
		currIndex, found := currMap[next.Name]
		if found {
			currPVCs[currIndex].Spec.Resources.Requests[corev1.ResourceStorage] = next.Spec.Resources.Requests[corev1.ResourceStorage]
		} else {
			currMap[next.Name] = len(currPVCs)
			currPVCs = append(currPVCs, nextPVCs[index])
		}
	}

	return currPVCs
}

func mergeTSCs(currTSCs, nextTSCs []corev1.TopologySpreadConstraint) []corev1.TopologySpreadConstraint {
	currMap := make(map[string]int)
	for index := range currTSCs {
		currMap[currTSCs[index].TopologyKey] = index
	}

	for index := range nextTSCs {
		next := nextTSCs[index]
		currIndex, found := currMap[next.TopologyKey]
		if found {
			currTSCs[currIndex] = next
		} else {
			currMap[next.TopologyKey] = len(currTSCs)
			currTSCs = append(currTSCs, nextTSCs[index])
		}
	}

	return currTSCs
}

func containerRabbitmq(containers []corev1.Container) corev1.Container {
	for _, container := range containers {
		if container.Name == "rabbitmq" {
			return container
		}
	}
	return corev1.Container{}
}

// copyObjectMeta copies name, labels, and annotations from a given EmbeddedObjectMeta to a metav1.ObjectMeta
// there is no need to copy the namespace because both PVCs and Pod have to be in the same namespace as its StatefulSet
func copyObjectMeta(base *metav1.ObjectMeta, override rabbitmqv1beta1.EmbeddedObjectMeta) {
	if override.Name != "" {
		base.Name = override.Name
	}

	if override.Labels != nil {
		base.Labels = mergeMap(base.Labels, override.Labels)
	}

	if override.Annotations != nil {
		base.Annotations = mergeMap(base.Annotations, override.Annotations)
	}
}

func copyLabelsAnnotations(base *metav1.ObjectMeta, override rabbitmqv1beta1.EmbeddedLabelsAnnotations) {
	if override.Labels != nil {
		base.Labels = mergeMap(base.Labels, override.Labels)
	}

	if override.Annotations != nil {
		base.Annotations = mergeMap(base.Annotations, override.Annotations)
	}
}

func applySvcOverride(svc *corev1.Service, override *rabbitmqv1beta1.Service) error {
	if override.EmbeddedLabelsAnnotations != nil {
		copyLabelsAnnotations(&svc.ObjectMeta, *override.EmbeddedLabelsAnnotations)
	}

	if override.Spec != nil {
		originalSvcSpec, err := json.Marshal(svc.Spec)
		if err != nil {
			return fmt.Errorf("error marshalling Service Spec: %w", err)
		}

		patch, err := json.Marshal(override.Spec)
		if err != nil {
			return fmt.Errorf("error marshalling Service Spec override: %w", err)
		}

		patchedJSON, err := strategicpatch.StrategicMergePatch(originalSvcSpec, patch, corev1.ServiceSpec{})
		if err != nil {
			return fmt.Errorf("error patching Service Spec: %w", err)
		}

		patchedSvcSpec := corev1.ServiceSpec{}
		err = json.Unmarshal(patchedJSON, &patchedSvcSpec)
		if err != nil {
			return fmt.Errorf("error unmarshalling patched Service Spec: %w", err)
		}
		svc.Spec = patchedSvcSpec
	}

	return nil
}

func applyStsOverride(instance *rabbitmqv1beta1.RabbitmqCluster, scheme *runtime.Scheme, sts *appsv1.StatefulSet, stsOverride *rabbitmqv1beta1.StatefulSet) error {
	if stsOverride.EmbeddedLabelsAnnotations != nil {
		copyLabelsAnnotations(&sts.ObjectMeta, *stsOverride.EmbeddedLabelsAnnotations)
	}

	if stsOverride.Spec == nil {
		return nil
	}
	if stsOverride.Spec.Replicas != nil {
		sts.Spec.Replicas = stsOverride.Spec.Replicas
	}
	if stsOverride.Spec.UpdateStrategy != nil {
		sts.Spec.UpdateStrategy = *stsOverride.Spec.UpdateStrategy
	}
	if stsOverride.Spec.PodManagementPolicy != "" {
		sts.Spec.PodManagementPolicy = stsOverride.Spec.PodManagementPolicy
	}

	if stsOverride.Spec.MinReadySeconds != 0 {
		sts.Spec.MinReadySeconds = stsOverride.Spec.MinReadySeconds
	}

	if len(stsOverride.Spec.VolumeClaimTemplates) != 0 {
		// If spec.persistence.storage == 0, ignore PVC overrides.
		// Main reason for that is that there is no default PVC in such case (emptyDir is used instead)
		// other PVCs could technically be still overridden/added but we would be entering a very confusing territory
		// where storage is set to 0 and yet there are PVCs with data
		if instance.Spec.Persistence.Storage.Cmp(k8sresource.MustParse("0Gi")) == 0 {
			logger := ctrl.Log.WithName("statefulset").WithName("RabbitmqCluster")
			logger.Info(fmt.Sprintf("Warning: persistentVolumeClaim overrides are ignored for cluster \"%s\", because spec.persistence.storage is set to zero.", sts.GetName()))
		} else {
			volumeClaimTemplatesOverride := stsOverride.Spec.VolumeClaimTemplates
			pvcOverride := make([]corev1.PersistentVolumeClaim, len(volumeClaimTemplatesOverride))
			for i := range volumeClaimTemplatesOverride {
				copyObjectMeta(&pvcOverride[i].ObjectMeta, volumeClaimTemplatesOverride[i].EmbeddedObjectMeta)
				pvcOverride[i].Namespace = sts.Namespace // PVC should always be in the same namespace as the Stateful Set
				pvcOverride[i].Spec = volumeClaimTemplatesOverride[i].Spec
				if err := controllerutil.SetControllerReference(instance, &pvcOverride[i], scheme); err != nil {
					return fmt.Errorf("failed setting controller reference: %w", err)
				}
				disableBlockOwnerDeletion(pvcOverride[i])
			}
			sts.Spec.VolumeClaimTemplates = pvcOverride
		}
	}

	if stsOverride.Spec.PersistentVolumeClaimRetentionPolicy != nil {
		sts.Spec.PersistentVolumeClaimRetentionPolicy = stsOverride.Spec.PersistentVolumeClaimRetentionPolicy
	}

	if stsOverride.Spec.Template == nil {
		return nil
	}
	if stsOverride.Spec.Template.EmbeddedObjectMeta != nil {
		copyObjectMeta(&sts.Spec.Template.ObjectMeta, *stsOverride.Spec.Template.EmbeddedObjectMeta)
	}
	if stsOverride.Spec.Template.Spec != nil {
		patchedPodSpec, err := applyPodSpecOverride(&sts.Spec.Template.Spec, stsOverride.Spec.Template.Spec)
		if err != nil {
			return err
		}
		sts.Spec.Template.Spec = patchedPodSpec
	}

	return nil
}

func applyPodSpecOverride(podSpec, podSpecOverride *corev1.PodSpec) (corev1.PodSpec, error) {
	originalPodSpec, err := json.Marshal(podSpec)
	if err != nil {
		return corev1.PodSpec{}, fmt.Errorf("error marshalling statefulSet podSpec: %w", err)
	}

	patch, err := json.Marshal(podSpecOverride)
	if err != nil {
		return corev1.PodSpec{}, fmt.Errorf("error marshalling statefulSet podSpec override: %w", err)
	}

	patchedJSON, err := strategicpatch.StrategicMergePatch(originalPodSpec, patch, corev1.PodSpec{})
	if err != nil {
		return corev1.PodSpec{}, fmt.Errorf("error patching podSpec: %w", err)
	}

	patchedPodSpec := corev1.PodSpec{}
	err = json.Unmarshal(patchedJSON, &patchedPodSpec)
	if err != nil {
		return corev1.PodSpec{}, fmt.Errorf("error unmarshalling patched Stateful Set: %w", err)
	}

	rmqContainer := containerRabbitmq(podSpecOverride.Containers)
	// handle the rabbitmq container envVar list as a special case if it's overwritten
	// we need to ensure that MY_POD_NAME, MY_POD_NAMESPACE and K8S_SERVICE_NAME are defined first so other envVars values can reference them
	if rmqContainer.Env != nil {
		sortEnvVar(patchedPodSpec.Containers[0].Env)
	}
	// handle the rabbitmq container volumeMounts list as a special case if it's overwritten
	// we need to ensure that '/var/lib/rabbitmq/' always mounts before '/var/lib/rabbitmq/mnesia/' to avoid shadowing
	if rmqContainer.VolumeMounts != nil {
		sortVolumeMounts(patchedPodSpec.Containers[0].VolumeMounts)
	}

	// A user may wish to override the controller-set securityContext for the RabbitMQ & init containers so that the
	// container runtime can override them. If the securityContext has been set to an empty struct, `strategicpatch.StrategicMergePatch`
	// won't pick this up, so manually override it here.
	if podSpecOverride.SecurityContext != nil && reflect.DeepEqual(*podSpecOverride.SecurityContext, corev1.PodSecurityContext{}) {
		patchedPodSpec.SecurityContext = nil
	}
	for i := range podSpecOverride.InitContainers {
		if podSpecOverride.InitContainers[i].SecurityContext != nil && reflect.DeepEqual(*podSpecOverride.InitContainers[i].SecurityContext, corev1.SecurityContext{}) {
			patchedPodSpec.InitContainers[i].SecurityContext = nil
		}
	}

	return patchedPodSpec, nil
}
