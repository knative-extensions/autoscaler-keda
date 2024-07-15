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
	"math"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	vegeta "github.com/tsenart/vegeta/v12/lib"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pkgTest "knative.dev/pkg/test"
	resourcenames "knative.dev/serving/pkg/reconciler/revision/resources/names"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

const (
	// Concurrency must be high enough to avoid the problems with sampling
	// but not high enough to generate scheduling problems.
	successRateSLO = 0.999
	autoscaleSleep = 500

	autoscaleTestImageName = "metrics-test"
)

// TestContext includes context for autoscaler testing.
type TestContext struct {
	t          *testing.T
	clients    *test.Clients
	names      *test.ResourceNames
	resources  *v1test.ResourceObjects
	autoscaler *AutoscalerOptions
}

// AutoscalerOptions holds autoscaling parameters for knative service.
type AutoscalerOptions struct {
	Class             string
	Metric            string
	TargetUtilization float64
	Target            int
}

// Clients returns the clients of the TestContext.
func (ctx *TestContext) Clients() *test.Clients {
	return ctx.clients
}

// Resources returns the resources of the TestContext.
func (ctx *TestContext) Resources() *v1test.ResourceObjects {
	return ctx.resources
}

// SetResources sets the resources of the TestContext to the given values.
func (ctx *TestContext) SetResources(resources *v1test.ResourceObjects) {
	ctx.resources = resources
}

// Names returns the resource names of the TestContext.
func (ctx *TestContext) Names() *test.ResourceNames {
	return ctx.names
}

// SetNames set the resource names of the TestContext to the given values.
func (ctx *TestContext) SetNames(names *test.ResourceNames) {
	ctx.names = names
}

func getVegetaTarget(domain string, paramName string, paramValue int, https bool) vegeta.Target {
	scheme := "http"
	if https {
		scheme = "https"
	}
	return vegeta.Target{
		Method: http.MethodGet,
		URL:    fmt.Sprintf("%s://%s?%s=%d", scheme, domain, paramName, paramValue),
	}
}

func generateTraffic(
	ctx *TestContext,
	attacker *vegeta.Attacker,
	pacer vegeta.Pacer,
	stopChan chan struct{},
	target vegeta.Target) error {

	// The 0 duration means that the attack will only be controlled by the `Stop` function.
	results := attacker.Attack(vegeta.NewStaticTargeter(target), pacer, 0, "load-test")
	defer attacker.Stop()

	var (
		totalRequests      int32
		successfulRequests int32
	)
	for {
		select {
		case <-stopChan:
			ctx.t.Logf("Stopping generateTraffic")
			successRate := float64(1)
			if totalRequests > 0 {
				successRate = float64(successfulRequests) / float64(totalRequests)
			}
			if successRate < successRateSLO {
				return fmt.Errorf("request success rate under SLO: total = %d, errors = %d, rate = %f, SLO = %f",
					totalRequests, totalRequests-successfulRequests, successRate, successRateSLO)
			}
			return nil
		case res := <-results:
			totalRequests++
			if res.Code != http.StatusOK {
				ctx.t.Logf("Status = %d, want: 200", res.Code)
				ctx.t.Logf("URL: %s Start: %s End: %s Duration: %v Error: %s Body:\n%s",
					res.URL, res.Timestamp.Format(time.RFC3339), res.End().Format(time.RFC3339), res.Latency, res.Error, string(res.Body))
				continue
			}
			successfulRequests++
		}
	}
}

func newVegetaHTTPClient(ctx *TestContext, url *url.URL) *http.Client {

	vegetaTransportDefaults := func(transport *http.Transport) *http.Transport {
		transport.MaxIdleConnsPerHost = vegeta.DefaultConnections
		transport.MaxConnsPerHost = vegeta.DefaultMaxConnections
		return transport
	}

	spoof, err := pkgTest.NewSpoofingClient(
		context.Background(),
		ctx.Clients().KubeClient,
		ctx.t.Logf,
		url.Hostname(),
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), ctx.t.Logf, ctx.Clients(), test.ServingFlags.HTTPS),
		vegetaTransportDefaults,
	)

	if err != nil {
		ctx.t.Fatal("Error creating spoofing client:", err)
	}
	return spoof.Client
}

func generateTrafficAtFixedRPS(ctx *TestContext, rps int, stopChan chan struct{}) error {
	pacer := vegeta.ConstantPacer{Freq: rps, Per: time.Second}
	attacker := vegeta.NewAttacker(
		vegeta.Timeout(0),
		vegeta.Client(newVegetaHTTPClient(ctx, ctx.resources.Route.Status.URL.URL())),
	)

	target := getVegetaTarget(ctx.resources.Route.Status.URL.URL().Hostname(), "sleep", autoscaleSleep, test.ServingFlags.HTTPS)

	ctx.t.Logf("Maintaining %v RPS.", rps)
	return generateTraffic(ctx, attacker, pacer, stopChan, target)
}

