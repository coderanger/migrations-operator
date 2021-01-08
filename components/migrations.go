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
	"strings"
	"time"

	cu "github.com/coderanger/controller-utils"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

type migrationsComponentWatchMap struct {
	client client.Client
	log    logr.Logger
}

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
		&handler.EnqueueRequestsFromMapFunc{ToRequests: &migrationsComponentWatchMap{client: ctx.Client, log: ctx.Log}},
	)
	return nil
}

// Watch map function used above.
// Obj is a Pod that just got an event, map it back to any matching Migrators.
func (m *migrationsComponentWatchMap) Map(obj handler.MapObject) []reconcile.Request {
	requests := []reconcile.Request{}
	// Find any Migrator objects that match this pod.
	migrators, err := utils.ListMatchingMigrators(context.Background(), m.client, obj.Meta)
	if err != nil {
		m.log.Error(err, "error listing matching migrators")
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
	allPods := &corev1.PodList{}
	err = ctx.Client.List(ctx, allPods, &client.ListOptions{Namespace: obj.Namespace})
	if err != nil {
		return cu.Result{}, errors.Wrapf(err, "error listing pods in namespace %s", obj.Namespace)
	}
	pods := []*corev1.Pod{}
	var templatePod *corev1.Pod
	for i := range allPods.Items {
		pod := &allPods.Items[i]
		labelSet := labels.Set(pod.Labels)
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
	var templateContainer *corev1.Container
	if obj.Spec.Container != "" {
		// Looking for a specific container name.
		for _, c := range templatePodSpec.Containers {
			if c.Name == obj.Spec.Container {
				templateContainer = &c
				break
			}
		}
	} else if len(templatePodSpec.Containers) > 0 {
		templateContainer = &templatePodSpec.Containers[0]
	}
	if templateContainer == nil {
		// Welp, either nothing matched the name or somehow there are no containers.
		return cu.Result{}, errors.New("no template container found")
	}

	// Build a migration job object.
	migrationContainer := templateContainer.DeepCopy()
	migrationContainer.Name = "migrations"
	if obj.Spec.Image != "" {
		migrationContainer.Image = obj.Spec.Image
	}
	if obj.Spec.Command != nil {
		migrationContainer.Command = *obj.Spec.Command
	}
	if obj.Spec.Args != nil {
		migrationContainer.Args = *obj.Spec.Args
	}
	// TODO resources?

	// Remove the probes since they will rarely work.
	migrationContainer.ReadinessProbe = nil
	migrationContainer.LivenessProbe = nil
	migrationContainer.StartupProbe = nil

	migrationPodSpec := templatePodSpec.DeepCopy()
	migrationPodSpec.Containers = []corev1.Container{*migrationContainer}
	migrationPodSpec.RestartPolicy = corev1.RestartPolicyNever

	// Purge any migration wait initContainers since that would be a yodawg situation.
	initContainers := []corev1.Container{}
	for _, c := range migrationPodSpec.InitContainers {
		if !strings.HasPrefix(c.Name, "migrate-wait-") {
			initContainers = append(initContainers, c)
		}
	}
	migrationPodSpec.InitContainers = initContainers

	migrationJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        obj.Name + "-migrations",
			Namespace:   obj.Namespace,
			Labels:      obj.Labels,
			Annotations: map[string]string{},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      map[string]string{"migrations": obj.Name},
					Annotations: map[string]string{webhook.NOWAIT_MIGRATOR_ANNOTATION: "true"},
				},
				Spec: *migrationPodSpec,
			},
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
		return cu.Result{}, errors.Wrap(err, "erro getting latest migrator for status")
	}
	if uncachedObj.Status.LastSuccessfulMigration == migrationContainer.Image {
		ctx.Conditions.SetfTrue(comp.GetReadyCondition(), "MigrationsUpToDate", "Migration %s already run", migrationContainer.Image)
		return cu.Result{}, nil
	}

	existingJob := &batchv1.Job{}
	err = ctx.Client.Get(ctx, types.NamespacedName{Name: migrationJob.Name, Namespace: migrationJob.Namespace}, existingJob)
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
			ctx.Events.Eventf(obj, "Normal", "MigrationsStarted", "Started migration job %s/%s using image %s", migrationJob.Namespace, migrationJob.Name, migrationContainer.Image)
			ctx.Conditions.SetfFalse(comp.GetReadyCondition(), "MigrationsRunning", "Started migration job %s/%s using image %s", migrationJob.Namespace, migrationJob.Name, migrationContainer.Image)
			return cu.Result{}, nil
		} else {
			return cu.Result{}, errors.Wrapf(err, "error getting existing migration job %s/%s", migrationJob.Namespace, migrationJob.Name)
		}
	}

	// Check if the existing job is stale, i.e. was for a previous migration image.
	var existingImage string
	if len(existingJob.Spec.Template.Spec.Containers) > 0 {
		existingImage = existingJob.Spec.Template.Spec.Containers[0].Image
	}
	if existingImage == "" || existingImage != migrationContainer.Image {
		// Old, stale migration. Remove it and try again.
		policy := metav1.DeletePropagationForeground
		err = ctx.Client.Delete(ctx, existingJob, &client.DeleteOptions{PropagationPolicy: &policy})
		if err != nil {
			return cu.Result{}, errors.Wrapf(err, "error deleting stale migration job %s/%s", existingJob.Namespace, existingJob.Name)
		}
		ctx.Events.Eventf(obj, "Normal", "StaleJob", "Deleted stale migration job %s/%s (%s)", migrationJob.Namespace, migrationJob.Name, existingImage)
		ctx.Conditions.SetfFalse(comp.GetReadyCondition(), "StaleJob", "Deleted stale migration job %s/%s (%s)", migrationJob.Namespace, migrationJob.Name, existingImage)
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
		obj.Status.LastSuccessfulMigration = migrationContainer.Image
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

func (_ *migrationsComponent) findOwners(ctx *cu.Context, obj cu.Object) ([]cu.Object, error) {
	namespace := obj.GetNamespace()
	owners := []cu.Object{}
	for {
		owners = append(owners, obj)
		ref := metav1.GetControllerOfNoCopy(obj)
		if ref == nil {
			break
		}
		gvk := schema.FromAPIVersionAndKind(ref.APIVersion, ref.Kind)
		ownerObj, err := ctx.Scheme.New(gvk)
		if err != nil {
			return nil, errors.Wrapf(err, "error finding object type for owner reference %v", ref)
		}
		err = ctx.Client.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: namespace}, ownerObj)
		if err != nil {
			// TODO IMPORTANT If this is a 403, don't bubble up the error. Probably a custom type we don't have access to, just pretend it's not there.
			return nil, errors.Wrapf(err, "error finding object type for owner reference %v", ref)
		}
		obj = ownerObj.(cu.Object)
	}
	// Reverse the slice so it goes top -> bottom.
	for i, j := 0, len(owners)-1; i < j; i, j = i+1, j-1 {
		owners[i], owners[j] = owners[j], owners[i]
	}
	return owners, nil
}

func (_ *migrationsComponent) findSpecFor(obj cu.Object) *corev1.PodSpec {
	switch v := obj.(type) {
	case *corev1.Pod:
		return &v.Spec
	case *appsv1.Deployment:
		return &v.Spec.Template.Spec
	// TODO other types. lots of them.
	default:
		return nil
	}
}

func (comp *migrationsComponent) findOwnerSpec(ctx *cu.Context, obj cu.Object) (*corev1.PodSpec, error) {
	owners, err := comp.findOwners(ctx, obj)
	if err != nil {
		return nil, err
	}
	for _, owner := range owners {
		spec := comp.findSpecFor(owner)
		if spec != nil {
			return spec, nil
		}
	}
	// This should be impossible since the top-level input is always a corev1.Pod.
	return nil, errors.Errorf("error finding pod spec for %s %s/%s", obj.GetObjectKind().GroupVersionKind(), obj.GetNamespace(), obj.GetName())
}
