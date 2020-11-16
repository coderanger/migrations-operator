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
	"context"
	"net/http"
	"os"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var log = ctrl.Log.WithName("api")

type apiServer struct {
	client client.Client
}

func APIServer(mgr ctrl.Manager) error {
	server := &apiServer{client: mgr.GetClient()}
	return mgr.Add(server)
}

func (s *apiServer) Start(stop <-chan struct{}) error {
	mux := http.NewServeMux()
	mux.Handle("/api/ready", &readyHandler{client: s.client})

	addr := os.Getenv("API_LISTEN")
	if addr == "" {
		addr = ":5000"
	}

	log.Info("serving API server", "addr", addr)

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	done := make(chan struct{})
	go func() {
		<-stop
		log.Info("shutting down API server")

		// TODO: use a context with reasonable timeout
		if err := srv.Shutdown(context.Background()); err != nil {
			// Error from closing listeners, or context timeout
			log.Error(err, "error shutting down the HTTP server")
		}
		close(done)
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}

	<-done
	return nil
}

func (_ *apiServer) NeedLeaderElection() bool {
	return false
}
