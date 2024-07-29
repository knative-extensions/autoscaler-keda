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
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ktesting "k8s.io/client-go/testing"

	fakekedaclient "knative.dev/autoscaler-keda/pkg/client/injection/client/fake"
	_ "knative.dev/autoscaler-keda/pkg/client/injection/informers/keda/v1alpha1/scaledobject"
	kedaresources "knative.dev/autoscaler-keda/pkg/reconciler/autoscaling/hpa/resources"
	nv1a1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	networkingclient "knative.dev/networking/pkg/client/injection/client"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/reconciler"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/apis/autoscaling"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	autoscalerconfig "knative.dev/serving/pkg/autoscaler/config"
	servingclient "knative.dev/serving/pkg/client/injection/client"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	"knative.dev/serving/pkg/client/injection/ducks/autoscaling/v1alpha1/podscalable"
	fakepainformer "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/podautoscaler/fake"
	pareconciler "knative.dev/serving/pkg/client/injection/reconciler/autoscaling/v1alpha1/podautoscaler"
	areconciler "knative.dev/serving/pkg/reconciler/autoscaling"
	"knative.dev/serving/pkg/reconciler/autoscaling/hpa/resources"
	aresources "knative.dev/serving/pkg/reconciler/autoscaling/resources"
	"knative.dev/serving/pkg/reconciler/serverlessservice/resources/names"

	_ "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/serverlessservice/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/autoscaling/v2/horizontalpodautoscaler/fake"
	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/service/fake"
	_ "knative.dev/pkg/metrics/testing"
	_ "knative.dev/serving/pkg/client/injection/ducks/autoscaling/v1alpha1/podscalable/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/autoscaling/v1alpha1/metric/fake"

	_ "knative.dev/autoscaler-keda/pkg/client/injection/informers/keda/v1alpha1/scaledobject/fake"

	reconcilertesting "knative.dev/pkg/reconciler/testing"
	testingv1 "knative.dev/serving/pkg/reconciler/testing/v1"
	. "knative.dev/serving/pkg/testing" //nolint:all

	kedav1alpha1 "github.com/kedacore/keda/v2/apis/keda/v1alpha1"
	hpaconfig "knative.dev/autoscaler-keda/pkg/reconciler/autoscaling/hpa/config"
	helpers "knative.dev/autoscaler-keda/pkg/reconciler/autoscaling/hpa/helpers"
)

func TestControllerCanReconcile(t *testing.T) {
	ctx, cancel, infs := reconcilertesting.SetupFakeContextWithCancel(t)
	ctl := NewController(ctx, configmap.NewStaticWatcher(
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      autoscalerconfig.ConfigName,
			},
			Data: map[string]string{},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      hpaconfig.AutoscalerKedaConfigName,
			},
		}))

	waitInformers, err := reconcilertesting.RunAndSyncInformers(ctx, infs...)
	if err != nil {
		t.Fatal("Failed to start informers:", err)
	}
	defer func() {
		cancel()
		waitInformers()
	}()

	podAutoscaler := helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass)
	fakeservingclient.Get(ctx).AutoscalingV1alpha1().PodAutoscalers(helpers.TestNamespace).Create(ctx, podAutoscaler, metav1.CreateOptions{})
	fakepainformer.Get(ctx).Informer().GetIndexer().Add(podAutoscaler)

	// The Reconciler won't do any work until it becomes the leader.
	if la, ok := ctl.Reconciler.(reconciler.LeaderAware); ok {
		la.Promote(reconciler.UniversalBucket(), func(reconciler.Bucket, types.NamespacedName) {})
	}

	err = ctl.Reconciler.Reconcile(ctx, helpers.TestNamespace+"/"+helpers.TestRevision)
	if err != nil {
		t.Error("Reconcile() =", err)
	}

	_, err = fakekedaclient.Get(ctx).KedaV1alpha1().ScaledObjects(helpers.TestNamespace).Get(ctx, helpers.TestRevision, metav1.GetOptions{})
	if err != nil {
		t.Error("error getting keda object:", err)
	}
}

