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
	"fmt"
	"os"
	"time"

	cu "github.com/coderanger/controller-utils"
	. "github.com/onsi/ginkgo"

	// . "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	migrationsv1beta1 "github.com/coderanger/migrations-operator/api/v1beta1"
	"github.com/coderanger/migrations-operator/controllers"
	"github.com/coderanger/migrations-operator/http"
	"github.com/coderanger/migrations-operator/webhook"
)

var _ = Describe("Integration", func() {
	var helper *cu.FunctionalHelper

	BeforeEach(func() {
		extName := os.Getenv("INTEGRATION_EXTERNAL_NAME")
		image := os.Getenv("INTEGRATION_IMAGE_NAME")
		if extName == "" || image == "" {
			Skip("Integration tests require $INTEGRATION_EXTERNAL_NAME and $INTEGRATION_IMAGE_NAME")
		}
		os.Setenv("API_HOSTNAME", fmt.Sprintf("%s:5000", extName))
		os.Setenv("WAITER_IMAGE", image)

		helper = suiteHelper.MustStart(
			controllers.Migrator,
			webhook.InitInjector,
			http.APIServer,
		)
	})

	AfterEach(func() {
		helper.MustStop()
		helper = nil
	})

	It("succesfully migrates", func() {
		c := helper.TestClient

		postgres := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "postgres"},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "postgres",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "postgres"}},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "postgres",
								Image: "postgres",
								Ports: []corev1.ContainerPort{
									{
										ContainerPort: 5432,
									},
								},
								Env: []corev1.EnvVar{
									{
										Name:  "POSTGRES_PASSWORD",
										Value: "secret",
									},
								},
							},
						},
					},
				},
			},
		}
		c.Create(postgres)

		postgresService := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "postgres"},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{
					"app": "postgres",
				},
				Ports: []corev1.ServicePort{
					{
						Port: 5432,
					},
				},
			},
		}
		c.Create(postgresService)

		migrator := &migrationsv1beta1.Migrator{
			ObjectMeta: metav1.ObjectMeta{Name: "testing"},
			Spec: migrationsv1beta1.MigratorSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "testing",
					},
				},
				Command: &[]string{"python", "manage.py", "migrate"},
			},
		}
		c.Create(migrator)

		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "testing"},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "testing",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "testing"}},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "main",
								Image: "ghcr.io/coderanger/migrations-operator-django-test:v1",
								Env: []corev1.EnvVar{
									{
										Name:  "DATABASE_URL",
										Value: "postgres://postgres:secret@postgres/postgres",
									},
								},
								ReadinessProbe: &corev1.Probe{
									ProbeHandler: corev1.ProbeHandler{
										HTTPGet: &corev1.HTTPGetAction{
											Path: "/",
											Port: intstr.FromInt(8000),
										},
									},
								},
							},
						},
					},
				},
			},
		}
		c.Create(deployment)

		c.EventuallyGetName("testing", deployment, c.EventuallyCondition("Available", "True"), c.EventuallyTimeout(5*time.Minute))

		deployment.Spec.Template.Spec.Containers[0].Image = "ghcr.io/coderanger/migrations-operator-django-test:v2"
		c.Update(deployment)

		c.EventuallyGetName("testing", deployment, c.EventuallyCondition("Available", "True"), c.EventuallyTimeout(5*time.Minute))

		// fmt.Printf("Sleeping for 10 minutes\n")
		// time.Sleep(600 * time.Second)
	})
})
