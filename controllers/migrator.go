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
	cu "github.com/coderanger/controller-utils"
	"github.com/coderanger/migrations-operator/components"
	ctrl "sigs.k8s.io/controller-runtime"

	migrationsv1beta1 "github.com/coderanger/migrations-operator/api/v1beta1"
)

// +kubebuilder:rbac:groups=migrations.coderanger.net,resources=migrators,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=migrations.coderanger.net,resources=migrators/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=apps,resources=replicasets;deployments;statfulsets;daemonsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch
// +kubebuilder:rbac:groups=argoproj.io,resources=rollouts,verbs=get;list;watch

func Migrator(mgr ctrl.Manager) error {
	return cu.NewReconciler(mgr).
		For(&migrationsv1beta1.Migrator{}).
		Component("user", components.Migrations()).
		ReadyStatusComponent("MigrationsReady").
		// Webhook().
		Complete()
}
