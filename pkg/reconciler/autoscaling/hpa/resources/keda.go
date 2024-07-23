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

	hpaconfig "knative.dev/autoscaler-keda/pkg/reconciler/autoscaling/hpa/config"
	"knative.dev/autoscaler-keda/pkg/reconciler/autoscaling/hpa/helpers"
)

const (
	KedaAutoscaleAnnotationPrometheusAddress   = autoscaling.GroupName + "/prometheus-address"
	KedaAutoscaleAnnotationPrometheusQuery     = autoscaling.GroupName + "/prometheus-query"
	KedaAutoscaleAnnotationPrometheusAuthName  = autoscaling.GroupName + "/trigger-prometheus-auth-name"
	KedaAutoscaleAnnotationPrometheusAuthKind  = autoscaling.GroupName + "/trigger-prometheus-auth-kind"
	KedaAutoscaleAnnotationPrometheusAuthModes = autoscaling.GroupName + "/trigger-prometheus-auth-modes"
)

// DesiredScaledObject creates an ScaledObject KEDA resource from a PA resource.
func DesiredScaledObject(pa *autoscalingv1alpha1.PodAutoscaler, config *autoscalerconfig.Config, autoscalerkedaconfig *hpaconfig.AutoscalerKedaConfig) (*v1alpha1.ScaledObject, error) {
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
				if v, ok := pa.Annotations[KedaAutoscaleAnnotationPrometheusQuery]; ok {
					query = v
				} else {
					query = fmt.Sprintf("sum(rate(%s{}[1m]))", pa.Metric())
				}
				if v, ok := pa.Annotations[KedaAutoscaleAnnotationPrometheusAddress]; ok {
					if err := helpers.ParseServerAddress(v); err != nil {
						return nil, fmt.Errorf("invalid prometheus address: %w", err)
					}
					address = v
				} else {
					address = autoscalerkedaconfig.PrometheusAddress
				}

				defaultTrigger, err := getDefaultPrometheusTrigger(pa.Annotations, address, query, targetQuantity.String(), pa.Namespace)
				if err != nil {
					return nil, err
				}
				sO.Spec.Triggers = []v1alpha1.ScaleTriggers{
					*defaultTrigger,
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

	return &sO, nil
}

func getDefaultPrometheusTrigger(annotations map[string]string, address string, query string, threshold string, ns string) (*v1alpha1.ScaleTriggers, error) {
	trigger := v1alpha1.ScaleTriggers{
		Type: "prometheus",
		Metadata: map[string]string{
			"serverAddress": address,
			"query":         query,
			"threshold":     threshold,
			"namespace":     ns,
		}}

	var ref *v1alpha1.AuthenticationRef

	if v, ok := annotations[KedaAutoscaleAnnotationPrometheusAuthName]; ok {
		ref = &v1alpha1.AuthenticationRef{}
		ref.Name = v
	}

	if v, ok := annotations[KedaAutoscaleAnnotationPrometheusAuthKind]; ok {
		if ref == nil {
			return nil, fmt.Errorf("you need to specify the name as well for authentication")
		}
		if v != "TriggerAuthentication" && v != "ClusterTriggerAuthentication" {
			return nil, fmt.Errorf("invalid auth kind: %s", v)
		}
		ref.Kind = v
	}

	if v, ok := annotations[KedaAutoscaleAnnotationPrometheusAuthModes]; ok {
		if ref == nil {
			return nil, fmt.Errorf("you need to specify the name as well for authentication")
		}
		trigger.Metadata["authModes"] = v
	} else if ref != nil {
		return nil, fmt.Errorf("you need to specify the authModes for authentication")
	}

	trigger.AuthenticationRef = ref
	return &trigger, nil
}
