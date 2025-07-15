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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	cu "github.com/coderanger/controller-utils"
	"github.com/pkg/errors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	migrationsv1beta1 "github.com/coderanger/migrations-operator/api/v1beta1"
	"github.com/coderanger/migrations-operator/utils"
	"github.com/coderanger/migrations-operator/webhook"
)

type migrationsComponent struct{}

func Migrations() *migrationsComponent {
	return &migrationsComponent{}
}

func (_ *migrationsComponent) GetReadyCondition() string {
	return "MigrationsReady"
}

func (comp *migrationsComponent) Setup(ctx *cu.Context, bldr *ctrl.Builder) error {
	bldr.Owns(&batchv1.Job{})
	bldr.Watches(
		&source.Kind{Type: &corev1.Pod{}},
		handler.EnqueueRequestsFromMapFunc(func(obj client.Object) []reconcile.Request {
			// Obj is a Pod that just got an event, map it back to any matching Migrators.
			requests := []reconcile.Request{}
			// Find any Migrator objects that match this pod.
			migrators, err := utils.ListMatchingMigrators(context.Background(), ctx.Client, obj)
			if err != nil {
				ctx.Log.Error(err, "error listing matching migrators")
				// TODO Metric to track this for alerting.
				return requests
			}
			for _, migrator := range migrators {
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      migrator.Name,
						Namespace: migrator.Namespace,
					},
				})
			}
			return requests
		}),
	)
	return nil
}

func deepCopyJSON(src map[string]interface{}, dest map[string]interface{}) error {
	if src == nil {
		return errors.New("src is nil. You cannot read from a nil map")
	}
	if dest == nil {
		return errors.New("dest is nil. You cannot insert to a nil map")
	}
	jsonStr, err := json.Marshal(src)
	if err != nil {
		return err
	}
	err = json.Unmarshal(jsonStr, &dest)
	if err != nil {
		return err
	}
	return nil
}