func TestReconcile(t *testing.T) {
	retryAttempted := false
	deployName := helpers.TestRevision + "-deployment"
	privateSvc := names.PrivateService(helpers.TestRevision)

	table := reconcilertesting.TableTest{{
		Name: "no op",
		Objects: []runtime.Object{
			hpa(helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, WithMetricAnnotation("cpu"))),
			helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, WithPASKSReady, WithTraffic, WithScaleTargetInitialized,
				WithPAStatusService(helpers.TestRevision), WithPAMetricsService(privateSvc), withScales(0, 0)),
			deploy(helpers.TestNamespace, helpers.TestRevision),
			sks(helpers.TestNamespace, helpers.TestRevision, WithDeployRef(deployName), WithSKSReady),
		},
		Key: key(helpers.TestNamespace, helpers.TestRevision),
	}, {
		Name: "create sks with retry",
		Objects: []runtime.Object{
			helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass),
			hpa(helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision,
				WithHPAClass, WithMetricAnnotation("cpu"))),
			deploy(helpers.TestNamespace, helpers.TestRevision),
		},
		Key: key(helpers.TestNamespace, helpers.TestRevision),
		WithReactors: []ktesting.ReactionFunc{
			func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
				if retryAttempted || !action.Matches("update", "podautoscalers") || action.GetSubresource() != "status" {
					return false, nil, nil
				}
				retryAttempted = true
				return true, nil, apierrs.NewConflict(v1.Resource("foo"), "bar", errors.New("foo"))
			},
		},
		WantCreates: []runtime.Object{
			sks(helpers.TestNamespace, helpers.TestRevision, WithDeployRef(deployName)),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, withScales(0, 0),
				WithTraffic, WithPASKSNotReady("SKS Services are not ready yet")),
		}, {
			Object: helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, withScales(0, 0),
				WithTraffic, WithPASKSNotReady("SKS Services are not ready yet")),
		}},
	}, {
		Name: "reconcile sks is still not ready",
		Objects: []runtime.Object{
			hpa(helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, WithMetricAnnotation("cpu"))),
			helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass),
			deploy(helpers.TestNamespace, helpers.TestRevision),
			sks(helpers.TestNamespace, helpers.TestRevision, WithDeployRef(deployName), WithPubService,
				WithPrivateService),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, WithTraffic, withScales(0, 0),
				WithPASKSNotReady("SKS Services are not ready yet"), WithTraffic,
				WithPAStatusService(helpers.TestRevision), WithPAMetricsService(privateSvc)),
		}},
		Key: key(helpers.TestNamespace, helpers.TestRevision),
	}, {
		Name: "reconcile sks becomes ready, scale target not initialized",
		Objects: []runtime.Object{
			hpa(helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, WithPASKSNotReady("I wasn't ready yet :-("),
				WithMetricAnnotation("cpu"))),
			helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, WithPAStatusService("the-wrong-one")),
			deploy(helpers.TestNamespace, helpers.TestRevision),
			sks(helpers.TestNamespace, helpers.TestRevision, WithDeployRef(deployName), WithSKSReady),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, withScales(0, 0),
				WithPASKSReady, WithTraffic,
				WithPAStatusService(helpers.TestRevision), WithPAMetricsService(privateSvc)),
		}},
		Key: key(helpers.TestNamespace, helpers.TestRevision),
	}, {
		Name: "reconcile sks becmes ready, scale target initialized",
		Objects: []runtime.Object{
			hpa(helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, WithMetricAnnotation("cpu")), withHPAScaleStatus(1, 1)),
			helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, withScales(1, 0), WithPASKSNotReady("crufty"), WithTraffic),
			deploy(helpers.TestNamespace, helpers.TestRevision),
			sks(helpers.TestNamespace, helpers.TestRevision, WithDeployRef("bar"), WithSKSReady),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, WithPASKSReady, WithTraffic,
				WithScaleTargetInitialized, withScales(1, 1),
				WithPAStatusService(helpers.TestRevision), WithPAMetricsService(privateSvc)),
		}},
		Key: key(helpers.TestNamespace, helpers.TestRevision),
		WantUpdates: []ktesting.UpdateActionImpl{{
			Object: sks(helpers.TestNamespace, helpers.TestRevision, WithDeployRef(deployName), WithSKSReady),
		}},
	}, {
		Name: "reconcile sks",
		Objects: []runtime.Object{
			hpa(helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, WithMetricAnnotation("cpu")), withHPAScaleStatus(5, 3)),
			helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, withScales(1, 4), WithPASKSNotReady("crufty"), WithTraffic),
			deploy(helpers.TestNamespace, helpers.TestRevision),
			sks(helpers.TestNamespace, helpers.TestRevision, WithDeployRef("bar"), WithSKSReady),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, WithPASKSReady, WithTraffic,
				WithScaleTargetInitialized, withScales(5, 3),
				WithPAStatusService(helpers.TestRevision), WithPAMetricsService(privateSvc)),
		}},
		Key: key(helpers.TestNamespace, helpers.TestRevision),
		WantUpdates: []ktesting.UpdateActionImpl{{
			Object: sks(helpers.TestNamespace, helpers.TestRevision, WithDeployRef(deployName), WithSKSReady),
		}},
	}, {
		Name: "reconcile unhappy sks",
		Objects: []runtime.Object{
			hpa(helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, WithMetricAnnotation("cpu"))),
			helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, WithTraffic),
			deploy(helpers.TestNamespace, helpers.TestRevision),
			sks(helpers.TestNamespace, helpers.TestRevision, WithDeployRef(deployName+"-hairy"),
				WithPubService, WithPrivateService),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, withScales(0, 0),
				WithPASKSNotReady("SKS Services are not ready yet"), WithTraffic,
				WithPAStatusService(helpers.TestRevision), WithPAMetricsService(privateSvc)),
		}},
		Key: key(helpers.TestNamespace, helpers.TestRevision),
		WantUpdates: []ktesting.UpdateActionImpl{{
			Object: sks(helpers.TestNamespace, helpers.TestRevision, WithDeployRef(deployName),
				WithPubService, WithPrivateService),
		}},
	}, {
		Name: "reconcile sks - update fails",
		Objects: []runtime.Object{
			helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, WithTraffic, withScales(0, 0)),
			deploy(helpers.TestNamespace, helpers.TestRevision),
			sks(helpers.TestNamespace, helpers.TestRevision, WithDeployRef("bar"), WithSKSReady),
			hpa(helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, WithMetricAnnotation("cpu"))),
		},
		Key: key(helpers.TestNamespace, helpers.TestRevision),
		WithReactors: []ktesting.ReactionFunc{
			reconcilertesting.InduceFailure("update", "serverlessservices"),
		},
		WantErr: true,
		WantUpdates: []ktesting.UpdateActionImpl{{
			Object: sks(helpers.TestNamespace, helpers.TestRevision, WithDeployRef(deployName), WithSKSReady),
		}},
		WantEvents: []string{
			reconcilertesting.Eventf(corev1.EventTypeWarning, "InternalError", "error reconciling SKS: error updating SKS test-revision: inducing failure for update serverlessservices"),
		},
	}, {
		Name: "create sks - create fails",
		Objects: []runtime.Object{
			helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, withScales(0, 0), WithTraffic),
			deploy(helpers.TestNamespace, helpers.TestRevision),
			hpa(helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, WithMetricAnnotation("cpu"))),
		},
		Key: key(helpers.TestNamespace, helpers.TestRevision),
		WithReactors: []ktesting.ReactionFunc{
			reconcilertesting.InduceFailure("create", "serverlessservices"),
		},
		WantErr: true,
		WantCreates: []runtime.Object{
			sks(helpers.TestNamespace, helpers.TestRevision, WithDeployRef(deployName)),
		},
		WantEvents: []string{
			reconcilertesting.Eventf(corev1.EventTypeWarning, "InternalError", "error reconciling SKS: error creating SKS test-revision: inducing failure for create serverlessservices"),
		},
	}, {
		Name: "sks is disowned",
		Objects: []runtime.Object{
			helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass),
			deploy(helpers.TestNamespace, helpers.TestRevision),
			sks(helpers.TestNamespace, helpers.TestRevision, WithDeployRef(deployName), WithSKSOwnersRemoved, WithSKSReady),
			hpa(helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, WithMetricAnnotation("cpu"))),
		},
		Key:     key(helpers.TestNamespace, helpers.TestRevision),
		WantErr: true,
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, MarkResourceNotOwnedByPA("ServerlessService", helpers.TestRevision)),
		}},
		WantEvents: []string{
			reconcilertesting.Eventf(corev1.EventTypeWarning, "InternalError", `error reconciling SKS: PA: test-revision does not own SKS: test-revision`),
		},
	}, {
		Name: "pa is disowned",
		Objects: []runtime.Object{
			helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass),
			deploy(helpers.TestNamespace, helpers.TestRevision),
			sks(helpers.TestNamespace, helpers.TestRevision, WithDeployRef(deployName)),
			scaledObject(helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, WithMetricAnnotation("cpu"), WithPAOwnersRemoved), withScaledObjectOwnersRemoved),
		},
		Key:     key(helpers.TestNamespace, helpers.TestRevision),
		WantErr: true,
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, MarkResourceNotOwnedByPA("ScaledObject", helpers.TestRevision)),
		}},
		WantEvents: []string{
			reconcilertesting.Eventf(corev1.EventTypeWarning, "InternalError",
				`PodAutoscaler: "test-revision" does not own ScaledObject: "test-revision"`),
		},
	}, {
		Name: "nop deletion reconcile",
		// Test that with a DeletionTimestamp we do nothing.
		Objects: []runtime.Object{
			helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, WithPADeletionTimestamp),
			deploy(helpers.TestNamespace, helpers.TestRevision),
		},
		Key: key(helpers.TestNamespace, helpers.TestRevision),
	}, {
		Name: "update pa fails",
		Objects: []runtime.Object{
			hpa(helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, WithMetricAnnotation("cpu")), withHPAScaleStatus(19, 18)),
			helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, WithPAStatusService("the-wrong-one"), withScales(42, 84)),
			deploy(helpers.TestNamespace, helpers.TestRevision),
			sks(helpers.TestNamespace, helpers.TestRevision, WithDeployRef(deployName), WithSKSReady),
		},
		WantStatusUpdates: []ktesting.UpdateActionImpl{{
			Object: helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass, withScales(19, 18),
				WithPASKSReady, WithTraffic, WithScaleTargetInitialized,
				WithPAStatusService(helpers.TestRevision), WithPAMetricsService(privateSvc)),
		}},
		Key:     key(helpers.TestNamespace, helpers.TestRevision),
		WantErr: true,
		WithReactors: []ktesting.ReactionFunc{
			reconcilertesting.InduceFailure("update", "podautoscalers"),
		},
		WantEvents: []string{
			reconcilertesting.Eventf(corev1.EventTypeWarning, "UpdateFailed", `Failed to update status for "test-revision": inducing failure for update podautoscalers`),
		},
	}, {
		Name: "invalid key",
		Objects: []runtime.Object{
			helpers.PodAutoscaler(helpers.TestNamespace, helpers.TestRevision, WithHPAClass),
		},
		Key: "sandwich///",
	}}

	table.Test(t, testingv1.MakeFactory(func(ctx context.Context, listers *testingv1.Listers, _ configmap.Watcher) controller.Reconciler {
		retryAttempted = false
		ctx = podscalable.WithDuck(ctx)
		ctx, _ = fakekedaclient.With(ctx)

		r := &Reconciler{
			Base: &areconciler.Base{
				Client:           servingclient.Get(ctx),
				NetworkingClient: networkingclient.Get(ctx),
				SKSLister:        listers.GetServerlessServiceLister(),
				MetricLister:     listers.GetMetricLister(),
			},
			kubeClient: kubeclient.Get(ctx),
			hpaLister:  listers.GetHorizontalPodAutoscalerLister(),
			kedaLister: listers.GetKedaLister(),
			kedaClient: fakekedaclient.Get(ctx),
		}
		return pareconciler.NewReconciler(ctx, logging.FromContext(ctx), servingclient.Get(ctx),
			listers.GetPodAutoscalerLister(), controller.GetEventRecorder(ctx), r, autoscaling.HPA,
			controller.Options{
				ConfigStore: &testConfigStore{config: defaultConfig()},
			})
	}))
}

