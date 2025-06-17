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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"text/template"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kedacore/keda/v2/apis/keda/v1alpha1"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/autoscaling"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"

	hpaconfig "knative.dev/autoscaler-keda/pkg/reconciler/autoscaling/hpa/config"
	"knative.dev/autoscaler-keda/pkg/reconciler/autoscaling/hpa/helpers"
)

const (
	KedaAutoscaleAnnotationPrometheusAddress       = autoscaling.GroupName + "/prometheus-address"
	KedaAutoscaleAnnotationPrometheusQuery         = autoscaling.GroupName + "/prometheus-query"
	KedaAutoscaleAnnotationMetricType              = autoscaling.GroupName + "/metric-type"
	KedaAutoscaleAnnotationPrometheusAuthName      = autoscaling.GroupName + "/trigger-prometheus-auth-name"
	KedaAutoscaleAnnotationPrometheusAuthKind      = autoscaling.GroupName + "/trigger-prometheus-auth-kind"
	KedaAutoscaleAnnotationPrometheusAuthModes     = autoscaling.GroupName + "/trigger-prometheus-auth-modes"
	KedaAutoscalerAnnnotationPrometheusName        = autoscaling.GroupName + "/trigger-prometheus-name"
	KedaAutoscaleAnnotationExtraPrometheusTriggers = autoscaling.GroupName + "/extra-prometheus-triggers"
	KedaAutoscaleAnnotationScalingModifiers        = autoscaling.GroupName + "/scaling-modifiers"
	KedaAutoscalingAnnotationHPAScaleUpRules       = autoscaling.GroupName + "/hpa-scale-up-rules"
	KedaAutoscalingAnnotationHPAScaleDownRules     = autoscaling.GroupName + "/hpa-scale-down-rules"
	KedaAutoscaleAnnotationsScaledObjectOverride   = autoscaling.GroupName + "/scaled-object-override"

	defaultCPUTarget = 70
)

