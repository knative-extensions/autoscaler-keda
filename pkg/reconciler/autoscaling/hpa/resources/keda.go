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
	"fmt"
	"math"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/autoscaling"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/autoscaler/config/autoscalerconfig"

	"github.com/kedacore/keda/v2/apis/keda/v1alpha1"
)

const (
	defaultPrometheusAddress = "http://prometheus-operated.default.svc:9090"

	KedaAutoscaleAnotationPrometheusAddress = autoscaling.GroupName + "/prometheus-address"
	KedaAutoscaleAnotationPrometheusQuery   = autoscaling.GroupName + "/prometheus-query"
)

// DesiredScaledObject creates an ScaledObject KEDA resource from a PA resource.
func DesiredScaledObject(pa *autoscalingv1alpha1.PodAutoscaler, config *autoscalerconfig.Config) *v1alpha1.ScaledObject {
	min, max := pa.ScaleBounds(config)
	if max == 0 {
		max = math.MaxInt32 // default to no limit
	}

	sO := v1alpha1.ScaledObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:            pa.Name,
			Namespace:       pa.Namespace,
			Labels:          pa.Labels,
			Annotations:     pa.Annotations,
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(pa)},
		},
		Spec: v1alpha1.ScaledObjectSpec{
			ScaleTargetRef: &v1alpha1.ScaleTarget{
				Name: pa.Name + "-deployment",
			},
			Advanced: &v1alpha1.AdvancedConfig{
				HorizontalPodAutoscalerConfig: &v1alpha1.HorizontalPodAutoscalerConfig{
					Name: pa.Name,
				},
			},
			MaxReplicaCount: ptr.Int32(max),
		},
	}

	if min > 0 {
		sO.Spec.MinReplicaCount = ptr.Int32(min)
	}

	if target, ok := pa.Target(); ok {
		switch pa.Metric() {
		case autoscaling.CPU:
			sO.Spec.Triggers = []v1alpha1.ScaleTriggers{
				{
					Type:       "cpu",
					MetricType: autoscalingv2.UtilizationMetricType,
					Metadata:   map[string]string{"value": fmt.Sprint(int32(math.Ceil(target)))},
				},
			}
			if min <= 0 {
				sO.Spec.MinReplicaCount = ptr.Int32(1)
			}
		case autoscaling.Memory:
			memory := resource.NewQuantity(int64(target)*1024*1024, resource.BinarySI)
			sO.Spec.Triggers = []v1alpha1.ScaleTriggers{
				{
					Type:       "memory",
					MetricType: autoscalingv2.AverageValueMetricType,
					Metadata:   map[string]string{"value": memory.String()},
				},
			}
			if min <= 0 {
				sO.Spec.MinReplicaCount = ptr.Int32(1)
			}
		default:
			if target, ok := pa.Target(); ok {
				targetQuantity := resource.NewQuantity(int64(target), resource.DecimalSI)
				var query, address string
				if v, ok := pa.Annotations[KedaAutoscaleAnotationPrometheusQuery]; ok {
					query = v
				} else {
					query = fmt.Sprintf("sum(rate(%s{}[1m]))", pa.Metric())
				}
				if v, ok := pa.Annotations[KedaAutoscaleAnotationPrometheusAddress]; ok {
					address = v
				} else {
					address = defaultPrometheusAddress
				}
				sO.Spec.Triggers = []v1alpha1.ScaleTriggers{
					{
						Type: "prometheus",
						Metadata: map[string]string{
							"serverAddress": address,
							"query":         query,
							"threshold":     targetQuantity.String(),
						},
					},
				}
			}
		}
	}

	if window, hasWindow := pa.Window(); hasWindow {
		windowSeconds := int32(window.Seconds())
		sO.Spec.Advanced.HorizontalPodAutoscalerConfig.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
			ScaleDown: &autoscalingv2.HPAScalingRules{
				StabilizationWindowSeconds: &windowSeconds,
			},
			ScaleUp: &autoscalingv2.HPAScalingRules{
				StabilizationWindowSeconds: &windowSeconds,
			},
		}
	}

	return &sO
}
