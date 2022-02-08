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

package components

import (
	"context"

	cu "github.com/coderanger/controller-utils"
	. "github.com/coderanger/controller-utils/tests/matchers"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	migrationsv1beta1 "github.com/coderanger/migrations-operator/api/v1beta1"
)

var _ = Describe("Migrations component", func() {
	var obj *migrationsv1beta1.Migrator
	var pod *corev1.Pod
	var job *batchv1.Job
	var helper *cu.UnitHelper

	BeforeEach(func() {
		comp := Migrations()
		obj = &migrationsv1beta1.Migrator{
			Spec: migrationsv1beta1.MigratorSpec{},
		}
		pod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "testpod",
				Namespace: "default",
				Labels:    map[string]string{},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "main",
						Image: "myapp:latest",
					},
				},
			},
		}
		job = &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "testing-migrations",
				Namespace: "default",
			},
			Spec: batchv1.JobSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "migrations",
								Image: "myapp:latest",
							},
						},
					},
				},
			},
		}
		helper = suiteHelper.Setup(comp, obj)
	})

	It("does nothing with no pods", func() {
		helper.MustReconcile()
		Expect(obj).ToNot(HaveCondition("MigrationsReady"))
	})

	It("does nothing with a non-matching pod", func() {
		obj.Spec.Selector = &metav1.LabelSelector{
			MatchLabels: map[string]string{"app": "foo"},
		}
		pod.Labels["app"] = "other"
		helper.TestClient.Create(pod)
		helper.MustReconcile()
		Expect(obj).ToNot(HaveCondition("MigrationsReady"))
		err := helper.Client.Get(context.Background(), types.NamespacedName{Name: "testing", Namespace: "default"}, job)
		Expect(kerrors.IsNotFound(err)).To(BeTrue())
	})

	It("errors with a matching pod but no matching template", func() {
		obj.Spec.TemplateSelector = &metav1.LabelSelector{
			MatchLabels: map[string]string{"app": "foo"},
		}
		pod.Labels["app"] = "other"
		helper.TestClient.Create(pod)
		_, err := helper.Reconcile()
		Expect(err).To(MatchError("no template pods found"))
	})

	It("starts a migration", func() {
		helper.TestClient.Create(pod)
		helper.MustReconcile()
		Expect(helper.Events).To(Receive(Equal("Normal MigrationsStarted Started migration job default/testing-migrations using image myapp:latest")))
		Expect(obj).To(HaveCondition("MigrationsReady").WithReason("MigrationsRunning").WithStatus("False"))
		helper.TestClient.GetName("testing-migrations", job)
		Expect(job.Spec.Template.Spec.Containers[0].Name).To(Equal("migrations"))
		Expect(job.Spec.Template.Spec.Containers[0].Image).To(Equal("myapp:latest"))
	})

	It("leaves an existing, matching job", func() {
		helper.TestClient.Create(pod)
		helper.TestClient.Create(job)
		helper.MustReconcile()
		Expect(obj).To(HaveCondition("MigrationsReady").WithReason("MigrationsRunning").WithStatus("False"))
		job2 := &batchv1.Job{}
		helper.TestClient.GetName("testing-migrations", job2)
		Expect(job.Spec).To(Equal(job2.Spec))
	})

	It("deletes a stale job", func() {
		helper.TestClient.Create(pod)
		job.Spec.Template.Spec.Containers[0].Image = "other"
		helper.TestClient.Create(job)
		helper.MustReconcile()
		Expect(obj).To(HaveCondition("MigrationsReady").WithReason("StaleJob").WithStatus("False"))
		err := helper.Client.Get(context.Background(), types.NamespacedName{Name: "testing-migrations", Namespace: "default"}, job)
		Expect(kerrors.IsNotFound(err)).To(BeTrue())
	})

	It("recognizes a successful job", func() {
		helper.TestClient.Create(pod)
		job.Status.Succeeded = 1
		helper.TestClient.Create(job)
		helper.MustReconcile()
		Expect(obj).To(HaveCondition("MigrationsReady").WithStatus("True"))
		err := helper.Client.Get(context.Background(), types.NamespacedName{Name: "testing-migrations", Namespace: "default"}, job)
		Expect(kerrors.IsNotFound(err)).To(BeTrue())
	})

	It("recognizes a failed job", func() {
		helper.TestClient.Create(pod)
		job.Status.Failed = 1
		helper.TestClient.Create(job)
		helper.MustReconcile()
		Expect(obj).To(HaveCondition("MigrationsReady").WithStatus("False").WithReason("MigrationsFailed"))
		job2 := &batchv1.Job{}
		helper.TestClient.GetName("testing-migrations", job2)
		Expect(job.Spec).To(Equal(job2.Spec))
	})

	It("it uses a template pod if specified", func() {
		obj.Spec.TemplateSelector = &metav1.LabelSelector{
			MatchLabels: map[string]string{"app": "two"},
		}
		pod1 := pod.DeepCopy()
		pod1.Name = "one"
		pod1.Labels["app"] = "one"
		pod1.Spec.Containers[0].Image = "one:latest"
		helper.TestClient.Create(pod1)
		pod2 := pod.DeepCopy()
		pod2.Name = "two"
		pod2.Labels["app"] = "two"
		pod2.Spec.Containers[0].Image = "two:latest"
		helper.TestClient.Create(pod2)
		helper.MustReconcile()
		helper.TestClient.GetName("testing-migrations", job)
		Expect(job.Spec.Template.Spec.Containers[0].Image).To(Equal("two:latest"))
	})

	It("applies image override", func() {
		obj.Spec.Image = "other:1"
		helper.TestClient.Create(pod)
		helper.MustReconcile()
		helper.TestClient.GetName("testing-migrations", job)
		Expect(job.Spec.Template.Spec.Containers[0].Image).To(Equal("other:1"))
	})

	It("applies command override", func() {
		command := []string{"run", "migrations"}
		obj.Spec.Command = &command
		helper.TestClient.Create(pod)
		helper.MustReconcile()
		helper.TestClient.GetName("testing-migrations", job)
		Expect(job.Spec.Template.Spec.Containers[0].Command).To(Equal([]string{"run", "migrations"}))
	})

	It("applies args override", func() {
		args := []string{"run", "migrations"}
		obj.Spec.Args = &args
		helper.TestClient.Create(pod)
		helper.MustReconcile()
		helper.TestClient.GetName("testing-migrations", job)
		Expect(job.Spec.Template.Spec.Containers[0].Args).To(Equal([]string{"run", "migrations"}))
	})

	It("follows owner references for a deployment", func() {
		truep := true
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "testing"},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "main",
								Image: "myapp:v1",
							},
						},
					},
				},
			},
		}
		helper.TestClient.Create(deployment)
		rs := &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testing-1234",
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "testing",
						Controller: &truep,
					},
				},
			},
		}
		helper.TestClient.Create(rs)
		pod.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion: "apps/v1",
				Kind:       "ReplicaSet",
				Name:       "testing-1234",
				Controller: &truep,
			},
		}
		helper.TestClient.Create(pod)

		helper.MustReconcile()
		helper.TestClient.GetName("testing-migrations", job)
		Expect(job.Spec.Template.Spec.Containers[0].Image).To(Equal("myapp:v1"))
	})

	It("applies specified labels to the migration pod", func() {
		obj.Spec.Labels = map[string]string{"key1": "value1"}
		helper.TestClient.Create(pod)
		helper.MustReconcile()
		helper.TestClient.GetName("testing-migrations", job)
		Expect(job.Spec.Template.ObjectMeta.Labels).To(HaveKeyWithValue("key1", "value1"))
		Expect(job.Spec.Template.ObjectMeta.Labels).To(HaveKeyWithValue("migrations", "testing"))
	})
})
