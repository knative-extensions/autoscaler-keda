/*
Copyright 2024 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	configmaptesting "knative.dev/pkg/configmap/testing"
	logtesting "knative.dev/pkg/logging/testing"
	autoscalerconfig "knative.dev/serving/pkg/autoscaler/config"
)

func TestStoreLoadWithContext(t *testing.T) {
	store := NewStore(logtesting.TestLogger(t))

	autoscalerKedaManagerConfig := configmaptesting.ConfigMapFromTestFile(t, AutoscalerKedaConfigName)
	autoscalerConfig := configmaptesting.ConfigMapFromTestFile(t, autoscalerconfig.ConfigName)
	store.OnConfigChanged(autoscalerKedaManagerConfig)
	store.OnConfigChanged(autoscalerConfig)
	config := FromContext(store.ToContext(context.Background()))

	wantAS, _ := autoscalerconfig.NewConfigFromConfigMap(autoscalerConfig)
	if !cmp.Equal(wantAS, config.Autoscaler) {
		t.Error("Autoscaler ConfigMap mismatch (-want, +got):", cmp.Diff(wantAS, config.Autoscaler))
	}

	wantASK, _ := NewAutoscalerKedaConfigFromConfigMap(autoscalerKedaManagerConfig)
	if diff := cmp.Diff(wantASK, config.AutoscalerKeda); diff != "" {
		t.Errorf("Unexpected AutoscalerKeda config (-want, +got): %v", diff)
	}
}

func TestStoreImmutableConfig(t *testing.T) {
	store := NewStore(logtesting.TestLogger(t))

	autoscalerConfig := configmaptesting.ConfigMapFromTestFile(t, autoscalerconfig.ConfigName)
	autoscalerKedaManagerConfig := configmaptesting.ConfigMapFromTestFile(t, AutoscalerKedaConfigName)
	store.OnConfigChanged(autoscalerConfig)
	store.OnConfigChanged(autoscalerKedaManagerConfig)
	config := store.Load()

	config.AutoscalerKeda.PrometheusAddress = ":9090"

	newConfig := store.Load()
	if newConfig.AutoscalerKeda.PrometheusAddress == ":9090" {
		t.Error("AutoscalerKeda config is not immutable")
	}
}
