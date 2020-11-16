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
	"net/http"

	cu "github.com/coderanger/controller-utils"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("API Server", func() {
	var helper *cu.FunctionalHelper

	BeforeEach(func() {
		helper = suiteHelper.MustStart(APIServer)
	})

	AfterEach(func() {
		helper.MustStop()
		helper = nil
	})

	It("responds to HTTP requests", func() {
		resp, err := http.Get(url)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(404))
	})
})