func sks(ns, n string, so ...SKSOption) *nv1a1.ServerlessService {
	hpa := helpers.PodAutoscaler(ns, n, WithHPAClass)
	s := aresources.MakeSKS(hpa, nv1a1.SKSOperationModeServe, 0)
	for _, opt := range so {
		opt(s)
	}
	return s
}

func key(namespace, name string) string {
	return namespace + "/" + name
}

type hpaOption func(*autoscalingv2.HorizontalPodAutoscaler)

func withScales(d, a int32) PodAutoscalerOption {
	return func(pa *autoscalingv1alpha1.PodAutoscaler) {
		pa.Status.DesiredScale, pa.Status.ActualScale = ptr.Int32(d), ptr.Int32(a)
	}
}
func withHPAScaleStatus(d, a int32) hpaOption {
	return func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
		hpa.Status.DesiredReplicas, hpa.Status.CurrentReplicas = d, a
	}
}

func hpa(pa *autoscalingv1alpha1.PodAutoscaler, options ...hpaOption) *autoscalingv2.HorizontalPodAutoscaler {
	h := resources.MakeHPA(pa, defaultConfig().Autoscaler)
	for _, o := range options {
		o(h)
	}
	return h
}

type kedaOption func(*kedav1alpha1.ScaledObject)

