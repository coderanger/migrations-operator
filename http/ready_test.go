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

package http

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	cu "github.com/coderanger/controller-utils"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	migrationsv1beta1 "github.com/coderanger/migrations-operator/api/v1beta1"
)

var _ = Describe("Ready API", func() {
	var helper *cu.FunctionalHelper
	var obj *migrationsv1beta1.Migrator

	BeforeEach(func() {
		obj = &migrationsv1beta1.Migrator{
			ObjectMeta: metav1.ObjectMeta{Name: "testing"},
			Spec: migrationsv1beta1.MigratorSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "testing",
					},
				},
			},
		}
		helper = suiteHelper.MustStart(APIServer)
		helper.TestClient.Create(obj)
	})

	AfterEach(func() {
		helper.MustStop()
		helper = nil
	})

	post := func(image, name string) bool {
		args := &ReadyArgs{TargetImage: image, MigratorNamespace: helper.Namespace, MigratorName: name}
		data, err := json.Marshal(args)
		Expect(err).ToNot(HaveOccurred())
		resp, err := http.Post(url+"api/ready", "application/json", bytes.NewBuffer(data))
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(200))
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		Expect(err).ToNot(HaveOccurred())
		return string(body) == "true"
	}

	It("returns false with no Migrator", func() {
		ready := post("myapp:latest", "other")
		Expect(ready).To(BeFalse())
	})

	It("returns true with a valid Migrator", func() {
		obj.Status.LastSuccessfulMigration = "myapp:latest"
		helper.TestClient.Status().Update(obj)
		ready := post("myapp:latest", "testing")
		Expect(ready).To(BeTrue())
	})

	It("returns false with a Migrator on the wrong version", func() {
		obj.Status.LastSuccessfulMigration = "myapp:latest"
		helper.TestClient.Status().Update(obj)
		ready := post("myapp:v2", "testing")
		Expect(ready).To(BeFalse())
	})
})
