//go:build e2e
// +build e2e

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

package e2e

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	resources2 "knative.dev/autoscaler-keda/pkg/reconciler/autoscaling/hpa/resources"
	"knative.dev/pkg/kmap"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/helpers"
	"knative.dev/pkg/test/spoof"
	"knative.dev/serving/pkg/apis/autoscaling"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	resourcenames "knative.dev/serving/pkg/reconciler/revision/resources/names"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	test2e "knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

const (
	scaleUpTimeout        = 10 * time.Minute
	scaleToMinimumTimeout = 15 * time.Minute // 5 minutes is the default window for hpa to calculate if should scale down
	minPods               = 1.0
	maxPods               = 10.0
	targetPods            = 2
)

func TestUpDownCustomMetric(t *testing.T) {
	metric := "http_requests_total"
	target := 5
	configAnnotations := map[string]string{
		autoscaling.ClassAnnotationKey:                    autoscaling.HPA,
		autoscaling.MetricAnnotationKey:                   metric,
		autoscaling.TargetAnnotationKey:                   strconv.Itoa(target),
		autoscaling.MinScaleAnnotationKey:                 "1",
		autoscaling.MaxScaleAnnotationKey:                 fmt.Sprintf("%d", int(maxPods)),
		autoscaling.WindowAnnotationKey:                   "20s",
		resources2.KedaAutoscaleAnnotationPrometheusQuery: fmt.Sprintf("sum(rate(http_requests_total{namespace='%s'}[1m]))", test.ServingFlags.TestNamespace),
	}
	ctx := setupCustomHPASvc(t, metric, target, configAnnotations, "")
	test.EnsureTearDown(t, ctx.Clients(), ctx.Names())
	assertCustomHPAAutoscaleUpToNumPods(ctx, targetPods, time.After(scaleUpTimeout), true /* quick */)
	assertScaleDownToN(ctx, 1)
	assertCustomHPAAutoscaleUpToNumPods(ctx, targetPods, time.After(scaleUpTimeout), true /* quick */)
}

func TestScaleToZero(t *testing.T) {
	metric := "raw_scale"
	target := 5
	configAnnotations := map[string]string{
		autoscaling.ClassAnnotationKey:                    autoscaling.HPA,
		autoscaling.MetricAnnotationKey:                   metric,
		autoscaling.TargetAnnotationKey:                   strconv.Itoa(target),
		autoscaling.MinScaleAnnotationKey:                 "0",
		autoscaling.MaxScaleAnnotationKey:                 fmt.Sprintf("%d", int(maxPods)),
		autoscaling.WindowAnnotationKey:                   "20s",
		resources2.KedaAutoscaleAnnotationMetricType:      string(autoscalingv2.ValueMetricType),
		resources2.KedaAutoscaleAnnotationPrometheusQuery: fmt.Sprintf("sum by (service) (%s{namespace='%s'})", metric, test.ServingFlags.TestNamespace),
	}

	// Create a ksvc that will control another one to scale up/down via its metric values
	configAnnotationsScale := map[string]string{
		autoscaling.MinScaleAnnotationKey: "1",
		autoscaling.MaxScaleAnnotationKey: "1",
	}
	scaleSvcName := helpers.MakeK8sNamePrefix(strings.TrimPrefix(t.Name(), "scale"))
	ctxScale := setupSvc(t, metric, target, configAnnotationsScale, scaleSvcName)
	test.EnsureTearDown(t, ctxScale.Clients(), ctxScale.names)

	// Create a ksvc that will be scaled up/down based on a metric value set by another, mimicking external metrics
	ctx := setupCustomHPASvcFromZero(t, metric, target, configAnnotations, "")
	test.EnsureTearDown(t, ctx.Clients(), ctx.Names())

	// Set the scale metric to 20, which should create 20/target=4 pods
	ctxScale.names.URL.RawQuery = "scale=20"
	if _, err := pkgTest.CheckEndpointState(
		context.Background(),
		ctxScale.clients.KubeClient,
		t.Logf,
		ctxScale.names.URL,
		spoof.MatchesAllOf(spoof.MatchesBody("Scaling to 20")),
		"CheckingEndpointScaleText",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, ctxScale.clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("Error probing %s: %v", ctxScale.names.URL.Hostname(), err)
	}

	// Waiting until HPA status is available, as it takes some time until HPA starts collecting metrics.
	if err := waitForHPAReplicas(t, ctx.resources.Revision.Name, ctx.resources.Revision.Namespace, ctx.clients); err != nil {
		t.Fatalf("Error collecting metrics by HPA: %v", err)
	}

	assertAutoscaleUpToNumPods(ctx, targetPods*2, time.After(scaleUpTimeout), true /* quick */, 0)

	// Set scale metric to zero
	ctxScale.names.URL.RawQuery = "scale=0"
	if _, err := pkgTest.CheckEndpointState(
		context.Background(),
		ctxScale.clients.KubeClient,
		t.Logf,
		ctxScale.names.URL,
		spoof.MatchesAllOf(spoof.MatchesBody("Scaling to 0")),
		"CheckingEndpointScaleText",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, ctxScale.clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("Error probing %s: %v", ctxScale.names.URL.Hostname(), err)
	}

	assertScaleDownToN(ctx, 0)

	// Set scale metric to 20 again, which should create 20/target=4 pods
	ctxScale.names.URL.RawQuery = "scale=20"
	if _, err := pkgTest.CheckEndpointState(
		context.Background(),
		ctxScale.clients.KubeClient,
		t.Logf,
		ctxScale.names.URL,
		spoof.MatchesAllOf(spoof.MatchesBody("Scaling to 20")),
		"CheckingEndpointScaleText",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, ctxScale.clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("Error probing %s: %v", ctxScale.names.URL.Hostname(), err)
	}

	// Waiting until HPA status is available, as it takes some time until HPA starts collecting metrics again after scale to zero.
	// Keda de-activates the HPA if metrics is zero, so we need to wait for it to be active again.
	if err := waitForHPAReplicas(t, ctx.resources.Revision.Name, ctx.resources.Revision.Namespace, ctx.clients); err != nil {
		t.Fatalf("Error collecting metrics by HPA: %v", err)
	}
	assertAutoscaleUpToNumPods(ctx, targetPods*2, time.After(scaleUpTimeout), true /* quick */, 0)
}

