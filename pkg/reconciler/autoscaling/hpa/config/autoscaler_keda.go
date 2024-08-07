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

package config

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"knative.dev/autoscaler-keda/pkg/reconciler/autoscaling/hpa/helpers"
	cm "knative.dev/pkg/configmap"
)

const (
	// AutoscalerKedaConfigName is the name of the configmap containing all
	// configuration related to Autoscaler-Keda.
	AutoscalerKedaConfigName = "config-autoscaler-keda"
	DefaultPrometheusAddress = "http://prometheus-operated.default.svc:9090"
)

// AutoscalerKedaConfig contains autoscaler keda related configuration defined in the
// `config-autoscaler-keda` config map.
type AutoscalerKedaConfig struct {
	PrometheusAddress        string
	ShouldCreateScaledObject bool
}

// NewAutoscalerKedaConfigFromConfigMap creates an AutoscalerKedaConfig from the supplied ConfigMap
func NewConfigFromMap(data map[string]string) (*AutoscalerKedaConfig, error) {
	config := &AutoscalerKedaConfig{
		PrometheusAddress:        DefaultPrometheusAddress,
		ShouldCreateScaledObject: true,
	}
	if err := cm.Parse(data,
		cm.AsString("autoscaler.keda.prometheus-address", &config.PrometheusAddress),
		cm.AsBool("autoscaler.keda.scaledobject-autocreate", &config.ShouldCreateScaledObject),
	); err != nil {
		return nil, fmt.Errorf("failed to parse data: %w", err)
	}

	if err := helpers.ParseServerAddress(config.PrometheusAddress); err != nil {
		return nil, err
	}
	return config, nil
}

// NewAutoscalerKedaConfigFromConfigMap creates an AutoscalerKedaConfig from the supplied ConfigMap
func NewAutoscalerKedaConfigFromConfigMap(configMap *corev1.ConfigMap) (*AutoscalerKedaConfig, error) {
	return NewConfigFromMap(configMap.Data)
}
