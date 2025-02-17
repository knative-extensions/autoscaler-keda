/*
Copyright 2022 The Knative Authors

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

package v1

import (
	http "net/http"

	rest "k8s.io/client-go/rest"
	servingv1 "knative.dev/serving/pkg/apis/serving/v1"
	scheme "knative.dev/serving/pkg/client/clientset/versioned/scheme"
)

type ServingV1Interface interface {
	RESTClient() rest.Interface
	ConfigurationsGetter
	RevisionsGetter
	RoutesGetter
	ServicesGetter
}

// ServingV1Client is used to interact with features provided by the serving.knative.dev group.
type ServingV1Client struct {
	restClient rest.Interface
}

func (c *ServingV1Client) Configurations(namespace string) ConfigurationInterface {
	return newConfigurations(c, namespace)
}

func (c *ServingV1Client) Revisions(namespace string) RevisionInterface {
	return newRevisions(c, namespace)
}

func (c *ServingV1Client) Routes(namespace string) RouteInterface {
	return newRoutes(c, namespace)
}

func (c *ServingV1Client) Services(namespace string) ServiceInterface {
	return newServices(c, namespace)
}

// NewForConfig creates a new ServingV1Client for the given config.
// NewForConfig is equivalent to NewForConfigAndClient(c, httpClient),
// where httpClient was generated with rest.HTTPClientFor(c).
func NewForConfig(c *rest.Config) (*ServingV1Client, error) {
	config := *c
	if err := setConfigDefaults(&config); err != nil {
		return nil, err
	}
	httpClient, err := rest.HTTPClientFor(&config)
	if err != nil {
		return nil, err
	}
	return NewForConfigAndClient(&config, httpClient)
}

// NewForConfigAndClient creates a new ServingV1Client for the given config and http client.
// Note the http client provided takes precedence over the configured transport values.
func NewForConfigAndClient(c *rest.Config, h *http.Client) (*ServingV1Client, error) {
	config := *c
	if err := setConfigDefaults(&config); err != nil {
		return nil, err
	}
	client, err := rest.RESTClientForConfigAndClient(&config, h)
	if err != nil {
		return nil, err
	}
	return &ServingV1Client{client}, nil
}

// NewForConfigOrDie creates a new ServingV1Client for the given config and
// panics if there is an error in the config.
func NewForConfigOrDie(c *rest.Config) *ServingV1Client {
	client, err := NewForConfig(c)
	if err != nil {
		panic(err)
	}
	return client
}

// New creates a new ServingV1Client for the given RESTClient.
func New(c rest.Interface) *ServingV1Client {
	return &ServingV1Client{c}
}

func setConfigDefaults(config *rest.Config) error {
	gv := servingv1.SchemeGroupVersion
	config.GroupVersion = &gv
	config.APIPath = "/apis"
	config.NegotiatedSerializer = rest.CodecFactoryForGeneratedClient(scheme.Scheme, scheme.Codecs).WithoutConversion()

	if config.UserAgent == "" {
		config.UserAgent = rest.DefaultKubernetesUserAgent()
	}

	return nil
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *ServingV1Client) RESTClient() rest.Interface {
	if c == nil {
		return nil
	}
	return c.restClient
}