func setupCustomHPASvc(t *testing.T, metric string, target int, annos map[string]string, svcName string) *TestContext {
	t.Helper()
	clients := test2e.Setup(t)
	var svc string
	if svcName != "" {
		svc = svcName
	} else {
		svc = test.ObjectNameForTest(t)
	}

	t.Log("Creating a new Route and Configuration")
	names := &test.ResourceNames{
		Service: svc,
		Image:   autoscaleTestImageName,
	}
	resources, err := v1test.CreateServiceReady(t, clients, names,
		[]rtesting.ServiceOption{
			withConfigLabels(map[string]string{"metrics-test": "metrics-test"}),
			rtesting.WithConfigAnnotations(annos), rtesting.WithResourceRequirements(corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("30m"),
					corev1.ResourceMemory: resource.MustParse("20Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("300m"),
				},
			}),
		}...)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	if _, err := pkgTest.CheckEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		names.URL,
		spoof.MatchesAllOf(spoof.IsStatusOK),
		"CheckingEndpointAfterCreate",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("Error probing %s: %v", names.URL.Hostname(), err)
	}

	// Waiting until HPA status is available, as it takes some time until HPA starts collecting metrics.
	if err := waitForHPAState(t, resources.Revision.Name, resources.Revision.Namespace, clients); err != nil {
		t.Fatalf("Error collecting metrics by HPA: %v", err)
	}

	return &TestContext{
		t:         t,
		clients:   clients,
		names:     names,
		resources: resources,
		autoscaler: &AutoscalerOptions{
			Metric: metric,
			Target: target,
		},
	}
}

func setupCustomHPASvcFromZero(t *testing.T, metric string, target int, annos map[string]string, svcName string) *TestContext {
	t.Helper()
	clients := test2e.Setup(t)
	var svc string
	if svcName != "" {
		svc = svcName
	} else {
		svc = test.ObjectNameForTest(t)
	}

	t.Log("Creating a new Route and Configuration")
	names := &test.ResourceNames{
		Service: svc,
		Image:   autoscaleTestImageName,
	}
	resources, err := CreateServiceReady(t, clients, names,
		[]rtesting.ServiceOption{
			withConfigLabels(map[string]string{"metrics-test": "metrics-test"}),
			rtesting.WithConfigAnnotations(annos), rtesting.WithResourceRequirements(corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("30m"),
					corev1.ResourceMemory: resource.MustParse("20Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("300m"),
				},
			}),
		}...)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	return &TestContext{
		t:         t,
		clients:   clients,
		names:     names,
		resources: resources,
		autoscaler: &AutoscalerOptions{
			Metric: metric,
			Target: target,
		},
	}
}

func setupSvc(t *testing.T, metric string, target int, annos map[string]string, svcName string) *TestContext {
	t.Helper()
	clients := test2e.Setup(t)
	var svc string
	if svcName != "" {
		svc = svcName
	} else {
		svc = test.ObjectNameForTest(t)
	}

	t.Log("Creating a new Route and Configuration")
	names := &test.ResourceNames{
		Service: svc,
		Image:   autoscaleTestImageName,
	}
	resources, err := v1test.CreateServiceReady(t, clients, names,
		[]rtesting.ServiceOption{
			withConfigLabels(map[string]string{"metrics-test": "metrics-test"}),
			rtesting.WithConfigAnnotations(annos), rtesting.WithResourceRequirements(corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("30m"),
					corev1.ResourceMemory: resource.MustParse("20Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("300m"),
				},
			}),
		}...)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	if _, err := pkgTest.CheckEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		names.URL,
		spoof.MatchesAllOf(spoof.IsStatusOK),
		"CheckingEndpointAfterCreate",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("Error probing %s: %v", names.URL.Hostname(), err)
	}

	return &TestContext{
		t:         t,
		clients:   clients,
		names:     names,
		resources: resources,
		autoscaler: &AutoscalerOptions{
			Metric: metric,
			Target: target,
		},
	}
}

