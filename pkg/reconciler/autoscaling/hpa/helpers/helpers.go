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

package helpers

import (
	"net/url"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/networking/pkg/apis/networking"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	pkgtesting "knative.dev/serving/pkg/testing" //nolint:all
)

const (
	TestNamespace = "test-namespace"
	TestRevision  = "test-revision"
)

func Pa(namespace, name string, options ...pkgtesting.PodAutoscalerOption) *autoscalingv1alpha1.PodAutoscaler {
	pa := &autoscalingv1alpha1.PodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: autoscalingv1alpha1.PodAutoscalerSpec{
			ScaleTargetRef: corev1.ObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       name + "-deployment",
			},
			ProtocolType: networking.ProtocolHTTP1,
		},
	}
	pa.Status.InitializeConditions()
	for _, opt := range options {
		opt(pa)
	}
	return pa
}

func WithAnnotations(annos map[string]string) pkgtesting.PodAutoscalerOption {
	return func(pa *autoscalingv1alpha1.PodAutoscaler) {
		if pa.Annotations == nil {
			pa.Annotations = make(map[string]string, 1)
		}
		for k, v := range annos {
			pa.Annotations[k] = v
		}
	}
}

func ParseServerAddress(address string) error {
	if _, err := url.ParseRequestURI(address); err != nil {
		return err
	}
	u, err := url.Parse(address)
	if err != nil || u.Host == "" {
		return err
	}
	return nil
}
