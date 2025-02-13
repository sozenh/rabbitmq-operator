package status_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mqv1beta1 "github.com/rabbitmq/cluster-operator/v2/api/v1beta1"
)

var _ = Describe("Status", func() {

	Context("Condition method", func() {
		var (
			someCondition     mqv1beta1.RabbitmqClusterCondition
			someConditionTime metav1.Time
		)

		BeforeEach(func() {
			someConditionTime = metav1.Unix(1, 1)
			someCondition = mqv1beta1.RabbitmqClusterCondition{
				Type:               "a-type",
				Status:             "some-status",
				LastTransitionTime: *someConditionTime.DeepCopy(),
				Reason:             "reasons",
				Message:            "ship-it",
			}
		})

		It("changes the status and transition time", func() {
			someCondition.UpdateState("maybe")
			Expect(someCondition.Status).To(Equal(corev1.ConditionStatus("maybe")))

			Expect(someCondition.LastTransitionTime).NotTo(Equal(someConditionTime))
			Expect(someCondition.LastTransitionTime.Before(&someConditionTime)).To(BeFalse(),
				"Actual transition time %v is before Expected transition time %v", someCondition.LastTransitionTime, someConditionTime)
		})

		It("preserves the status and transition time", func() {
			someCondition.UpdateState("some-status")
			Expect(someCondition.Status).To(Equal(corev1.ConditionStatus("some-status")))
			Expect(someCondition.LastTransitionTime).To(Equal(someConditionTime))
		})

		It("changes reason and message", func() {
			someCondition.UpdateReason("my-reason", "my-message")
			Expect(someCondition.Reason).To(Equal("my-reason"))
			Expect(someCondition.Message).To(Equal("my-message"))
		})
	})

})