func assertCustomHPAAutoscaleUpToNumPods(ctx *TestContext, targetPods float64, done <-chan time.Time, quick bool) {
	ctx.t.Helper()

	stopChan := make(chan struct{})
	var grp errgroup.Group
	grp.Go(func() error {
		return generateTrafficAtFixedRPS(ctx, 10, stopChan)
	})

	grp.Go(func() error {
		defer close(stopChan)
		return checkPodScale(ctx, targetPods, minPods, maxPods, done, quick)
	})

	if err := grp.Wait(); err != nil {
		ctx.t.Fatal(err)
	}
}

func assertAutoscaleUpToNumPods(ctx *TestContext, targetPods float64, done <-chan time.Time, quick bool, num float64) {
	ctx.t.Helper()
	var grp errgroup.Group
	grp.Go(func() error {
		return checkPodScale(ctx, targetPods, num, maxPods, done, quick)
	})
	if err := grp.Wait(); err != nil {
		ctx.t.Fatal(err)
	}
}

func assertScaleDownToN(ctx *TestContext, n int) {
	deploymentName := resourcenames.Deployment(ctx.resources.Revision)
	if err := waitForScaleToOne(ctx.t, deploymentName, ctx.clients); err != nil {
		ctx.t.Fatalf("Unable to observe the Deployment named %s scaling down: %v", deploymentName, err)
	}
	ctx.t.Logf("Wait for all pods to terminate.")

	if err := pkgTest.WaitForPodListState(
		context.Background(),
		ctx.clients.KubeClient,
		func(p *corev1.PodList) (bool, error) {
			if !(len(getDepPods(p.Items, deploymentName)) == n) {
				return false, nil
			}
			return true, nil
		},
		"WaitForAvailablePods", test.ServingFlags.TestNamespace); err != nil {
		ctx.t.Fatalf("Waiting for Pod.List to have no non-Evicted pods of %q: %v", deploymentName, err)
	}

	ctx.t.Logf("The Revision should remain ready after scaling to %d.", n)
	if err := v1test.CheckRevisionState(ctx.clients.ServingClient, ctx.names.Revision, v1test.IsRevisionReady); err != nil {
		ctx.t.Fatalf("The Revision %s did not stay Ready after scaling down to zero: %v", ctx.names.Revision, err)
	}

	ctx.t.Logf("Scaled down.")
}

func getDepPods(nsPods []corev1.Pod, deploymentName string) []corev1.Pod {
	var pods []corev1.Pod
	for _, p := range nsPods {
		if strings.Contains(p.Name, deploymentName) && !strings.Contains(p.Status.Reason, "Evicted") {
			pods = append(pods, p)
		}
	}
	return pods
}

func waitForScaleToOne(t *testing.T, deploymentName string, clients *test.Clients) error {
	t.Helper()
	t.Logf("Waiting for %q to scale to one", deploymentName)

	return pkgTest.WaitForDeploymentState(
		context.Background(),
		clients.KubeClient,
		deploymentName,
		func(d *appsv1.Deployment) (bool, error) {
			return d.Status.ReadyReplicas == 1, nil
		},
		"DeploymentIsScaledDown",
		test.ServingFlags.TestNamespace,
		scaleToMinimumTimeout,
	)
}

func waitForHPAReplicas(t *testing.T, name, namespace string, clients *test.Clients) error {
	return wait.PollUntilContextTimeout(context.Background(), time.Second, 15*time.Minute, true, func(context.Context) (bool, error) {
		hpa, err := clients.KubeClient.AutoscalingV2().HorizontalPodAutoscalers(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		if hpa.Status.CurrentMetrics == nil || hpa.Status.CurrentReplicas < 1 {
			t.Logf("Waiting for hpa.status is available: %#v", hpa.Status)
			return false, nil
		}
		return true, nil
	})
}

func waitForHPAState(t *testing.T, name, namespace string, clients *test.Clients) error {
	return wait.PollUntilContextTimeout(context.Background(), time.Second, 15*time.Minute, true, func(context.Context) (bool, error) {
		hpa, err := clients.KubeClient.AutoscalingV2().HorizontalPodAutoscalers(namespace).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		if hpa.Status.CurrentMetrics == nil {
			t.Logf("Waiting for hpa.status is available: %#v", hpa.Status)
			return false, nil
		}
		return true, nil
	})
}

func withConfigLabels(labels map[string]string) rtesting.ServiceOption {
	return func(service *v1.Service) {
		service.Spec.Template.Labels = kmap.Union(
			service.Spec.Template.Labels, labels)
	}
}
