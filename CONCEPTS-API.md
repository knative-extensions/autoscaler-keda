# Introduction

The goal of the Autoscaler KEDA extension is to keep the existing UX provided by the current Knative Serving and HPA integration.
It builds on top of that to allow more fine-grained configurations and replaces the current HPA implementation with a more flexible and powerful one.
The point of integration between KEDA and this extension is the existence of an HPA resource.
If an HPA resource is available for a Knative Service, then the extension can reconcile the corresponding SKS in order networking setup to follow up.

In the current implementation at the core Serving, this HPA resource is created by the Serving controller based on the Pod Autoscaler (PA) resource for the current revision.
The latter happens when the user sets the annotation: `autoscaling.knative.dev/class: hpa.autoscaling.knative.dev` in the Knative service (ksvc).

In this extension we create a ScaledObject instead of an HPA resource and delegate the HPA creation to KEDA.
The above annotation still needs to exist for the extension to pick up the corresponding PA resource created for each revision
and to create the ScaledObject based on that PA resource. With this approach we can benefit from using KEDA for a shorter monitoring cycle (no need for a Prometheus Adapter service) and its features eg. multiple triggers.
Users can drive configurations from the ksvc resource via annotations or can bring their own ScaledObject. The extension supports two modes: ScaledObject auto-creation and bring your own (BYO) ScaledObject.

# Configuration API


## Basic configuration

The idea is to configure the ScaledObject resource via annotations. Annotations need to be specified in the ksvc resource at the `spec.template.metadata.annotations` level.
A user can start with `cpu` or `memory` for the metric annotation as with current Serving HPA support and need to only specify a target.
Note here that if no metric name specified the Serving controller will create a PA resource with a metric set to `cpu` by default.
In any case when defining a `cpu` or `memory` metric the user needs to specify the corresponding `resources.request` for each container deployed.
The rest is automatically setup by the extension and the corresponding ScaledObject is created.

For example:

```yaml
...
spec:
  template:
    metadata:
      annotations:
        autoscaling.knative.dev/class: "hpa.autoscaling.knative.dev"
        autoscaling.knative.dev/metric: "cpu"
        autoscaling.knative.dev/minScale: "1"
        autoscaling.knative.dev/maxScale: "10"
        autoscaling.knative.dev/target: "50"
    spec:
        containers:
        - resources:
            requests:
              cpu: "100m"
...
```

MinScale and maxScale define the minimum and maximum allowed replicas, they are optional and if not specified the extension will use the default values of 1 and infinite respectively.

**Important** : At this point scale from zero is not supported, thus triggers should be always active.

**Note** : For ready to use examples check samples under `./test/test_images/metrics-test` and the [DEVELOPMENT](DEVELOPMENT.md) guide on how to apply them.

## Custom metric configuration

If the user chooses a custom metric then he needs to define additionally the metric name, the Prometheus address and the query through the following annotations:

```yaml
...
spec:
  template:
    metadata:
      annotations:
        autoscaling.knative.dev/class: "hpa.autoscaling.knative.dev"
        autoscaling.knative.dev/minScale: "1"
        autoscaling.knative.dev/maxScale: "10"
        autoscaling.knative.dev/target: "5"
        autoscaling.knative.dev/metric: "my_metric"
        autoscaling.knative.dev/prometheus-address: "http://prometheus:9090"
        autoscaling.knative.dev/prometheus-query: sum(rate(http_requests_total{namespace="test"}[1m]))
...
```

In scenarios where more advanced Prometheus setup are used e.g. Thanos, the user can specify the Prometheus auth name, kind and modes.

```yaml
...
spec:
  template:
    metadata:
      annotations:
        autoscaling.knative.dev/metric: "my_metric"
        autoscaling.knative.dev/prometheus-address: "https://thanos-querier.openshift-monitoring.svc.cluster.local:9092"
        autoscaling.knative.dev/prometheus-query: sum(rate(http_requests_total{namespace="test"}[1m]))
        autoscaling.knative.dev/trigger-prometheus-auth-name: "keda-trigger-auth-prometheus"
        autoscaling.knative.dev/trigger-prometheus-auth-modes: "bearer"
        autoscaling.knative.dev/trigger-prometheus-auth-kind: "TriggerAuthentication"
...
```

User can also configure the metric type by defining the following annotation:

```
autoscaling.knative.dev/metric-type: "AverageValue"
```

Allowed values are: `AverageValue`, `Utilization`, `Value`.
If no metric type is used the following defaults are used:
- cpu: `Utilization`
- memory: `AverageValue`
- custom: `AverageValue`

