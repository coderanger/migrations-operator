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

package controllers

import (
	"context"
	"os"

	cu "github.com/coderanger/controller-utils"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	migrationsv1beta1 "github.com/coderanger/migrations-operator/api/v1beta1"
	"github.com/coderanger/migrations-operator/webhook"
)

var _ = Describe("Migrator controller", func() {
	var helper *cu.FunctionalHelper

	BeforeEach(func() {
		os.Setenv("API_HOSTNAME", "migrations-operator.migration-operator.svc")
		os.Setenv("WAITER_IMAGE", "migrations-operator:latest")
		helper = suiteHelper.MustStart(Migrator, webhook.InitInjector)
	})

	AfterEach(func() {
		helper.MustStop()
		helper = nil
	})

	It("runs a basic reconcile", func() {
		c := helper.TestClient

		migrator := &migrationsv1beta1.Migrator{
			ObjectMeta: metav1.ObjectMeta{Name: "testing"},
			Spec: migrationsv1beta1.MigratorSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "test",
					},
				},
			},
		}
		c.Create(migrator)

		// Create a pod.
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testpod",
				Labels: map[string]string{
					"app": "test",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "main",
						Image: "myapp:v1",
					},
				},
			},
		}
		c.Create(pod.DeepCopy())

		// Mark the job as successful.
		job := &batchv1.Job{}
		c.EventuallyGetName("testing", job)
		job.Status.Succeeded = 1
		c.Status().Update(job)

		// Make sure the migrator is ready.
		c.EventuallyGetName("testing", migrator, c.EventuallyReady())
		Expect(migrator.Status.LastSuccessfulMigration).To(Equal("myapp:v1"))

		// Make sure the job doesn't come back.
		Consistently(func() error {
			return helper.Client.Get(context.Background(), types.NamespacedName{Name: "testing", Namespace: helper.Namespace}, job)
		}, 5).Should(MatchError("Job.batch \"testing\" not found"))

		// Update the pod image and see if a new migration is run.
		c.Delete(pod)
		pod.Spec.Containers[0].Image = "myapp:v2"
		c.Create(pod.DeepCopy())

		// Hold until unready.
		c.EventuallyGetName("testing", migrator, c.EventuallyCondition("Ready", "False"))

		// Mark the new job as successful.
		c.EventuallyGetName("testing", job)
		job.Status.Succeeded = 1
		c.Status().Update(job)

		// Make sure the migrator is ready, again.
		c.EventuallyGetName("testing", migrator, c.EventuallyReady())
		Expect(migrator.Status.LastSuccessfulMigration).To(Equal("myapp:v2"))
	})
})
