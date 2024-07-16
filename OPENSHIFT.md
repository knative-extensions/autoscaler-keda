
# Instructions

## Install the Serverless Operator
Install serverless operator from the repo https://github.com/openshift-knative/serverless-operator

```bash

export DOCKER_REPO_OVERRIDE=...

git clone https://github.com/openshift-knative/serverless-operator

cd serverless-operator

# disable the default hpa autoscaler
rm openshift-knative-operator/cmd/operator/kodata/knative-serving/1.14/3-serving-hpa.yaml

make images; make install-serving
```

Verify the setup:

```bash
oc get po -n openshift-serverless
NAME                                         READY   STATUS    RESTARTS   AGE
knative-openshift-7944d6844d-9rmrb           1/1     Running   0          21m
knative-openshift-ingress-65f54bf65b-hp6fd   1/1     Running   0          21m
knative-operator-webhook-776b45658f-49g6x    1/1     Running   0          21m

oc get po -n openshift-serverless
NAME                               READY   STATUS    RESTARTS   AGE
activator-6f6bb6db7b-pbnsv         2/2     Running   0          21m
activator-6f6bb6db7b-qgvdv         2/2     Running   0          21m
autoscaler-7c4f944c6-kds66         2/2     Running   0          21m
autoscaler-7c4f944c6-m6skz         2/2     Running   0          21m
controller-6556dfd44f-j8m9j        2/2     Running   0          21m
controller-6556dfd44f-t26ht        2/2     Running   0          21m
webhook-985b5dbf5-l8k5d            2/2     Running   0          21m
webhook-985b5dbf5-mc549            2/2     Running   0          21m

```

## Install the Custom Metrics Autoscaler

Install the Custom Metrics Autoscaler (KEDA) by applying the following:

```yaml
---
apiVersion: v1
kind: Namespace
metadata:
  name: openshift-keda
---
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: keda
  namespace: openshift-keda
spec: {}
---
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: openshift-custom-metrics-autoscaler-operator
  namespace: openshift-keda
spec:
  channel: stable
  installPlanApproval: Automatic
  name: openshift-custom-metrics-autoscaler-operator
  source: redhat-operators
  sourceNamespace: openshift-marketplace

```

Create a KEDA controller in openshift-keda ns by applying the following:

```yaml
apiVersion: keda.sh/v1alpha1
kind: KedaController
metadata:
  name: keda
  namespace: openshift-keda
spec:
  admissionWebhooks:
    logEncoder: console
    logLevel: info
  metricsServer:
    logLevel: '0'
  operator:
    logEncoder: console
    logLevel: info
  watchNamespace: ''
```

Verify all pods are up:

```bash
oc get po -n openshift-keda
NAME                                                  READY   STATUS    RESTARTS   AGE
custom-metrics-autoscaler-operator-7b8f4599c7-g6s8x   1/1     Running   0          16m
keda-admission-86df47c77-9hmlv                        1/1     Running   0          11m
keda-metrics-apiserver-565b8cd678-r8cts               1/1     Running   0          11m
keda-operator-59b778498c-tdxcc                        1/1     Running   0          11m

```

## Install the Autoscaler Keda

```bash
# cd at the top dir of this repo and install the autoscaler Keda component
ko apply -f ./config

oc get po -n knative-serving
...
autoscaler-keda-85b9d88698-7qh68   1/1     Running   0          12m
```

## Setup the KEDA TriggerAuthentication for connecting to Thanos

```bash
oc create ns test
oc create serviceaccount thanos -n test
```

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: thanos-metrics-reader
rules:
- apiGroups:
  - ""
  resources:
  - pods
  verbs:
  - get
- apiGroups:
  - metrics.k8s.io
  resources:
  - pods
  - nodes
  verbs:
  - get
  - list
  - watch
```

```bash
# alternatively this can be done with a rolebinding
oc adm policy add-role-to-user thanos-metrics-reader -z thanos --role-namespace=test -n test

oc get secret -n test | grep  thanos-token | head -n 1 | awk '{print $1 }'
thanos-token-bp2l8
```

```yaml
apiVersion: keda.sh/v1alpha1
kind: TriggerAuthentication
metadata:
  name: keda-trigger-auth-prometheus
spec:
  secretTargetRef:
  - parameter: bearerToken
    name: thanos-token-bp2l8
    key: token
  - parameter: ca
    name: thanos-token-bp2l8
    key: ca.crt