func (comp *migrationsComponent) Reconcile(ctx *cu.Context) (cu.Result, error) {
	obj := ctx.Object.(*migrationsv1beta1.Migrator)

	// Create the selectors.
	rawSelector := obj.Spec.Selector
	if rawSelector == nil {
		rawSelector = &metav1.LabelSelector{MatchLabels: map[string]string{}}
	}
	selector, err := metav1.LabelSelectorAsSelector(rawSelector)
	if err != nil {
		return cu.Result{}, errors.Wrap(err, "error parsing selector")
	}
	rawSelector = obj.Spec.TemplateSelector
	if rawSelector == nil {
		rawSelector = &metav1.LabelSelector{MatchLabels: map[string]string{}}
	}
	templateSelector, err := metav1.LabelSelectorAsSelector(rawSelector)
	if err != nil {
		return cu.Result{}, errors.Wrap(err, "error parsing template selector")
	}

	// Find a template pod to start from.
	allPods := &unstructured.UnstructuredList{}
	allPods.SetAPIVersion("v1")
	allPods.SetKind("Pod")
	err = ctx.Client.List(ctx, allPods, &client.ListOptions{Namespace: obj.Namespace})
	if err != nil {
		return cu.Result{}, errors.Wrapf(err, "error listing pods in namespace %s", obj.Namespace)
	}
	pods := []*unstructured.Unstructured{}
	var templatePod *unstructured.Unstructured
	for i := range allPods.Items {
		pod := &allPods.Items[i]
		labelSet := labels.Set(pod.GetLabels())
		if selector.Matches(labelSet) {
			pods = append(pods, pod)
			if templatePod == nil && templateSelector.Matches(labelSet) {
				templatePod = pod
			}
		}
	}
	if len(pods) == 0 {
		// No matching pods, just bail out for now.
		return cu.Result{}, nil
	}
	if templatePod == nil {
		// We had at least one matching pod, but no valid templates, error out.
		return cu.Result{}, errors.New("no template pods found")
	}

	// Find the template pod spec, possibly from an owner object.
	templatePodSpec, err := comp.findOwnerSpec(ctx, templatePod)
	if err != nil {
		return cu.Result{}, errors.Wrap(err, "error finding template pod spec")
	}

	// Find the template container.
	templatePodSpecContainers := templatePodSpec["containers"].([]interface{})
	var templateContainer map[string]interface{}
	if obj.Spec.Container != "" {
		// Looking for a specific container name.
		for _, c := range templatePodSpecContainers {
			container := c.(map[string]interface{})
			if container["name"].(string) == obj.Spec.Container {
				templateContainer = container
				break
			}
		}
	} else if len(templatePodSpecContainers) > 0 {
		templateContainer = templatePodSpecContainers[0].(map[string]interface{})
	}
	if templateContainer == nil {
		// Welp, either nothing matched the name or somehow there are no containers.
		return cu.Result{}, errors.New("no template container found")
	}

	// Build a migration job object.
	migrationContainer := make(map[string]interface{})
	err = deepCopyJSON(templateContainer, migrationContainer)
	if err != nil {
		return cu.Result{}, errors.Wrap(err, "error copying template container")
	}
	migrationContainer["name"] = "migrations"
	if obj.Spec.Image != "" {
		migrationContainer["image"] = obj.Spec.Image
	}
	if obj.Spec.Command != nil {
		migrationContainer["command"] = *obj.Spec.Command
	}
	if obj.Spec.Args != nil {
		migrationContainer["args"] = *obj.Spec.Args
	}
	// TODO resources?

	// Remove the probes since they will rarely work.
	migrationContainer["readinessProbe"] = nil
	migrationContainer["livenessProbe"] = nil
	migrationContainer["startupProbe"] = nil

	migrationPodSpec := make(map[string]interface{})
	err = deepCopyJSON(templatePodSpec, migrationPodSpec)
	if err != nil {
		return cu.Result{}, errors.Wrap(err, "error copying template pod spec")
	}
	migrationPodSpec["containers"] = []map[string]interface{}{migrationContainer}
	migrationPodSpec["restartPolicy"] = corev1.RestartPolicyNever

	// Purge any migration wait initContainers since that would be a yodawg situation.
	initContainers := []map[string]interface{}{}
	if migrationPodSpec["initContainers"] != nil {
		for _, c := range migrationPodSpec["initContainers"].([]interface{}) {
			container := c.(map[string]interface{})
			if !strings.HasPrefix(container["name"].(string), "migrate-wait-") {
				initContainers = append(initContainers, container)
			}
		}
	}
	migrationPodSpec["initContainers"] = initContainers

	// add labels to the job's pod template
	jobTemplateLabels := map[string]string{"migrations": obj.Name}
	if obj.Spec.Labels != nil {
		for k, v := range obj.Spec.Labels {
			jobTemplateLabels[k] = v
		}
	}

	// add annotations to the job's pod template
	jobTemplateAnnotations := map[string]string{
		webhook.NOWAIT_MIGRATOR_ANNOTATION: "true",
	}
	if obj.Spec.Annotations != nil {
		for k, v := range obj.Spec.Annotations {
			jobTemplateAnnotations[k] = v
		}
	}

	migrationJobName := obj.Name + "-migrations"
	migrationJobNamespace := obj.Namespace
	migrationJobImage := migrationContainer["image"].(string)
	migrationJob := &unstructured.Unstructured{}
	migrationJob.SetAPIVersion("batch/v1")
	migrationJob.SetKind("Job")
	migrationJob.SetName(migrationJobName)
	migrationJob.SetNamespace(migrationJobNamespace)
	migrationJob.SetLabels(obj.Labels)
	migrationJob.UnstructuredContent()["spec"] = map[string]interface{}{
		"template": map[string]interface{}{
			"metadata": map[string]interface{}{
				"labels":      jobTemplateLabels,
				"annotations": jobTemplateAnnotations,
			},
			"spec": migrationPodSpec,
		},
	}
	err = controllerutil.SetControllerReference(obj, migrationJob, ctx.Scheme)
	if err != nil {
		return cu.Result{}, errors.Wrap(err, "error setting controller reference")
	}

	// Check if we're already up to date.
	uncachedObj := &migrationsv1beta1.Migrator{}
	err = ctx.UncachedClient.Get(ctx, types.NamespacedName{Name: obj.Name, Namespace: obj.Namespace}, uncachedObj)
	if err != nil {
		return cu.Result{}, errors.Wrap(err, "error getting latest migrator for status")
	}
	if uncachedObj.Status.LastSuccessfulMigration == migrationJobImage {
		ctx.Conditions.SetfTrue(comp.GetReadyCondition(), "MigrationsUpToDate", "Migration %s already run", migrationJobImage)
		return cu.Result{}, nil
	}

	existingJob := &batchv1.Job{}
	err = ctx.Client.Get(ctx, types.NamespacedName{Name: migrationJobName, Namespace: migrationJobNamespace}, existingJob)
	if err != nil {
		if kerrors.IsNotFound(err) {
			// Try to start the migrations.
			err = ctx.Client.Create(ctx, migrationJob, &client.CreateOptions{FieldManager: ctx.FieldManager})
			if err != nil {
				// Possible race condition, try again.
				ctx.Events.Eventf(obj, "Warning", "CreateError", "Error on create, possible conflict: %v", err)
				ctx.Conditions.SetfUnknown(comp.GetReadyCondition(), "CreateError", "Error on create, possible conflict: %v", err)
				return cu.Result{Requeue: true}, nil
			}
			ctx.Events.Eventf(obj, "Normal", "MigrationsStarted", "Started migration job %s/%s using image %s", migrationJobNamespace, migrationJobName, migrationJobImage)
			ctx.Conditions.SetfFalse(comp.GetReadyCondition(), "MigrationsRunning", "Started migration job %s/%s using image %s", migrationJobNamespace, migrationJobName, migrationJobImage)
			return cu.Result{}, nil
		} else {
			return cu.Result{}, errors.Wrapf(err, "error getting existing migration job %s/%s", migrationJobNamespace, migrationJobName)
		}
	}

	// Check if the existing job is stale, i.e. was for a previous migration image.
	var existingImage string
	if len(existingJob.Spec.Template.Spec.Containers) > 0 {
		existingImage = existingJob.Spec.Template.Spec.Containers[0].Image
	}
	if existingImage == "" || existingImage != migrationJobImage {
		// Old, stale migration. Remove it and try again.
		policy := metav1.DeletePropagationForeground
		err = ctx.Client.Delete(ctx, existingJob, &client.DeleteOptions{PropagationPolicy: &policy})
		if err != nil {
			return cu.Result{}, errors.Wrapf(err, "error deleting stale migration job %s/%s", existingJob.Namespace, existingJob.Name)
		}
		ctx.Events.Eventf(obj, "Normal", "StaleJob", "Deleted stale migration job %s/%s (%s)", migrationJobNamespace, migrationJobName, existingImage)
		ctx.Conditions.SetfFalse(comp.GetReadyCondition(), "StaleJob", "Deleted stale migration job %s/%s (%s)", migrationJobNamespace, migrationJobName, existingImage)
		return cu.Result{RequeueAfter: 1 * time.Second, SkipRemaining: true}, nil
	}

	// Check if the job succeeded.
	if existingJob.Status.Succeeded > 0 {
		// Success! Update LastSuccessfulMigration and delete the job.
		err = ctx.Client.Delete(ctx.Context, existingJob, client.PropagationPolicy(metav1.DeletePropagationBackground))
		if err != nil {
			return cu.Result{}, errors.Wrapf(err, "error deleting successful migration job %s/%s", existingJob.Namespace, existingJob.Name)
		}
		ctx.Events.Eventf(obj, "Normal", "MigrationsSucceeded", "Migration job %s/%s using image %s succeeded", existingJob.Namespace, existingJob.Name, existingImage)
		ctx.Conditions.SetfTrue(comp.GetReadyCondition(), "MigrationsSucceeded", "Migration job %s/%s using image %s succeeded", existingJob.Namespace, existingJob.Name, existingImage)
		obj.Status.LastSuccessfulMigration = migrationJobImage
		return cu.Result{}, nil
	}

	// ... Or if the job failed.
	if existingJob.Status.Failed > 0 {
		// If it was an outdated job, we would have already deleted it, so this means it's a failed migration for the current version.
		ctx.Events.Eventf(obj, "Warning", "MigrationsFailed", "Migration job %s/%s using image %s failed", existingJob.Namespace, existingJob.Name, existingImage)
		ctx.Conditions.SetfFalse(comp.GetReadyCondition(), "MigrationsFailed", "Migration job %s/%s using image %s failed", existingJob.Namespace, existingJob.Name, existingImage)
		return cu.Result{}, nil
	}

	// Job is still running, will get reconciled when it finishes.
	ctx.Conditions.SetfFalse(comp.GetReadyCondition(), "MigrationsRunning", "Migration job %s/%s using image %s still running", existingJob.Namespace, existingJob.Name, existingImage)
	return cu.Result{}, nil
}