// DesiredScaledObject creates an ScaledObject KEDA resource from a PA resource.
func DesiredScaledObject(ctx context.Context, pa *autoscalingv1alpha1.PodAutoscaler) (*v1alpha1.ScaledObject, error) {

	config := hpaconfig.FromContext(ctx).Autoscaler
	autoscalerkedaconfig := hpaconfig.FromContext(ctx).AutoscalerKeda

	minScale, maxScale := pa.ScaleBounds(config)
	if maxScale == 0 {
		maxScale = math.MaxInt32 // default to no limit
	}

	var sO v1alpha1.ScaledObject
	if v, ok := pa.Annotations[KedaAutoscaleAnnotationsScaledObjectOverride]; ok {
		if err := json.Unmarshal([]byte(v), &sO); err != nil {
			return nil, fmt.Errorf("unable to unmarshal scaled object override: %w", err)
		}
		setScaledObjectDefaults(&sO, maxScale, pa)
		return &sO, nil
	}

	sO = v1alpha1.ScaledObject{}
	setScaledObjectDefaults(&sO, maxScale, pa)

	if v, ok := pa.Annotations[KedaAutoscaleAnnotationScalingModifiers]; ok {
		scalingModifiers := v1alpha1.ScalingModifiers{}
		if err := json.Unmarshal([]byte(v), &scalingModifiers); err != nil {
			return nil, fmt.Errorf("unable to unmarshal scaling modifiers: %w", err)
		}
		sO.Spec.Advanced.ScalingModifiers = scalingModifiers
		log.Printf("scaling modifiers: %v\n", scalingModifiers)
	}

	if minScale > 0 {
		sO.Spec.MinReplicaCount = ptr.Int32(minScale)
	}

	if target, ok := resolveTarget(pa); ok {
		mt, err := getMetricType(pa.Annotations, pa.Metric())
		if err != nil {
			return nil, err
		}
		switch pa.Metric() {
		case autoscaling.CPU:
			sO.Spec.Triggers = []v1alpha1.ScaleTriggers{
				{
					Name:       "default-trigger-cpu",
					Type:       "cpu",
					MetricType: *mt,
					Metadata:   map[string]string{"value": fmt.Sprint(int32(math.Ceil(target)))},
				},
			}
			if minScale <= 0 {
				sO.Spec.MinReplicaCount = ptr.Int32(1)
			}
		case autoscaling.Memory:
			memory := resource.NewQuantity(int64(target)*1024*1024, resource.BinarySI)
			sO.Spec.Triggers = []v1alpha1.ScaleTriggers{
				{
					Name:       "default-trigger-memory",
					Type:       "memory",
					MetricType: *mt,
					Metadata:   map[string]string{"value": memory.String()},
				},
			}
			if minScale <= 0 {
				sO.Spec.MinReplicaCount = ptr.Int32(1)
			}
		default:
			targetQuantity := resource.NewQuantity(int64(target), resource.DecimalSI)
			var query, address string
			if query, ok = pa.Annotations[KedaAutoscaleAnnotationPrometheusQuery]; !ok {
				return nil, fmt.Errorf("query is missing for custom metric: %w", err)
			}

			tmpl, err := template.New("query").Parse(query)
			if err != nil {
				return nil, fmt.Errorf("template initialization failed: %w", err)
			}
			values := map[string]string{
				"revisionName": pa.Name,
			}
			var output bytes.Buffer
			if err := tmpl.Execute(&output, values); err != nil {
				return nil, fmt.Errorf("template execution failed: %w", err)
			}
			query = output.String()

			if v, ok := pa.Annotations[KedaAutoscaleAnnotationPrometheusAddress]; ok {
				if err := helpers.ParseServerAddress(v); err != nil {
					return nil, fmt.Errorf("invalid prometheus address: %w", err)
				}
				address = v
			} else {
				address = autoscalerkedaconfig.PrometheusAddress
			}
			defaultTrigger, err := getDefaultPrometheusTrigger(pa.Annotations, address, query, targetQuantity.String(), pa.Namespace, *mt)
			if err != nil {
				return nil, err
			}
			sO.Spec.Triggers = []v1alpha1.ScaleTriggers{*defaultTrigger}
		}
	}

	extraPrometheusTriggers, err := getExtraPrometheusTriggers(pa.Annotations)
	if err != nil {
		return nil, err
	}

	sO.Spec.Triggers = append(sO.Spec.Triggers, extraPrometheusTriggers...)

	if len(sO.Spec.Triggers) == 0 {
		return nil, fmt.Errorf("no triggers were specified, make sure a metric target is specified or extra triggers are added")
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

	if v, ok := pa.Annotations[KedaAutoscalingAnnotationHPAScaleUpRules]; ok {
		var scaleUpRules autoscalingv2.HPAScalingRules
		if err := json.Unmarshal([]byte(v), &scaleUpRules); err != nil {
			return nil, fmt.Errorf("unable to unmarshal scale up rules: %w", err)
		}
		if sO.Spec.Advanced.HorizontalPodAutoscalerConfig.Behavior == nil {
			sO.Spec.Advanced.HorizontalPodAutoscalerConfig.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{}
		}
		sO.Spec.Advanced.HorizontalPodAutoscalerConfig.Behavior.ScaleUp = &scaleUpRules
	}

	if v, ok := pa.Annotations[KedaAutoscalingAnnotationHPAScaleDownRules]; ok {
		var scaleDownRules autoscalingv2.HPAScalingRules
		if err := json.Unmarshal([]byte(v), &scaleDownRules); err != nil {
			return nil, fmt.Errorf("unable to unmarshal scale down rules: %w", err)
		}
		if sO.Spec.Advanced.HorizontalPodAutoscalerConfig.Behavior == nil {
			sO.Spec.Advanced.HorizontalPodAutoscalerConfig.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{}
		}
		sO.Spec.Advanced.HorizontalPodAutoscalerConfig.Behavior.ScaleDown = &scaleDownRules
	}

	return &sO, nil
}

func resolveTarget(pa *autoscalingv1alpha1.PodAutoscaler) (float64, bool) {
	if target, ok := pa.Target(); ok {
		return target, true
	}
	// When user has not specified a target value, we default to 70% for CPU
	// This improves the UX as Serving defaults to CPU if no metric is specified via an annotation.
	if pa.Metric() == autoscaling.CPU {
		return defaultCPUTarget, true
	}
	return 0, false
}

func getDefaultPrometheusTrigger(annotations map[string]string, address string, query string, threshold string, ns string, targetType autoscalingv2.MetricTargetType) (*v1alpha1.ScaleTriggers, error) {
	var name string

	if v, ok := annotations[KedaAutoscalerAnnnotationPrometheusName]; ok {
		name = v
	} else {
		name = "default-trigger-custom"
	}
	trigger := v1alpha1.ScaleTriggers{
		Type:       "prometheus",
		Name:       name,
		MetricType: targetType,
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

func getExtraPrometheusTriggers(annotations map[string]string) ([]v1alpha1.ScaleTriggers, error) {
	triggers := []v1alpha1.ScaleTriggers(nil)
	if v, ok := annotations[KedaAutoscaleAnnotationExtraPrometheusTriggers]; ok {
		if err := json.Unmarshal([]byte(v), &triggers); err != nil {
			return nil, fmt.Errorf("unable to unmarshal extra prometheus triggers: %w", err)
		}
	}
	return triggers, nil
}

func getMetricType(annotations map[string]string, metric string) (*autoscalingv2.MetricTargetType, error) {
	var mt *autoscalingv2.MetricTargetType
	v, ok := annotations[KedaAutoscaleAnnotationMetricType]
	if ok {
		if v != string(autoscalingv2.AverageValueMetricType) && v != string(autoscalingv2.UtilizationMetricType) && v != string(autoscalingv2.ValueMetricType) {
			return nil, fmt.Errorf("invalid metric type: %s", v)
		}
	}
	switch metric {
	case autoscaling.CPU, autoscaling.Memory:
		var err error
		mt, err = getCPUOrMemoryMetricType(metric, v)
		if err != nil {
			return nil, err
		}
	default:
		dMetricType := getDefaultMetricType(v)
		mt = &dMetricType
	}
	return mt, nil
}

func getDefaultMetricType(metricType string) autoscalingv2.MetricTargetType {
	v := autoscalingv2.AverageValueMetricType
	if metricType != "" {
		v = autoscalingv2.MetricTargetType(metricType)
	}
	return v
}

func getCPUOrMemoryMetricType(metric string, metricType string) (*autoscalingv2.MetricTargetType, error) {
	var v autoscalingv2.MetricTargetType
	if metricType == string(autoscalingv2.ValueMetricType) {
		return nil, fmt.Errorf("invalid metric type: %s", v)
	}
	if metricType != "" {
		v = autoscalingv2.MetricTargetType(metricType)
		return &v, nil
	}
	if metric == autoscaling.CPU {
		v = autoscalingv2.UtilizationMetricType
	} else {
		v = autoscalingv2.AverageValueMetricType
	}
	return &v, nil
}

func setScaledObjectDefaults(sO *v1alpha1.ScaledObject, maxScale int32, pa *autoscalingv1alpha1.PodAutoscaler) {
	sO.SetName(pa.Name)
	sO.SetNamespace(pa.Namespace)
	sO.SetOwnerReferences([]metav1.OwnerReference{*kmeta.NewControllerRef(pa)})
	sO.Annotations = pa.Annotations
	sO.Labels = pa.Labels
	if sO.Spec.ScaleTargetRef == nil {
		sO.Spec.ScaleTargetRef = &v1alpha1.ScaleTarget{}
	}
	sO.Spec.ScaleTargetRef.Name = pa.Spec.ScaleTargetRef.Name
	sO.Spec.ScaleTargetRef.Kind = pa.Spec.ScaleTargetRef.Kind
	sO.Spec.ScaleTargetRef.APIVersion = pa.Spec.ScaleTargetRef.APIVersion
	if sO.Spec.Advanced == nil {
		sO.Spec.Advanced = &v1alpha1.AdvancedConfig{}
		sO.Spec.Advanced.HorizontalPodAutoscalerConfig = &v1alpha1.HorizontalPodAutoscalerConfig{}
	}
	sO.Spec.Advanced.HorizontalPodAutoscalerConfig.Name = pa.Name
	sO.Spec.MaxReplicaCount = ptr.Int32(maxScale)
}