Allowed values for the metric type are:
- cpu: `Utilization`, `AverageValue`
- memory: `Utilization`, `AverageValue`
- custom: `Utilization`, `AverageValue`, `Value`

For cpu, memory and a custom metric the extension creates a default Prometheus trigger, however user can add more triggers in json format via the following annotation:

```
autoscaling.knative.dev/extra-prometheus-triggers: '[{"type": "prometheus", "name": "trigger2",  "metadata": { "serverAddress": "http://prometheus-operated.default.svc:9090" , "namespace": "test-namespace",  "query": "sum(rate(http_requests_total{}[1m]))", "threshold": "5"}}]'
```
This is useful when the user wants to scale based on multiple metrics and also in combination with the scaling modifiers feature.

The scaling modifiers annotation allows to configure that property in the ScaledObject using json format:

```yaml
...
spec:
  template:
    metadata:
      annotations:
        autoscaling.knative.dev/metric: "my_metric"
        autoscaling.knative.dev/trigger-prometheus-name: "trigger1"
        autoscaling.knative.dev/prometheus-query: sum(rate(http_requests_total{namespace="test"}[1m]))
        autoscaling.knative.dev/scaling-modifiers: '{"formula": "(trigger1 + trigger2)/2", "target": "5", "activationTarget": "1", "metricType": "AverageValue"}'
        autoscaling.knative.dev/extra-prometheus-triggers: '[{"type": "prometheus", "name": "trigger2",  "metadata": { "serverAddress": "http://prometheus-operated.default.svc:9090" , "namespace": "test-namespace",  "query": "sum(rate(http_requests_total{namespace=\"test\"}[1m]))", "threshold": "5"}}]'
...
```

The extension creates a default Prometheus trigger named `default-trigger`, here we change the name via the annotation `autoscaling.knative.dev/trigger-prometheus-name`. Then we use that name as part of the formula.
The second trigger is defined completely via an annotation as seen above.

### Trigger metadata - namespace

The extension injects the namespace of the ksvc in the trigger's metadata. This is required when the user wants to use systems like Thanos e.g. on Openshift.
When used with Thanos the namespace is added to the query url and makes sure the metrics are namespaced. That means you dont need to add namespace in the perometheus query.
However, that is not the case if you use Prometheus directly, you need to add the namespace in the query.

## Override the ScaledObject

The user can also specify the ScaledObject directly in json format via the following annotation:

```
autoscaling.knative.dev/scaled-object-override: '{...}'
```

The extension will make sure that the ScaledObject is created with proper settings like annotations, max replicas, owner references
and name. The rest of the object properties will be passed without modification. This is meant for power users who want to have full control over the ScaledObject
but still let the extension manage the target reference. In the scenario where the user wants to bring his own ScaledObject and disable the autoscreation he will have to track revisions
make sure the targetRef is correct each time.

If the user does not want the extension to create the ScaledObject, he can bring his own ScaledObject and disable the autoscreation either globally in autoscaler-keda configmap or per ksvc
using the annotation:

```
autoscaling.knative.dev/scaled-object-auto-create: "false"
```

## HPA Advanced Configuration

HPA allows to stabilize the scaling process by introducing a stabilization window. By default, this is 5 minutes.
If user has specified a window annotation for example `autoscaling.knative.dev/window: "20s"` then the extension will pass this setting to the HPA scale up/down
stabilization period. The stabilization window is used by the HPA algorithm to gather recommendations. At the end of it a decision is made.
See [here](https://github.com/kubernetes/enhancements/blob/master/keps/sig-autoscaling/853-configurable-hpa-scale-velocity/README.md) for more.
When `autoscaling.knative.dev/window` is present the extension will not set up any hpa policies by default.
K8s will setup the scaleup/scaledown policies to the defaults as follows:

```yaml
{
  "behavior": {
    "scaleDown": {
      "policies": [
        {
          "periodSeconds": 15,
          "type": "Percent",
          "value": 100
        }
      ],
      "selectPolicy": "Max",
      "stabilizationWindowSeconds": 20
    },
    "scaleUp": {
      "policies": [
        {
          "periodSeconds": 15,
          "type": "Pods",
          "value": 4
        },
        {
          "periodSeconds": 15,
          "type": "Percent",
          "value": 100
        }
      ],
      "selectPolicy": "Max",
      "stabilizationWindowSeconds": 20
    }
  }
}
```

User can override the above completely by setting the annotations using json format:

```
autoscaling.knative.dev/hpa-scale-up-rules: '{...}'
autoscaling.knative.dev/hpa-scale-down-rules: '{...}'
```
