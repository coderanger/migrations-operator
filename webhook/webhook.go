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
	"fmt"
	"net/http"
	"os"

	"github.com/pkg/errors"
	jsonpatch "gomodules.xyz/jsonpatch/v2"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/coderanger/migrations-operator/utils"
)

const REQUIRE_MIGRATOR_ANNOTATION = "migrations.coderanger.net/required"
const NOWAIT_MIGRATOR_ANNOTATION = "migrations.coderanger.net/no-wait"

// +kubebuilder:webhook:path=/mutate-v1-pod,mutating=true,failurePolicy=ignore,sideEffects=None,groups="",resources=pods,verbs=create;update,versions=v1,name=mpod.migrations.coderanger.net,admissionReviewVersions=v1beta1

// initInjector injects migration initContainers into Pods
type initInjector struct {
	Client  client.Client
	decoder *admission.Decoder
}

func InitInjector(mgr ctrl.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register("/mutate-v1-pod", &webhook.Admission{Handler: &initInjector{Client: mgr.GetClient()}})
	return nil
}

// initInjector adds migration wait initContainers if needed.
func (hook *initInjector) Handle(ctx context.Context, req admission.Request) admission.Response {
	resp, err := hook.handleInner(ctx, req)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return *resp
}

func (hook *initInjector) handleInner(ctx context.Context, req admission.Request) (*admission.Response, error) {
	log := ctrl.Log.WithName("webhooks").WithName("InitInjector")
	// Get the incoming pod.
	pod := &corev1.Pod{}
	err := hook.decoder.Decode(req, pod)
	if err != nil {
		return nil, errors.Wrap(err, "error decoding request")
	}

	// Check for the no-wait annotations.
	ann, ok := pod.Annotations[NOWAIT_MIGRATOR_ANNOTATION]
	if ok && ann == "true" {
		resp := admission.Allowed("skipping migration wait due to annotation")
		return &resp, nil
	}

	// Find any Migrator objects that match this pod.
	migrators, err := utils.ListMatchingMigrators(ctx, hook.Client, pod)
	if err != nil {
		return nil, errors.Wrap(err, "error listing matching migrators")
	}

	// If we have no migrators, check if that's okay.
	if len(migrators) == 0 && pod.Annotations != nil {
		ann, ok := pod.Annotations[REQUIRE_MIGRATOR_ANNOTATION]
		if ok && ann == "true" {
			return nil, errors.New("no migrators found matching pod")
		}
	}

	if len(migrators) == 0 {
		// Nothing to do.
		resp := admission.Allowed("no migrators")
		return &resp, nil
	}

	patches := []jsonpatch.JsonPatchOperation{}
	// Check that initContainers exists at all.
	if len(pod.Spec.InitContainers) == 0 {
		patch := jsonpatch.JsonPatchOperation{
			Operation: "add",
			Path:      "/spec/initContainers",
			Value:     []interface{}{},
		}
		patches = append(patches, patch)
	}

	// For each migrator, inject an initContainer.
	for _, m := range migrators {
		// Look for the container named in the migrator and pull the image from that. If no container
		// matches, fall back to using the first container.
		podIdx := 0
		for i, p := range pod.Spec.Containers {
			if p.Name == m.Spec.Container {
				podIdx = i
			}
		}
		patch := jsonpatch.JsonPatchOperation{
			Operation: "add",
			Path:      "/spec/initContainers/-",
			Value: map[string]interface{}{
				"name":    fmt.Sprintf("migrate-wait-%s", m.Name),
				"image":   os.Getenv("WAITER_IMAGE"),
				"command": []string{"/waiter", pod.Spec.Containers[podIdx].Image, m.Namespace, m.Name, os.Getenv("API_HOSTNAME")},
				"resources": map[string]interface{}{
					"requests": map[string]string{
						"memory": "16M",
						"cpu":    "10m",
					},
				},
			},
		}
		log.Info("Injecting init container", "pod", fmt.Sprintf("%s/%s", req.Namespace, req.Name), "migrator", fmt.Sprintf("%s/%s", m.Namespace, m.Name))
		patches = append(patches, patch)
	}

	resp := admission.Patched("injecting init containers", patches...)
	return &resp, nil
}

// initInjector implements admission.DecoderInjector.
// A decoder will be automatically injected.

// InjectDecoder injects the decoder.
func (hook *initInjector) InjectDecoder(d *admission.Decoder) error {
	hook.decoder = d
	return nil
}
