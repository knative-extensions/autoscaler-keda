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

package resources

import (
	kedav1alpha1 "github.com/kedacore/keda/v2/apis/keda/v1alpha1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/ptr"
)

type ScaledObjectOption func(object *kedav1alpha1.ScaledObject)

func ScaledObject(namespace, name string, options ...ScaledObjectOption) *kedav1alpha1.ScaledObject {
	sObj := &kedav1alpha1.ScaledObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: kedav1alpha1.ScaledObjectSpec{
			ScaleTargetRef: &kedav1alpha1.ScaleTarget{Name: name + "-deployment"},
			Advanced: &kedav1alpha1.AdvancedConfig{
				HorizontalPodAutoscalerConfig: &kedav1alpha1.HorizontalPodAutoscalerConfig{}},
		},
	}

	for _, opt := range options {
		opt(sObj)
	}
	return sObj
}

func WithAnnotations(annotations map[string]string) ScaledObjectOption {
	return func(scaledObject *kedav1alpha1.ScaledObject) {
		if scaledObject.Annotations == nil {
			scaledObject.Annotations = make(map[string]string, 1)
		}
		for k, v := range annotations {
			scaledObject.Annotations[k] = v
		}
	}
}

func WithMinScale(min int32) ScaledObjectOption {
	return func(scaledObject *kedav1alpha1.ScaledObject) {
		scaledObject.Spec.MinReplicaCount = ptr.Int32(min)
	}
}

func WithMaxScale(max int32) ScaledObjectOption {
	return func(scaledObject *kedav1alpha1.ScaledObject) {
		scaledObject.Spec.MaxReplicaCount = ptr.Int32(max)
	}
}

func WithScaleTargetRef(name string) ScaledObjectOption {
	return func(scaledObject *kedav1alpha1.ScaledObject) {
		if scaledObject.Spec.ScaleTargetRef == nil {
			scaledObject.Spec.ScaleTargetRef = &kedav1alpha1.ScaleTarget{
				Name: name,
			}
		} else {
			scaledObject.Spec.ScaleTargetRef.Name = name
		}
	}
}

func WithScalingModifiers(sm kedav1alpha1.ScalingModifiers) ScaledObjectOption {
	return func(scaledObject *kedav1alpha1.ScaledObject) {
		if scaledObject.Spec.Advanced == nil {
			scaledObject.Spec.Advanced = &kedav1alpha1.AdvancedConfig{
				ScalingModifiers: sm,
			}
		} else {
			scaledObject.Spec.Advanced.ScalingModifiers = sm
		}
	}
}

func WithPrometheusTrigger(metadata map[string]string) ScaledObjectOption {
	return func(scaledObject *kedav1alpha1.ScaledObject) {
		scaledObject.Spec.Triggers = append(scaledObject.Spec.Triggers, kedav1alpha1.ScaleTriggers{
			Type:     "prometheus",
			Metadata: metadata,
		})
	}
}

func WithCpuTrigger(metadata map[string]string) ScaledObjectOption {
	return func(scaledObject *kedav1alpha1.ScaledObject) {
		scaledObject.Spec.Triggers = append(scaledObject.Spec.Triggers, kedav1alpha1.ScaleTriggers{
			Type:       "cpu",
			Metadata:   metadata,
			MetricType: "Utilization",
		})
	}
}

func WithAuthTrigger(triggerType string, metricType autoscalingv2.MetricTargetType, metadata map[string]string, name, kind string) ScaledObjectOption {
	return func(scaledObject *kedav1alpha1.ScaledObject) {
		scaledObject.Spec.Triggers = []kedav1alpha1.ScaleTriggers{
			{
				Type:       triggerType,
				MetricType: metricType,
				Metadata:   metadata,
				AuthenticationRef: &kedav1alpha1.AuthenticationRef{
					Name: name,
					Kind: kind,
				},
			}}
	}
}

func WithHorizontalPodAutoscalerConfig(name string) ScaledObjectOption {
	return func(scaledObject *kedav1alpha1.ScaledObject) {
		if scaledObject.Spec.Advanced == nil {
			scaledObject.Spec.Advanced = &kedav1alpha1.AdvancedConfig{
				HorizontalPodAutoscalerConfig: &kedav1alpha1.HorizontalPodAutoscalerConfig{
					Name: name,
				}}
		} else {
			scaledObject.Spec.Advanced.HorizontalPodAutoscalerConfig = &kedav1alpha1.HorizontalPodAutoscalerConfig{
				Name: name,
			}
		}
	}
}

func WithAuthenticationRef(name, kind string) ScaledObjectOption {
	return func(scaledObject *kedav1alpha1.ScaledObject) {
		for _, t := range scaledObject.Spec.Triggers {
			t.AuthenticationRef = &kedav1alpha1.AuthenticationRef{
				Name: name,
				Kind: kind,
			}
		}
	}
}

func WithTrigger(triggerType string, metricType autoscalingv2.MetricTargetType, metadata map[string]string) ScaledObjectOption {
	return func(scaledObject *kedav1alpha1.ScaledObject) {
		scaledObject.Spec.Triggers = []kedav1alpha1.ScaleTriggers{
			{
				Type:       triggerType,
				Metadata:   metadata,
				MetricType: metricType,
			}}
	}
}
