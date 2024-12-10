/*
Copyright 2024 The Knative Authors

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

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1serving "knative.dev/serving/pkg/apis/serving/v1"
	serviceresourcenames "knative.dev/serving/pkg/reconciler/service/resources/names"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

func CreateServiceReady(t testing.TB, clients *test.Clients, names *test.ResourceNames, fopt ...rtesting.ServiceOption) (*v1test.ResourceObjects, error) {
	if names.Image == "" {
		return nil, fmt.Errorf("expected non-empty Image name; got Image=%v", names.Image)
	}
	svc, err := v1test.CreateService(t, clients, *names, fopt...)
	if err != nil {
		return nil, err
	}
	return getResourceObjects(t, clients, names, svc)
}

func getResourceObjects(t testing.TB, clients *test.Clients, names *test.ResourceNames, svc *v1serving.Service) (*v1test.ResourceObjects, error) {
	// Populate Route and Configuration Objects with name
	names.Route = serviceresourcenames.Route(svc)
	names.Config = serviceresourcenames.Configuration(svc)
	names.Revision = svc.GetName() + "-00001"

	// Might have been overridden by ServiceOptions
	names.Service = svc.Name

	// Wait before getting the objects
	time.Sleep(30 * time.Second)

	t.Log("Getting latest objects Created by Service")
	resources, err := GetResourceObjects(clients, *names)
	if err == nil {
		t.Log("Successfully created Service", names.Service)
	}
	return resources, err
}

// GetResourceObjects obtains the services resources from the k8s API server.
func GetResourceObjects(clients *test.Clients, names test.ResourceNames) (*v1test.ResourceObjects, error) {
	routeObject, err := clients.ServingClient.Routes.Get(context.Background(), names.Route, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	serviceObject, err := clients.ServingClient.Services.Get(context.Background(), names.Service, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	configObject, err := clients.ServingClient.Configs.Get(context.Background(), names.Config, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	revisionObject, err := clients.ServingClient.Revisions.Get(context.Background(), names.Revision, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return &v1test.ResourceObjects{
		Route:    routeObject,
		Service:  serviceObject,
		Config:   configObject,
		Revision: revisionObject,
	}, nil
}

func getResourceObjectsRevision2(t testing.TB, clients *test.Clients, names *test.ResourceNames, svc *v1serving.Service) (*v1test.ResourceObjects, error) {
	// Populate Route and Configuration Objects with name
	names.Route = serviceresourcenames.Route(svc)
	names.Config = serviceresourcenames.Configuration(svc)
	names.Revision = svc.GetName() + "-00002"

	// Might have been overridden by ServiceOptions
	names.Service = svc.Name

	// Wait before getting the objects
	time.Sleep(30 * time.Second)

	t.Log("Getting latest objects Created by Service")
	resources, err := GetResourceObjects(clients, *names)
	if err == nil {
		t.Log("Successfully created Service", names.Service)
	}
	return resources, err
}
