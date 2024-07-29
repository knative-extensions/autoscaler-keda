# Introduction

The goal of the Autoscaler KEDA extension is to keep the existing UX provided by the current Knative Serving and HPA integration.
It builds on top of that to allow more fine-grained configurations and replaces the current HPA implementation with a more flexible and powerful one.
The point of integration between KEDA and this extension is the existance of an HPA resource.
If an HPA resource is available for a Knative Service, then the extension can reconcile the corresponding SKS in order networking setup to follow up.
In the current implementation at the core Serving, this HPA resource is created by the Serving controller based on the Pod Autoscaler (PA) resource for the current revision.
The latter happens when the user sets the annotation: `autoscaling.knative.dev/class: hpa.autoscaling.knative.dev` in the ksvc.
In this extension we create a ScaledObject instead of an HPA resource and delegate the HPA creation to KEDA.
The above annotation still needs to exist for the extension to pick up the corresponding PA resource created for each revision
and to create the ScaledObject based on that PA resource. 
With the approach taken with this extension we can benefit from using KEDA for a shorter monitoring cycle (no need for a Prometheus Adapter service) and it's features eg. multiple triggers.
Users can drive configurations from the ksvc resource via annotations or can bring their own ScaledObject.
The extension supports two modes: ScaledObject auto-creation and bring your own (BYO) ScaledObject.

# Configuration API

The idea is to configure the ScaledObject resource via annotations. This is done incrementally. Annotations need to be specified in the ksvc resource at the `spec.template.metadata.annotations` level.
A user can start with "cpu" or "memory" for the metric annotation as with current Serving HPA support and need to only specify a target.
The rest is automatically setup by the extension and the corresponding ScaledObject is created. If the user chooses a custom metric then he needs to define the Prometheus address and query through the following annotations:

```
autoscaling.knative.dev/prometheus-address: http://prometheus:9090
autoscaling.knative.dev/prometheus-query: sum(rate(http_requests_total{job="myjob"}[1m]))
```

In scenarios where more advanced Prometheus setup are used eg. Thanos, the user can specify the Prometheus auth name, kind and modes.

```
autoscaling.knative.dev/prometheus-address: "https://thanos-querier.openshift-monitoring.svc.cluster.local:9092"
autoscaling.knative.dev/trigger-prometheus-auth-name: "keda-trigger-auth-prometheus"
autoscaling.knative.dev/trigger-prometheus-auth-modes: "bearer"
autoscaling.knative.dev/trigger-prometheus-auth-kind: "TriggerAuthentication"
```

For cpu, memory and a custom metric the extension creates a default Prometheus trigger, however user can add more triggers in json format via the following annotation:

```
autoscaling.knative.dev/extra-prometheus-triggers="[{"type": "prometheus",  "metadata": { "serverAddress": "http://prometheus-operated.default.svc:9090" , "namespace": "test-namespace",  "query": "sum(rate(http_requests_total{}[1m]))", "threshold": "5"}}]"
```
This is useful when the user wants to scale based on multiple metrics and also in combination with the scaling modifiers feature.

The scaling modifiers annotation allows to configure that property in the ScaledObject using json format:

```
autoscaling.knative.dev/scaling-modifiers="{"formula": "(sum(rate(http_requests_total{}[1m])) + sum(rate(http_requests_total{}[1m])))/2", "target": "5", "activationTarget": "1", "metricType": "AverageValue"}"
```

The user can also specify the ScaledObject directly in json format via the following annotation:

```
autoscaling.knative.dev/scaled-object-override = "{...}"
```
The extension will make sure that the ScaledObject is created with proper settings like annotations, max replicas, owner references
and name. The rest of the object properties will be passed without modification. This is meant for power users who want to have full control over the ScaledObject
but still let the extension manage the target reference. In the scenario where the user wants to bring his own ScaledObject and disable the autoscreation he will have to track revisions
make sure the targetRef is correct each time he brings his own ScaledObject.

```

If the user does not want the extension to create the ScaledObject, he can bring his own ScaledObject and disable the autoscreation either globally in autoscaler-keda configmap or per ksvc
using the annotation:

```
autoscaling.knative.dev/scaled-object-auto-create: "false"
```
