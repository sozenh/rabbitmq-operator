package resource_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	rabbitmqv1beta1 "github.com/pivotal/rabbitmq-for-kubernetes/api/v1beta1"
	"github.com/pivotal/rabbitmq-for-kubernetes/internal/resource"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("HeadlessService", func() {
	var (
		instance       rabbitmqv1beta1.RabbitmqCluster
		cluster        *resource.RabbitmqResourceBuilder
		serviceBuilder *resource.HeadlessServiceBuilder
		service        *corev1.Service
	)

	BeforeEach(func() {
		instance = rabbitmqv1beta1.RabbitmqCluster{}
		instance.Namespace = "foo"
		instance.Name = "foo"
		cluster = &resource.RabbitmqResourceBuilder{
			Instance: &instance,
		}
		serviceBuilder = cluster.HeadlessService()
	})

	Context("Build with defaults", func() {
		BeforeEach(func() {
			obj, _ := serviceBuilder.Build()
			service = obj.(*corev1.Service)
		})
		It("generates a service object with the correct name", func() {
			Expect(service.Name).To(Equal(instance.ChildResourceName("headless")))
		})

		It("generates a service object with the correct namespace", func() {
			Expect(service.Namespace).To(Equal(instance.Namespace))
		})

		It("generates a service object with the correct label", func() {
			labels := service.Labels
			Expect(labels["app.kubernetes.io/name"]).To(Equal(instance.Name))
			Expect(labels["app.kubernetes.io/component"]).To(Equal("rabbitmq"))
			Expect(labels["app.kubernetes.io/part-of"]).To(Equal("pivotal-rabbitmq"))
		})

		It("generates a service object with the correct selector", func() {
			Expect(service.Spec.Selector["app.kubernetes.io/name"]).To(Equal(instance.Name))
		})

		It("generates a headless service object", func() {
			Expect(service.Spec.ClusterIP).To(Equal("None"))
		})

		It("generates a service object with the right ports exposed", func() {
			epmdPort := corev1.ServicePort{
				Name:     "epmd",
				Port:     4369,
				Protocol: corev1.ProtocolTCP,
			}
			Expect(service.Spec.Ports).Should(ConsistOf(epmdPort))
		})

	})

	Context("Build with instance labels", func() {
		BeforeEach(func() {
			instance.Labels = map[string]string{
				"app.kubernetes.io/foo": "bar",
				"foo":                   "bar",
				"rabbitmq":              "is-great",
				"foo/app.kubernetes.io": "edgecase",
			}
			obj, _ := serviceBuilder.Build()
			service = obj.(*corev1.Service)
		})

		It("has the labels from the CRD on the headless service", func() {
			testLabels(service.Labels)
		})
	})

	Context("Build with instance annotations", func() {
		BeforeEach(func() {
			instance.Annotations = map[string]string{
				"my-annotation":              "i-like-this",
				"kubernetes.io/name":         "i-do-not-like-this",
				"kubectl.kubernetes.io/name": "i-do-not-like-this",
				"k8s.io/name":                "i-do-not-like-this",
			}

			obj, err := serviceBuilder.Build()
			Expect(err).NotTo(HaveOccurred())
			service = obj.(*corev1.Service)
		})

		It("has the annotations from the CRD on the headless service", func() {
			expectedAnnotations := map[string]string{
				"my-annotation": "i-like-this",
			}

			Expect(service.Annotations).To(Equal(expectedAnnotations))
		})
	})

	Context("Update with instance labels", func() {
		BeforeEach(func() {
			instance = rabbitmqv1beta1.RabbitmqCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "rabbit-labelled",
				},
			}
			instance.Labels = map[string]string{
				"app.kubernetes.io/foo": "bar",
				"foo":                   "bar",
				"rabbitmq":              "is-great",
				"foo/app.kubernetes.io": "edgecase",
			}

			service = &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name":      instance.Name,
						"app.kubernetes.io/part-of":   "pivotal-rabbitmq",
						"this-was-the-previous-label": "should-be-deleted",
					},
				},
			}
			err := serviceBuilder.Update(service)
			Expect(err).NotTo(HaveOccurred())
		})

		It("adds labels from the CR", func() {
			testLabels(service.Labels)
		})

		It("restores the default labels", func() {
			labels := service.Labels
			Expect(labels["app.kubernetes.io/name"]).To(Equal(instance.Name))
			Expect(labels["app.kubernetes.io/component"]).To(Equal("rabbitmq"))
			Expect(labels["app.kubernetes.io/part-of"]).To(Equal("pivotal-rabbitmq"))
		})

		It("deletes the labels that are removed from the CR", func() {
			Expect(service.Labels).NotTo(HaveKey("this-was-the-previous-label"))
		})
	})

	Context("Update with instance annotations", func() {
		BeforeEach(func() {
			instance = rabbitmqv1beta1.RabbitmqCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "rabbit-labelled",
				},
			}
			instance.Annotations = map[string]string{
				"my-annotation":              "i-like-this",
				"kubernetes.io/name":         "i-do-not-like-this",
				"kubectl.kubernetes.io/name": "i-do-not-like-this",
				"k8s.io/name":                "i-do-not-like-this",
			}

			service = &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"i-was-here-already":            "please-dont-delete-me",
						"im-here-to-stay.kubernetes.io": "for-a-while",
						"kubernetes.io/name":            "should-stay",
						"kubectl.kubernetes.io/name":    "should-stay",
						"k8s.io/name":                   "should-stay",
					},
				},
			}
			err := serviceBuilder.Update(service)
			Expect(err).NotTo(HaveOccurred())
		})

		It("updates service annotations", func() {
			expectedAnnotations := map[string]string{
				"i-was-here-already":            "please-dont-delete-me",
				"im-here-to-stay.kubernetes.io": "for-a-while",
				"kubernetes.io/name":            "should-stay",
				"kubectl.kubernetes.io/name":    "should-stay",
				"k8s.io/name":                   "should-stay",
				"my-annotation":                 "i-like-this",
			}

			Expect(service.Annotations).To(Equal(expectedAnnotations))
		})
	})

	Context("Update Spec", func() {
		BeforeEach(func() {
			instance = rabbitmqv1beta1.RabbitmqCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "rabbit-spec",
				},
			}
			instance.Labels = map[string]string{
				"app.kubernetes.io/foo": "bar",
				"foo":                   "bar",
				"rabbitmq":              "is-great",
				"foo/app.kubernetes.io": "edgecase",
			}

			service = &corev1.Service{
				Spec: corev1.ServiceSpec{
					ClusterIP: "None",
					Selector: map[string]string{
						"some-selector": "some-tag",
					},
					Ports: []corev1.ServicePort{
						{
							Protocol: corev1.ProtocolTCP,
							Port:     43691,
							Name:     "epmdf",
						},
					},
				},
			}
			err := serviceBuilder.Update(service)
			Expect(err).NotTo(HaveOccurred())
		})

		It("sets the required Spec", func() {
			expectedSpec := corev1.ServiceSpec{
				ClusterIP: "None",
				Selector: map[string]string{
					"app.kubernetes.io/name": "rabbit-spec",
				},
				Ports: []corev1.ServicePort{
					{
						Protocol: corev1.ProtocolTCP,
						Port:     4369,
						Name:     "epmd",
					},
				},
			}

			Expect(service.Spec).To(Equal(expectedSpec))
		})
	})
})
