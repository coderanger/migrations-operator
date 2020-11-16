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
	"encoding/json"
	"fmt"
	"net/http"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	migrationsv1beta1 "github.com/coderanger/migrations-operator/api/v1beta1"
)

type readyHandler struct {
	client client.Client
}

type ReadyArgs struct {
	TargetImage       string `json:"targetImage"`
	MigratorNamespace string `json:"migratorNamespace"`
	MigratorName      string `json:"migratorName"`
}

func (h *readyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ready, err := h.handle(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
	} else {
		fmt.Fprintf(w, "%v", ready)
	}
}

func (h *readyHandler) handle(w http.ResponseWriter, r *http.Request) (bool, error) {
	// Parse args.
	var args ReadyArgs
	err := json.NewDecoder(r.Body).Decode(&args)
	if err != nil {
		return false, err
	}

	// Try to find the migrator object.
	migrator := &migrationsv1beta1.Migrator{}
	err = h.client.Get(r.Context(), types.NamespacedName{Name: args.MigratorName, Namespace: args.MigratorNamespace}, migrator)
	if err != nil {
		if kerrors.IsNotFound(err) {
			return false, nil
		} else {
			return false, err
		}
	}

	// Check if the version matches.
	return migrator.Status.LastSuccessfulMigration == args.TargetImage, nil
}