func (_ *migrationsComponent) findOwners(ctx *cu.Context, obj *unstructured.Unstructured) ([]*unstructured.Unstructured, error) {
	namespace := obj.GetNamespace()
	owners := []*unstructured.Unstructured{}
	for {
		owners = append(owners, obj)
		ref := metav1.GetControllerOfNoCopy(obj)
		if ref == nil {
			break
		}
		gvk := schema.FromAPIVersionAndKind(ref.APIVersion, ref.Kind)
		obj = &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)
		obj.SetName(ref.Name) // Is this needed?
		err := ctx.Client.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: namespace}, obj)
		if err != nil {
			// Gracefully handle objects we don't have access to
			if kerrors.IsForbidden(err) {
				break
			}
			return nil, errors.Wrapf(err, "error finding object type for owner reference %v", ref)
		}
	}
	// Reverse the slice so it goes top -> bottom.
	for i, j := 0, len(owners)-1; i < j; i, j = i+1, j-1 {
		owners[i], owners[j] = owners[j], owners[i]
	}
	return owners, nil
}

func (_ *migrationsComponent) findSpecFor(ctx *cu.Context, obj *unstructured.Unstructured) map[string]interface{} {
	gvk := obj.GetObjectKind().GroupVersionKind()
	switch fmt.Sprintf("%s/%s", gvk.Group, gvk.Kind) {
	case "/Pod":
		return obj.UnstructuredContent()["spec"].(map[string]interface{})
	case "apps/Deployment":
		spec := obj.UnstructuredContent()["spec"].(map[string]interface{})
		template := spec["template"].(map[string]interface{})
		return template["spec"].(map[string]interface{})
	case "argoproj.io/Rollout":
		spec := obj.UnstructuredContent()["spec"].(map[string]interface{})
		if spec["workloadRef"] != nil {
			workloadRef := spec["workloadRef"].(map[string]interface{})
			workloadKind := workloadRef["kind"].(string)
			if workloadKind == "Deployment" {
				deployment := &unstructured.Unstructured{}
				deployment.SetAPIVersion(workloadRef["apiVersion"].(string))
				deployment.SetKind(workloadKind)
				err := ctx.Client.Get(ctx, types.NamespacedName{Name: workloadRef["name"].(string), Namespace: obj.GetNamespace()}, deployment)
				if err != nil {
					return nil
				}
				deploymentSpec := deployment.UnstructuredContent()["spec"].(map[string]interface{})
				deploymentTemplate := deploymentSpec["template"].(map[string]interface{})
				return deploymentTemplate["spec"].(map[string]interface{})
			} else {
				// TODO handle other WorkloadRef types
				return nil
			}
		}
		template := spec["template"].(map[string]interface{})
		return template["spec"].(map[string]interface{})
	default:
		return nil
	}
}

func (comp *migrationsComponent) findOwnerSpec(ctx *cu.Context, obj *unstructured.Unstructured) (map[string]interface{}, error) {
	owners, err := comp.findOwners(ctx, obj)
	if err != nil {
		return nil, err
	}
	for _, owner := range owners {
		spec := comp.findSpecFor(ctx, owner)
		if spec != nil {
			return spec, nil
		}
	}
	// This should be impossible since the top-level input is always a corev1.Pod.
	return nil, errors.Errorf("error finding pod spec for %s %s/%s", obj.GetObjectKind().GroupVersionKind(), obj.GetNamespace(), obj.GetName())
}
