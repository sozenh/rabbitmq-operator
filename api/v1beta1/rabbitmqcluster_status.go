package v1beta1

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RabbitmqClusterStatus presents the observed state of RabbitmqCluster
type RabbitmqClusterStatus struct {
	// Set of Conditions describing the current state of the RabbitmqCluster
	Conditions []RabbitmqClusterCondition `json:"conditions"`

	// Identifying information on internal resources
	DefaultUser *RabbitmqClusterDefaultUser `json:"defaultUser,omitempty"`

	// Binding exposes a secret containing the binding information for this
	// RabbitmqCluster. It implements the service binding Provisioned Service
	// duck type. See: https://github.com/servicebinding/spec#provisioned-service
	Binding *corev1.LocalObjectReference `json:"binding,omitempty"`

	// observedGeneration is the most recent successful generation observed for this RabbitmqCluster. It corresponds to the
	// RabbitmqCluster's generation, which is updated on mutation by the API Server.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// RabbitmqClusterDefaultUser contains references to resources created with the RabbitmqCluster resource.
type RabbitmqClusterDefaultUser struct {
	// Reference to the Kubernetes Secret containing the credentials of the default
	// user.
	SecretReference *RabbitmqClusterSecretReference `json:"secretReference,omitempty"`
	// Reference to the Kubernetes Service serving the cluster.
	ServiceReference *RabbitmqClusterServiceReference `json:"serviceReference,omitempty"`
}

// RabbitmqClusterSecretReference reference to the Kubernetes Secret containing the credentials of the default user.
type RabbitmqClusterSecretReference struct {
	// Name of the Secret containing the default user credentials
	Name string `json:"name"`
	// Namespace of the Secret containing the default user credentials
	Namespace string `json:"namespace"`
	// Key-value pairs in the Secret corresponding to `username`, `password`, `host`, and `port`
	Keys map[string]string `json:"keys"`
}

// RabbitmqClusterServiceReference reference to the Kubernetes Service serving the cluster.
type RabbitmqClusterServiceReference struct {
	// Name of the Service serving the cluster
	Name string `json:"name"`
	// Namespace of the Service serving the cluster
	Namespace string `json:"namespace"`
}

type RabbitmqClusterCondition struct {
	// Type indicates the scope of RabbitmqCluster status addressed by the condition.
	Type RabbitmqClusterConditionType `json:"type"`
	// True, False, or Unknown
	Status corev1.ConditionStatus `json:"status"`
	// The last time this Condition type changed.
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
	// One word, camel-case reason for current status of the condition.
	Reason string `json:"reason,omitempty"`
	// Full text reason for current status of the condition.
	Message string `json:"message,omitempty"`
}

type RabbitmqClusterConditionType string

const (
	AllReplicasReady RabbitmqClusterConditionType = "AllReplicasReady"
	ClusterAvailable RabbitmqClusterConditionType = "ClusterAvailable"
	NoWarnings       RabbitmqClusterConditionType = "NoWarnings"
	ReconcileSuccess RabbitmqClusterConditionType = "ReconcileSuccess"
)

func (condition *RabbitmqClusterCondition) UpdateState(status corev1.ConditionStatus) {
	if condition.Status != status {
		condition.LastTransitionTime = metav1.Now()
	}
	condition.Status = status
}

func (condition *RabbitmqClusterCondition) UpdateReason(reason string, messages ...string) {
	condition.Reason = reason
	condition.Message = strings.Join(messages, ". ")
}
