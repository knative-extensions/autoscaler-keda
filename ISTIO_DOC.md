# Istio Setup 

## Simple-Mode

In simple-mode, `istio` acts only as the ingress to `knative`. 

Install `istio`:

```bash
$ kubectl apply -f https://github.com/knative/net-istio/releases/download/knative-v1.16.0/istio.yaml
$ kubectl apply -f https://github.com/knative/net-istio/releases/download/knative-v1.16.0/net-istio.yaml
```

Check that all pods are in a `Running` state:

```bash
NAME                                   READY   STATUS    RESTARTS   AGE
activator-d66fd5dd8-9phqr              1/1     Running   0          15m
autoscaler-6c7bf97997-rkd2n            1/1     Running   0          15m
autoscaler-keda-7f87794cb7-g2wm8       1/1     Running   0          6m32s
controller-5b54cd98c-bzgpt             1/1     Running   0          15m
net-istio-controller-c9444c8ff-rwcp5   1/1     Running   0          11m
net-istio-webhook-66b6b6444c-zpxjg     1/1     Running   0          11m
webhook-56ffd84996-ck5tm               1/1     Running   0          15m
```

The testing reported in the [development instructions](./DEVELOPMENT.md) was conducted also for `istio` in simple-mode. 
If mesh-mode is not required, continue the setup as documented in the main instructions. 

## Mesh-Mode 