func numberOfReadyPods(ctx *TestContext) (float64, *appsv1.Deployment, error) {
	n := resourcenames.Deployment(ctx.resources.Revision)
	deploy, err := ctx.clients.KubeClient.AppsV1().Deployments(test.ServingFlags.TestNamespace).Get(
		context.Background(), n, metav1.GetOptions{})
	if err != nil {
		return 0, nil, fmt.Errorf("failed to get deployment %s: %w", n, err)
	}

	if isInRollout(deploy) {
		// Ref: #11092
		// The deployment was updated and the update is being rolled out so we defensively
		// pick the desired replicas to assert the autoscaling decisions.
		// TODO: Drop this once we solved the underscale issue.
		ctx.t.Logf("Deployment is being rolled, picking spec.replicas=%d", *deploy.Spec.Replicas)
		return float64(*deploy.Spec.Replicas), deploy, nil
	}
	// Otherwise we pick the ready pods to assert maximum consistency for ramp up tests.
	return float64(deploy.Status.ReadyReplicas), deploy, nil
}

func checkPodScale(ctx *TestContext, targetPods, minPods, maxPods float64, done <-chan time.Time, quick bool) error {
	// Short-circuit traffic generation once we exit from the check logic.
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	originalMaxPods := maxPods
	for {
		select {
		case <-ticker.C:
			// Each 2 second, check that the number of pods is at least `minPods`. `minPods` is increasing
			// to verify that the number of pods doesn't go down while we are scaling up.
			got, d, err := numberOfReadyPods(ctx)
			if err != nil {
				return err
			}

			if isInRollout(d) {
				// Ref: #11092
				// Allow for a higher scale if the deployment is being rolled as that
				// might be skewing metrics in the autoscaler.
				maxPods = math.Ceil(originalMaxPods * 1.2)
			}

			mes := fmt.Sprintf("revision %q #replicas: %v, want at least: %v", ctx.resources.Revision.Name, got, minPods)
			ctx.t.Logf(mes)
			// verify that the number of pods doesn't go down while we are scaling up.
			if got < minPods {
				return fmt.Errorf("interim scale didn't fulfill constraints: %s\ndeployment state: %s", mes, spew.Sdump(d))
			}
			// A quick test succeeds when the number of pods scales up to `targetPods`
			// (and, as an extra check, no more than `maxPods`).
			if quick && got >= targetPods && got <= maxPods {
				ctx.t.Logf("Quick Mode: got %v >= %v", got, targetPods)
				return nil
			}
			if minPods < targetPods-1 {
				// Increase `minPods`, but leave room to reduce flakiness.
				minPods = math.Min(got, targetPods) - 1
			}

		case <-done:
			// The test duration is over. Do a last check to verify that the number of pods is at `targetPods`
			// (with a little room for de-flakiness).
			got, d, err := numberOfReadyPods(ctx)
			if err != nil {
				return fmt.Errorf("failed to fetch number of ready pods: %w", err)
			}

			if isInRollout(d) {
				// Ref: #11092
				// Allow for a higher scale if the deployment is being rolled as that
				// might be skewing metrics in the autoscaler.
				maxPods = math.Ceil(originalMaxPods * 1.2)
			}

			mes := fmt.Sprintf("revision %q #replicas: %v, want between [%v, %v]", ctx.resources.Revision.Name, got, targetPods-1, maxPods)
			ctx.t.Logf(mes)
			if got < targetPods-1 || got > maxPods {
				return fmt.Errorf("final scale didn't fulfill constraints: %s\ndeployment state: %s", mes, spew.Sdump(d))
			}
			return nil
		}
	}
}

// isInRollout is a loose copy of the kubectl function handling rollouts.
// See: https://github.com/kubernetes/kubectl/blob/0149779a03735a5d483115ca4220a7b6c861430c/pkg/polymorphichelpers/rollout_status.go#L75-L91
func isInRollout(deploy *appsv1.Deployment) bool {
	if deploy.Generation > deploy.Status.ObservedGeneration {
		// Waiting for update to be observed.
		return true
	}
	if deploy.Spec.Replicas != nil && deploy.Status.UpdatedReplicas < *deploy.Spec.Replicas {
		// Not enough replicas updated yet.
		return true
	}
	if deploy.Status.Replicas > deploy.Status.UpdatedReplicas {
		// Old replicas are being terminated.
		return true
	}
	if deploy.Status.AvailableReplicas < deploy.Status.UpdatedReplicas {
		// Not enough available yet.
		return true
	}
	return false
}
