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

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	mohttp "github.com/coderanger/migrations-operator/http"
)

func main() {
	if len(os.Args) != 5 {
		log.Fatal("Incorrect number of arguments")
	}
	targetImage := os.Args[1]
	migratorNamespace := os.Args[2]
	migratorName := os.Args[3]
	apiUrl := fmt.Sprintf("http://%s/api/ready", os.Args[4])

	log.Printf("Polling for migrator %s at image %s", migratorName, targetImage)
	for {
		ready, err := migratorReady(targetImage, migratorNamespace, migratorName, apiUrl)
		if err != nil {
			log.Fatalf("Error while polling: %v", err)
		}
		if ready {
			break
		}
		time.Sleep(5 * time.Second)
	}
	log.Printf("Migrations ready, exiting")
}

func migratorReady(targetImage, migratorNamespace, migratorName, apiUrl string) (bool, error) {
	args := &mohttp.ReadyArgs{TargetImage: targetImage, MigratorNamespace: migratorNamespace, MigratorName: migratorName}
	data, err := json.Marshal(args)
	if err != nil {
		return false, err
	}
	resp, err := http.Post(apiUrl, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	ready := string(body) == "true"
	return ready, nil
}