Make sure to review the section of the `knative` documentation about installing istio in mesh-mode: [Knative installation doc](https://knative.dev/docs/admin/install/serving/install-serving-with-yaml/#install-a-networking-layer).

Brief instructions are reported below for developing this extension.

Install `istio`:

```bash
$ kubectl apply -f https://github.com/knative/net-istio/releases/download/knative-v1.16.0/istio.yaml
$ kubectl apply -f https://github.com/knative/net-istio/releases/download/knative-v1.16.0/net-istio.yaml
```

### Add knative serving to the mesh

Enable `istio-injection` in the `knative-serving` namespace:

```bash
$ kubectl label namespace knative-serving istio-injection=enabled
```

Restart all deployments in the `knative-serving` namespace:

```bash
$ kubectl rollout restart deploy -n knative-serving
```

Verify that the pods are now injected with the `istio` sidecar container (note the number of `READY` containers is `2`):

```bash
kubectl get po -n knative-serving
NAME                                    READY   STATUS    RESTARTS      AGE
activator-5754bdb79d-9gpn5              2/2     Running   0             91s
autoscaler-57d66f69d9-cmv6j             2/2     Running   0             91s
controller-f48959855-lhrwz              2/2     Running   1 (90s ago)   91s
net-istio-controller-69858b66f7-qfnq8   1/1     Running   0             91s
net-istio-webhook-5645569675-jvj2r      2/2     Running   0             91s
webhook-778946f8-ftjps                  2/2     Running   0             91s
```

### Configure knative domain 

```bash
$ kubectl patch configmap/config-domain \
      --namespace knative-serving \
      --type merge \
      --patch '{"data":{"example.com":""}}'
```

### Install Prometheus and KEDA

In `PERMISSIVE` mode (under the `knative-serving` namespace), clear-text traffic and encrypted traffic are both allowed, with the latter preferred by `istio` when properly configured. The following instructions will not enable Mutual TLS between `knative`, `keda` and `prometheus`. For enabling `STRICT` mode, follow the instructions in the [STRICT mode section](#strict-mode) below.

Install `keda` and `prometheus` in their own namespaces:
```bash
$ helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
$ helm repo add kedacore https://kedacore.github.io/charts
$ helm repo update

$ helm install prometheus prometheus-community/kube-prometheus-stack -f values.yaml --namespace prometheus --create-namespace
$ helm install keda kedacore/keda --namespace keda --create-namespace
```

Check that all pods are in a `Running` state:

```bash
$ kubectl get po -n prometheus
kubectl get po -n prometheus
NAME                                                     READY   STATUS    RESTARTS   AGE
alertmanager-prometheus-kube-prometheus-alertmanager-0   2/2     Running   0          75s
prometheus-grafana-69f9ccfd8d-72xb8                      3/3     Running   0          87s
prometheus-kube-prometheus-operator-6f4fc4dcbd-2hzqm     1/1     Running   0          87s
prometheus-kube-state-metrics-57c8464f66-7h25x           1/1     Running   0          87s
prometheus-prometheus-kube-prometheus-prometheus-0       2/2     Running   0          75s
prometheus-prometheus-node-exporter-h7gp6                1/1     Running   0          87s
```

```bash
$ kubectl get po -n keda
kubectl get po -n keda
NAME                                               READY   STATUS    RESTARTS      AGE
keda-admission-webhooks-685d94fcff-kjvlw           1/1     Running   0             80s
keda-operator-65f5568c7b-9kbc8                     1/1     Running   1 (73s ago)   80s
keda-operator-metrics-apiserver-69c577c9cf-qxxhx   1/1     Running   0             80s
```

### Install KEDA autoscaler

```bash
$ ko apply -f config/
```

Patch the `config-autoscaler-keda` configmap to reflect the `prometheus` service in its own namespace:

```bash
$ kubectl patch configmap/config-autoscaler-keda -n knative-serving --type merge -p '{"data": { "autoscaler.keda.prometheus-address": "http://prometheus-operated.prometheus.svc:9090"}}'
```

### Run a ksvc with Keda HPA support in the istio mesh

```bash
$ ko apply -f ./test/test_images/metrics-test/service_istio_injected.yaml -- -n metrics-test-istio
```

This will create a new namespace `metrics-test-istio` with istio injection enabled.

```bash
$ kubectl get po -n metrics-test-istio
NAME                                                   READY   STATUS    RESTARTS   AGE
metrics-test-istio-00001-deployment-7d57bbb8d8-86zsg   3/3     Running   0          77s
```

To generate traffic and test the deployment follow the [development instructions](./DEVELOPMENT.md). The service will be accessible at `http://metrics-test-istio.metrics-test-istio.example.com`.

### STRICT mode

`STRICT` mode only allows for mutual TLS traffic. To enable this mode some additional configuration are required.

#### Optional: Install Kiali

`Kiali` is the monitoring tool for `istio`.

```bash
kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.22/samples/addons/kiali.yaml
```

Under the ns `istio-system` edit the `kiali` config and add under the key `config.yaml`:

```yaml
external_services: # this value will be there already
  prometheus:
    url: "http://prometheus-operated.prometheus.svc:9090/"
```

To enable `kiali` to monitor the various components in the service mesh the service monitors need to be deployed:

```bash
kubectl apply -f ./test/test_images/metrics-test/service_monitors.yaml
```

Verify your `kiali` installation by generating traffic to the previously deployed service and check the `Traffic Graph` page. 

#### Setup PeerAuthentication

Run:

```bash
kubectl apply -f - <<EOF
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
  namespace: istio-system
spec:
  mtls:
    mode: STRICT
EOF
```

At this point, traffic to the service will flow, but no scaling will occur as `prometheus` is unable to scrape metrics.

`STRICT` mode is already enabled at mesh-level (see: [Istio doc](https://istio.io/latest/docs/reference/config/security/peer_authentication/)). 
The following command will specify the configuration in the `knative-serving` namespace, allowing scraping of metrics on yhe `9090` port.

```bash
kubectl apply -f - <<EOF
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
  namespace: knative-serving
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: knative-serving
  mtls:
    mode: STRICT
  portLevelMtls:
    9090:
      mode: PERMISSIVE
EOF
```

At this point, the service still won't scale. To enable metrics scraping, we need to target port `9096` on the specific namespace:

```bash
kubectl apply -f - <<EOF
apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: default
  namespace: metrics-test-istio
spec:
  selector:
    matchLabels:
      app: metrics-test-istio
  mtls:
    mode: STRICT
  portLevelMtls:
    9096:
      mode: DISABLE
EOF
```

When generating traffic, the service will scale. `Prometheus` will also show healthy targets and the `http_request_total` metric will be available.

**NOTE:** `label-selectors` are needed in the `PeerAuthentication` as `portLevelMTLS` is considered only in that case. 