func withScaledObjectOwnersRemoved(scaledObj *kedav1alpha1.ScaledObject) {
	scaledObj.OwnerReferences = nil
}

func scaledObject(pa *autoscalingv1alpha1.PodAutoscaler, options ...kedaOption) *kedav1alpha1.ScaledObject {
	k, _ := kedaresources.DesiredScaledObject(hpaconfig.ToContext(context.Background(), &hpaconfig.Config{
		Autoscaler:     defaultConfig().Autoscaler,
		AutoscalerKeda: defaultConfig().AutoscalerKeda}), pa)
	for _, o := range options {
		o(k)
	}
	return k
}

type deploymentOption func(*appsv1.Deployment)

func deploy(namespace, name string, opts ...deploymentOption) *appsv1.Deployment {
	s := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-deployment",
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"a": "b",
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			Replicas: 42,
		},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func defaultConfig() *hpaconfig.Config {
	autoscalerConfig, _ := autoscalerconfig.NewConfigFromMap(nil)
	autoscalerKedaConfig, _ := hpaconfig.NewConfigFromMap(nil)
	return &hpaconfig.Config{
		Autoscaler:     autoscalerConfig,
		AutoscalerKeda: autoscalerKedaConfig,
	}
}

type testConfigStore struct {
	config *hpaconfig.Config
}

func (t *testConfigStore) ToContext(ctx context.Context) context.Context {
	return hpaconfig.ToContext(ctx, t.config)
}

var _ reconciler.ConfigStore = (*testConfigStore)(nil)