``` 

## Integrating with the OCP Cluster Monitoring

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: knative-prometheus-k8s
rules:
- apiGroups:
  - ""
  resources:
  - services
  - endpoints
  - pods
  verbs:
  - get
  - list
  - watch
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: knative-prometheus-k8s
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: knative-prometheus-k8s
subjects:
- kind: ServiceAccount
  name: prometheus-k8s
  namespace: openshift-monitoring
```

```bash
oc label namespace test openshift.io/cluster-monitoring=true
```

## Integrating with the OCP User Workload Monitoring

Edit the cm:
```bash
oc -n openshift-monitoring edit configmap cluster-monitoring-config
```

Add:

```
apiVersion: v1
data:
  config.yaml: |-
    enableUserWorkload: true
```

```bash
oc -n openshift-user-workload-monitoring get pod
NAME                                   READY   STATUS    RESTARTS   AGE
prometheus-operator-5c69ff6449-g4jdk   2/2     Running   0          10m
prometheus-user-workload-0             6/6     Running   0          9m55s
prometheus-user-workload-1             6/6     Running   0          9m55s
thanos-ruler-user-workload-0           4/4     Running   0          9m55s
thanos-ruler-user-workload-1           4/4     Running   0          9m55s
```

## Deploy the sample service

Add the following annotations to the sample ksvc and then apply it

```bash
# autoscaling.knative.dev/prometheus-address: "https://thanos-querier.openshift-monitoring.svc.cluster.local:9092"
# autoscaling.knative.dev/trigger-prometheus-auth-name: "keda-trigger-auth-prometheus"
# autoscaling.knative.dev/trigger-prometheus-auth-modes: "bearer"
# autoscaling.knative.dev/trigger-prometheus-auth-kind: "TriggerAuthentication"
ko apply -f ./test/test_images/metrics-test/service.yaml -- --namespace=test

oc get ksvc -n test
NAME           URL                                     LATESTCREATED        LATESTREADY          READY   REASON
metrics-test   https://metrics-test-test.apps...       metrics-test-00001   metrics-test-00001   True    

oc get po -n test
NAME                                             READY   STATUS    RESTARTS   AGE
metrics-test-00001-deployment-7f8995d5fc-zg6qg   2/2     Running   0          10m

```

If you want to use a ClusterTriggerAuthentication you just need to chaneg the kind in the annotation
to be ClusterTriggerAuthentication. Config should be similar to the following:

```yaml

apiVersion: keda.sh/v1alpha1
kind: ClusterTriggerAuthentication
metadata:
name: keda-trigger-auth-prometheus
spec:
secretTargetRef:
- parameter: bearerToken
  name: thanos-token-h9mch
  key: token
- parameter: ca
  name: thanos-token-h9mch
  key: ca.crt
```
The thanos sa needs to be created in the openshift-keda ns and instead of a role binding a cluster role binding should be created.

If everything is setup correctly you should see the following:

```bash
oc get scaledobject metrics-test-00001 -n test -ojson | jq .status.conditions
[
  {
    "message": "ScaledObject is defined correctly and is ready for scaling",
    "reason": "ScaledObjectReady",
    "status": "True",
    "type": "Ready"
  },
...
```

```bash

On one terminal apply some load:
```bash
hey -z 100s  -c 10  https://metrics-test-test.apps...
```

Observe the autoscaling in action:
```bash
oc get po -n test
NAME                                            READY   STATUS    RESTARTS   AGE
metrics-test-00001-deployment-c44746b84-ft5xs   2/2     Running   0          4m42s
metrics-test-00001-deployment-c44746b84-j928h   2/2     Running   0          27s
metrics-test-00001-deployment-c44746b84-p9tsf   2/2     Running   0          3m42s
metrics-test-00001-deployment-c44746b84-pxg94   2/2     Running   0          12s
metrics-test-00001-deployment-c44746b84-vw4xz   2/2     Running   0          11s
metrics-test-00001-deployment-c44746b84-wr6sl   2/2     Running   0          11s

oc get hpa -n test
NAME                 REFERENCE                                  TARGETS          MINPODS   MAXPODS   REPLICAS   AGE
metrics-test-00001   Deployment/metrics-test-00001-deployment   10608m/2 (avg)   1         10        3          4m45s

```