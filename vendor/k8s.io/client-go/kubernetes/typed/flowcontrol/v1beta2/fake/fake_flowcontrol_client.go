/*
Copyright The Kubernetes Authors.

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

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	v1beta2 "k8s.io/client-go/kubernetes/typed/flowcontrol/v1beta2"
	rest "k8s.io/client-go/rest"
	testing "k8s.io/client-go/testing"
)

type FakeFlowcontrolV1beta2 struct {
	*testing.Fake
}

func (c *FakeFlowcontrolV1beta2) FlowSchemas() v1beta2.FlowSchemaInterface {
	return newFakeFlowSchemas(c)
}

func (c *FakeFlowcontrolV1beta2) PriorityLevelConfigurations() v1beta2.PriorityLevelConfigurationInterface {
	return newFakePriorityLevelConfigurations(c)
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *FakeFlowcontrolV1beta2) RESTClient() rest.Interface {
	var ret *rest.RESTClient
	return ret
}
