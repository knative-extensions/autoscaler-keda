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

package hpa

import (
	"context"
	"fmt"
	"strconv"

	"github.com/kedacore/keda/v2/apis/keda/v1alpha1"
	"knative.dev/serving/pkg/apis/autoscaling"

	v2 "k8s.io/api/autoscaling/v2"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	autoscalingv2listers "k8s.io/client-go/listers/autoscaling/v2"

	nv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/ptr"
	pkgreconciler "knative.dev/pkg/reconciler"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/autoscaler/config/autoscalerconfig"
	pareconciler "knative.dev/serving/pkg/client/injection/reconciler/autoscaling/v1alpha1/podautoscaler"
	areconciler "knative.dev/serving/pkg/reconciler/autoscaling"

	"knative.dev/autoscaler-keda/pkg/client/clientset/versioned"
	kedav1alpha1 "knative.dev/autoscaler-keda/pkg/client/listers/keda/v1alpha1"
	hpaconfig "knative.dev/autoscaler-keda/pkg/reconciler/autoscaling/hpa/config"
	"knative.dev/autoscaler-keda/pkg/reconciler/autoscaling/hpa/resources"
)

const (
	allActivators                     = 0
	KedaAutoscaleAnnotationAutocreate = autoscaling.GroupName + "/scaled-object-auto-create"
)

// Reconciler implements the control loop for the HPA resources.
type Reconciler struct {
	*areconciler.Base

	kubeClient kubernetes.Interface
	kedaClient versioned.Interface
	kedaLister kedav1alpha1.ScaledObjectLister
	hpaLister  autoscalingv2listers.HorizontalPodAutoscalerLister
}

// Check that our Reconciler implements pareconciler.Interface
var _ pareconciler.Interface = (*Reconciler)(nil)

// ReconcileKind implements Interface.ReconcileKind.
func (c *Reconciler) ReconcileKind(ctx context.Context, pa *autoscalingv1alpha1.PodAutoscaler) pkgreconciler.Event {
	ctx, cancel := context.WithTimeout(ctx, pkgreconciler.DefaultTimeout)
	defer cancel()

	logger := logging.FromContext(ctx)

	var scaledObj *v1alpha1.ScaledObject
	var hpa *v2.HorizontalPodAutoscaler
	shouldCreateScaledObject := true

	if hpaconfig.FromContext(ctx).AutoscalerKeda != nil {
		shouldCreateScaledObject = hpaconfig.FromContext(ctx).AutoscalerKeda.ShouldCreateScaledObject
	}

	if v, ok := pa.Annotations[KedaAutoscaleAnnotationAutocreate]; ok {
		if b, err := strconv.ParseBool(v); err == nil {
			shouldCreateScaledObject = b
		} else {
			logger.Warnf("Failed to parse annotation %q value %q as boolean: %v", KedaAutoscaleAnnotationAutocreate, v, err)
		}
	}

	if shouldCreateScaledObject {
		dScaledObject, err := resources.DesiredScaledObject(ctx, pa)
		if err != nil {
			return fmt.Errorf("failed to contruct desiredScaledObject: %w", err)
		}

		scaledObj, err = c.kedaLister.ScaledObjects(pa.Namespace).Get(dScaledObject.Name)
		if errors.IsNotFound(err) {
			logger.Infof("Creating Scaled Object %q", dScaledObject.Name)
			if scaledObj, err = c.kedaClient.KedaV1alpha1().ScaledObjects(dScaledObject.Namespace).Create(ctx, dScaledObject, metav1.CreateOptions{}); err != nil {
				pa.Status.MarkResourceFailedCreation("ScaledObject", dScaledObject.Name)
				return fmt.Errorf("failed to create ScaledObject: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("failed to get ScaledObject: %w", err)
		} else if !metav1.IsControlledBy(scaledObj, pa) {
			// Surface an error in the PodAutoscaler's status, and return an error.
			pa.Status.MarkResourceNotOwned("ScaledObject", dScaledObject.Name)
			return fmt.Errorf("PodAutoscaler: %q does not own ScaledObject: %q", pa.Name, dScaledObject.Name)
		}
		if !equality.Semantic.DeepEqual(dScaledObject.Spec, scaledObj.Spec) {
			logger.Infof("Updating ScaledObject %q", dScaledObject.Name)
			update := scaledObj.DeepCopy()
			update.Spec = dScaledObject.Spec
			if _, err := c.kedaClient.KedaV1alpha1().ScaledObjects(pa.Namespace).Update(ctx, update, metav1.UpdateOptions{}); err != nil {
				return fmt.Errorf("failed to update ScaledObject: %w", err)
			}
		}
	}
	hpa, err := c.hpaLister.HorizontalPodAutoscalers(pa.Namespace).Get(pa.Name)
	if errors.IsNotFound(err) {
		logger.Infof("Skipping HPA %q", pa.Name)
		return nil // skip wait to be triggered by hpa events eg. creation
	}

	if scaledObj != nil && scaledObj.Spec.MinReplicaCount != nil {
		if hpa.Status.DesiredReplicas < *scaledObj.Spec.MinReplicaCount {
			return nil // skip wait to be triggered by hpa events
		}
	}

	sks, err := c.ReconcileSKS(ctx, pa, nv1alpha1.SKSOperationModeServe, allActivators)
	if err != nil {
		return fmt.Errorf("error reconciling SKS: %w", err)
	}

	pa.Status.MetricsServiceName = sks.Status.PrivateServiceName

	// Propagate the service name regardless of the status.
	pa.Status.ServiceName = sks.Status.ServiceName
	if !sks.IsReady() {
		pa.Status.MarkSKSNotReady("SKS Services are not ready yet")
	} else {
		pa.Status.MarkSKSReady()
		// If a min-scale value has been set, we don't want to mark the scale target
		// as initialized until the current replicas are >= the min-scale value.
		if !pa.Status.IsScaleTargetInitialized() {
			ms := activeThreshold(ctx, pa)
			if hpa.Status.CurrentReplicas >= int32(ms) {
				pa.Status.MarkScaleTargetInitialized()
			}
		}
	}

	pa.Status.MarkActive()

	pa.Status.DesiredScale = ptr.Int32(hpa.Status.DesiredReplicas)
	pa.Status.ActualScale = ptr.Int32(hpa.Status.CurrentReplicas)
	return nil
}

// activeThreshold returns the scale required for the pa to be marked Active
func activeThreshold(ctx context.Context, pa *autoscalingv1alpha1.PodAutoscaler) int {
	asConfig := hpaconfig.FromContext(ctx).Autoscaler
	min, _ := pa.ScaleBounds(asConfig)
	if !pa.Status.IsScaleTargetInitialized() {
		initialScale := getInitialScale(asConfig, pa)
		return int(intMax(min, initialScale))
	}
	return int(intMax(min, 1))
}

// getInitialScale returns the calculated initial scale based on the autoscaler
// ConfigMap and PA initial scale annotation value.
func getInitialScale(asConfig *autoscalerconfig.Config, pa *autoscalingv1alpha1.PodAutoscaler) int32 {
	initialScale := asConfig.InitialScale
	revisionInitialScale, ok := pa.InitialScale()
	if !ok || (revisionInitialScale == 0 && !asConfig.AllowZeroInitialScale) {
		return initialScale
	}
	return revisionInitialScale
}

func intMax(a, b int32) int32 {
	if a < b {
		return b
	}
	return a
}
