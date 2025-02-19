
## Run Locally

Please refer to the [serving](https://github.com/knative/serving/blob/main/DEVELOPMENT.md) development guidelines for a list of required tools and settings.

The following sets up the `autoscaler-keda` extension on a local Minikube cluster.

**NOTE:** the initial version of this extension was tested on `1.14.0` using only `kourier` as networking layer.

### Install Serving with KEDA support for HPA

#### Setup Minikube

**NOTE:** depending on your OS, the following command may need edits:

```bash
$ MEMORY=${MEMORY:-40000}
$ CPUS=${CPUS:-6}

$ EXTRA_CONFIG="apiserver.enable-admission-plugins=\
LimitRanger,\
NamespaceExists,\
NamespaceLifecycle,\
ResourceQuota,\
ServiceAccount,\
DefaultStorageClass,\
MutatingAdmissionWebhook"

$ minikube start --driver=kvm2 --memory=$MEMORY --cpus=$CPUS \
  --kubernetes-version=v1.30.0 \
  --disk-size=30g \
  --extra-config="$EXTRA_CONFIG" \
  --extra-config=kubelet.authentication-token-webhook=true
```

#### Install cert-manager

```bash
$ kubectl apply -f ./third_party/cert-manager-latest/cert-manager.yaml
```

#### Install Knative Serving

```bash
$ kubectl apply -f https://github.com/knative/serving/releases/download/knative-v1.16.0/serving-crds.yaml
$ kubectl apply -f https://github.com/knative/serving/releases/download/knative-v1.16.0/serving-core.yaml
```

#### Install and configure a networking layer

Follow the instructions in the
[Knative installation doc](https://knative.dev/docs/admin/install/serving/install-serving-with-yaml/#install-a-networking-layer) for more complete instructions on networking layers. 

**NOTE:** this documentation was tested only with `istio` and `kourier`.

##### Istio

To setup the `autoscaler-keda` extension with `istio` follow the [istio development instructions](./ISTIO_DOC.md)

##### Kourier

```bash
$ kubectl apply -f https://github.com/knative/net-kourier/releases/download/knative-v1.16.0/kourier.yaml

$ kubectl patch configmap/config-network \
  -n knative-serving \
  --type merge \
  -p '{"data":{"ingress.class":"kourier.ingress.networking.knative.dev"}}'
```

Check that all pods in a `Running` state:

```bash
$ kubectl get po -n knative-serving
NAME                                      READY   STATUS    RESTARTS   AGE
activator-d66fd5dd8-875zt                 1/1     Running   0          2m41s
autoscaler-6c7bf97997-clc7q               1/1     Running   0          2m41s
controller-5b54cd98c-brd6l                1/1     Running   0          2m41s
net-kourier-controller-5db85876d8-7hnr7   1/1     Running   0          2m30s
webhook-56ffd84996-qskmt                  1/1     Running   0          2m41s
```

##### Configure knative domain 

```bash
$ kubectl patch configmap/config-domain \
      --namespace knative-serving \
      --type merge \
      --patch '{"data":{"example.com":""}}'
```

#### Install Prometheus and KEDA

```bash
$ helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
$ helm repo add kedacore https://kedacore.github.io/charts
$ helm repo update

$ helm install prometheus prometheus-community/kube-prometheus-stack -f values.yaml
$ helm install keda kedacore/keda --namespace keda --create-namespace
```

Check that all pods in a `Running` state:

```bash
$ kubectl get po -n default
NAME                                                     READY   STATUS    RESTARTS   AGE
alertmanager-prometheus-kube-prometheus-alertmanager-0   2/2     Running   0          61s
prometheus-grafana-69f9ccfd8d-svcpn                      3/3     Running   0          76s
prometheus-kube-prometheus-operator-6f4fc4dcbd-gh2t2     1/1     Running   0          76s
prometheus-kube-state-metrics-57c8464f66-487vm           1/1     Running   0          76s
prometheus-prometheus-kube-prometheus-prometheus-0       2/2     Running   0          61s
prometheus-prometheus-node-exporter-wkn2w                1/1     Running   0          76s
```

```bash
$ kubectl get po -n keda
NAME                                               READY   STATUS    RESTARTS      AGE
keda-admission-webhooks-685d94fcff-krrgd           1/1     Running   0             57s
keda-operator-65f5568c7b-rghfz                     1/1     Running   1 (43s ago)   57s
keda-operator-metrics-apiserver-69c577c9cf-6bp5k   1/1     Running   0             57s
```

#### Install KEDA autoscaler

```bash
$ ko apply -f config/
```

```bash
$ kubectl get po -n knative-serving             
NAME                                      READY   STATUS    RESTARTS   AGE
activator-d66fd5dd8-z2rt8                 1/1     Running   0          12m
autoscaler-6c7bf97997-ds4g9               1/1     Running   0          12m
autoscaler-keda-5b5576c47b-gcjsf          1/1     Running   0          10m
controller-5b54cd98c-dmht8                1/1     Running   0          12m
net-kourier-controller-5db85876d8-nvcj5   1/1     Running   0          12m
webhook-56ffd84996-65qb2                  1/1     Running   0          12m
```

#### Run a ksvc with Keda HPA support

Apply the [service.yaml](./test/test_images/metrics-test/service.yaml) and wait for the service to be ready.

```bash
$ ko apply -f ./test/test_images/metrics-test/service.yaml
```

Check the `ksvc` and the created `scaledobject`:
```bash
$ kubectl get ksvc
NAME           URL                                       LATESTCREATED        LATESTREADY          READY   REASON
metrics-test   http://metrics-test.default.example.com   metrics-test-00001   metrics-test-00001   True

$ kubectl get scaledobject
NAME                 SCALETARGETKIND      SCALETARGETNAME                 MIN   MAX   READY   ACTIVE   FALLBACK   PAUSED    TRIGGERS   AUTHENTICATIONS   AGE
metrics-test-00001   apps/v1.Deployment   metrics-test-00001-deployment   1     10    True    False    False      Unknown                                3m32s

$ kubectl get hpa         
NAME                 REFERENCE                                  TARGETS     MINPODS   MAXPODS   REPLICAS   AGE
metrics-test-00001   Deployment/metrics-test-00001-deployment   0/5 (avg)   1         10        1          5m27s
```

**NOTE:** as there is no traffic, the metric reported is `0`.

#### Test your deployment

Enable tunnelling with minikube:

```bash
$ minikube tunnel
```

Get the `kourier` service external-ip:

```bash
$ kubectl get svc kourier -n kourier-system
NAME      TYPE           CLUSTER-IP      EXTERNAL-IP   PORT(S)                      AGE
kourier   LoadBalancer   10.110.53.103   127.0.0.1     80:30168/TCP,443:30836/TCP   16m
```

Let's create some traffic:

```bash 
for i in {1..100000}; do curl -H "Host: metrics-test.default.example.com " http://127.0.0.1:80; done
```

The `ksvc` will scale based on the prometheus metric. 

```bash

$ kubectl get hpa
NAME                 REFERENCE                                  TARGETS         MINPODS   MAXPODS   REPLICAS   AGE
metrics-test-00001   Deployment/metrics-test-00001-deployment   8191m/5 (avg)   1         10        10         8m17s

$ kubectl get po -n default -l app=metrics-test
NAME                                             READY   STATUS    RESTARTS   AGE
metrics-test-00001-deployment-554cfbdcdc-5dmck   2/2     Running   0          58s
metrics-test-00001-deployment-554cfbdcdc-6njbs   2/2     Running   0          88s
metrics-test-00001-deployment-554cfbdcdc-6xg4p   2/2     Running   0          43s
metrics-test-00001-deployment-554cfbdcdc-7ts2b   2/2     Running   0          73s
metrics-test-00001-deployment-554cfbdcdc-8z2zb   2/2     Running   0          88s
metrics-test-00001-deployment-554cfbdcdc-g5x4v   2/2     Running   0          58s
metrics-test-00001-deployment-554cfbdcdc-gz5c2   2/2     Running   0          8m44s
metrics-test-00001-deployment-554cfbdcdc-hl2h8   2/2     Running   0          58s
metrics-test-00001-deployment-554cfbdcdc-q7nk5   2/2     Running   0          58s
metrics-test-00001-deployment-554cfbdcdc-rv6nx   2/2     Running   0          43s
```


After traffic is sent target replicas are increased until the max value is reached. The reason is that we have set a threshold for scaling to be 5 for that metric, and if we check the peak load, Prometheus reports (`kubectl port-forward -n default svc/prometheus-operated 9090:9090`:

![prom.png](prom.png)

The full configuration is shown next:

```
        autoscaling.knative.dev/minScale: "1"
        autoscaling.knative.dev/maxScale: "10"
        autoscaling.knative.dev/target: "5"
        autoscaling.knative.dev/class: "hpa.autoscaling.knative.dev"
        autoscaling.knative.dev/metric: "http_requests_total"
        autoscaling.knative.dev/query: "sum(rate(http_requests_total{}[1m]))"
```

After some cooldown period replicas are terminated back to 1 (see [keda cooldownPeriod](https://keda.sh/docs/2.14/concepts/scaling-deployments/#cooldownperiod)):

```bash
$ kubectl get po -n default -l app=metrics-test
NAME                                             READY   STATUS        RESTARTS   AGE
metrics-test-00001-deployment-554cfbdcdc-5dmck   2/2     Terminating   0          12m
metrics-test-00001-deployment-554cfbdcdc-6njbs   2/2     Terminating   0          12m
metrics-test-00001-deployment-554cfbdcdc-6xg4p   2/2     Terminating   0          12m
metrics-test-00001-deployment-554cfbdcdc-7ts2b   1/2     Terminating   0          12m
metrics-test-00001-deployment-554cfbdcdc-8z2zb   2/2     Terminating   0          12m
metrics-test-00001-deployment-554cfbdcdc-g5x4v   2/2     Terminating   0          12m
metrics-test-00001-deployment-554cfbdcdc-gz5c2   2/2     Running       0          20m
metrics-test-00001-deployment-554cfbdcdc-hl2h8   2/2     Terminating   0          12m
metrics-test-00001-deployment-554cfbdcdc-q7nk5   2/2     Terminating   0          12m
metrics-test-00001-deployment-554cfbdcdc-rv6nx   1/2     Terminating   0          12m
```

### Bring Your Own ScaledObject

You can bring your own ScaledObject by either disabling globally the auto-creation mode or by setting the `autoscaling.knative.dev/scaled-object-auto-create` annotation to `false` in the service (`spec.template.metadata` field).

Then at any time you can create the ScaledObject with the desired configuration as follows:

```yaml
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  annotations:
    "autoscaling.knative.dev/class": "hpa.autoscaling.knative.dev"
  name: metrics-test-00001
  namespace: test
spec:
  advanced:
    horizontalPodAutoscalerConfig:
      name: metrics-test-00001
    scalingModifiers: {}
  maxReplicaCount: 10
  minReplicaCount: 1
  scaleTargetRef:
    name: metrics-test-00001-deployment
  triggers:
    - metadata:
        namespace: test
        query: sum(rate(http_requests_total{namespace="test"}[1m]))
        serverAddress: http://prometheus-operated.default.svc:9090
        threshold: "5"
      type: prometheus
```

The annotation for the scaling class is required so reconciliation is triggered for the HPA that is generated by KEDA.
