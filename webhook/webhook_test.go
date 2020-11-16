/*
Copyright 2020 Noah Kantrowitz

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package webhook

import (
	"context"
	"os"

	cu "github.com/coderanger/controller-utils"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	migrationsv1beta1 "github.com/coderanger/migrations-operator/api/v1beta1"
)

var _ = Describe("InitInjector", func() {
	var helper *cu.FunctionalHelper

	BeforeEach(func() {
		os.Setenv("API_HOSTNAME", "migrations-operator.migration-operator.svc")
		os.Setenv("WAITER_IMAGE", "migrations-operator:latest")
		helper = suiteHelper.MustStart(InitInjector)
	})

	AfterEach(func() {
		helper.MustStop()
		helper = nil
	})

	It("does nothing with no migrators", func() {
		c := helper.TestClient

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "testing", Labels: map[string]string{"app": "testing"}},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "main",
						Image: "fake",
					},
				},
			},
		}
		c.Create(pod)

		c.EventuallyGetName("testing", pod)
		Expect(pod.Spec.InitContainers).To(BeEmpty())
	})

	It("injects with a matching migrator", func() {
		c := helper.TestClient

		migrator := &migrationsv1beta1.Migrator{
			ObjectMeta: metav1.ObjectMeta{Name: "testing"},
			Spec: migrationsv1beta1.MigratorSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "testing"},
				},
			},
		}
		c.Create(migrator)

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "testing", Labels: map[string]string{"app": "testing"}},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "main",
						Image: "fake",
					},
				},
			},
		}
		c.Create(pod)

		c.EventuallyGetName("testing", pod)
		Expect(pod.Spec.InitContainers).To(HaveLen(1))
		Expect(pod.Spec.InitContainers[0].Command).To(Equal([]string{"/waiter", "fake", helper.Namespace, "testing", "migrations-operator.migration-operator.svc"}))
	})

	It("doesn't inject with a non-matching migrator", func() {
		c := helper.TestClient

		migrator := &migrationsv1beta1.Migrator{
			ObjectMeta: metav1.ObjectMeta{Name: "testing"},
			Spec: migrationsv1beta1.MigratorSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "other"},
				},
			},
		}
		c.Create(migrator)

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "testing", Labels: map[string]string{"app": "testing"}},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "main",
						Image: "fake",
					},
				},
			},
		}
		c.Create(pod)

		c.EventuallyGetName("testing", pod)
		Expect(pod.Spec.InitContainers).To(BeEmpty())
	})

	It("fails with a missed, expected migrator", func() {
		c := helper.Client

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "testing",
				Namespace:   helper.Namespace,
				Annotations: map[string]string{"migrations.coderanger.net/required": "true"},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "main",
						Image: "fake",
					},
				},
			},
		}
		err := c.Create(context.Background(), pod)
		Expect(err).To(MatchError(ContainSubstring("no migrators found matching pod")))
	})
})
