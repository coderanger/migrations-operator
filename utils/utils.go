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

package utils

import (
	"context"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	migrationsv1beta1 "github.com/coderanger/migrations-operator/api/v1beta1"
)

func ListMatchingMigrators(ctx context.Context, c client.Client, pod metav1.Object) ([]*migrationsv1beta1.Migrator, error) {
	// Find any Migrator objects that match this pod.
	allMigrators := &migrationsv1beta1.MigratorList{}
	err := c.List(ctx, allMigrators, &client.ListOptions{Namespace: pod.GetNamespace()})
	if err != nil {
		return nil, errors.Wrapf(err, "error listing migrators in %s", pod.GetNamespace())
	}
	migrators := []*migrationsv1beta1.Migrator{}
	podLabels := labels.Set(pod.GetLabels())
	for _, m := range allMigrators.Items {
		selector, err := metav1.LabelSelectorAsSelector(m.Spec.Selector)
		if err != nil {
			// Ignore this migrator. Maybe print a warning in the future? This should probably be handled via a validator on Migrator.
			continue
		}
		if selector.Matches(podLabels) {
			migrator := m
			migrators = append(migrators, &migrator)
		}
	}
	return migrators, nil
}
