#!/usr/bin/env bash

# Copyright 2024 The Knative Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -x
failed=0
source $(dirname $0)/../vendor/knative.dev/hack/e2e-tests.sh

HELM_BIN="/tmp/helm"
KEDA_NS="keda"

initialize --num-nodes=4 --cluster-version=1.28 "$@"

git clone https://github.com/knative/serving.git "serving"
pushd serving
rm ./test/config/chaosduck/chaosduck.yaml

# Disable HPA manifests
sed -e '/SERVING_HPA_YAML/ s/^#*/#/' -i ./test/e2e-common.sh
sed -e '/\/serving-hpa.yaml/ s/^#*/#/' -i ./test/e2e-common.sh
sed -e '/serving hpa file/ s/^#*/#/' -i ./test/e2e-common.sh

# Update top dir in checked out repo
sed -e 's/{REPO_ROOT_DIR}/{REPO_ROOT_DIR}\/serving/g' -i ./test/e2e-common.sh
sed -e 's/{REPO_ROOT_DIR}/{REPO_ROOT_DIR}\/serving/g' -i ./test/e2e-networking-library.sh

# Remove skip condition, run the mem test too
sed -e '/11944/d' -i ./test/e2e/autoscale_hpa_test.go

source ./test/e2e-common.sh

knative_setup

echo ">> Uploading test images..."
./test/upload-test-images.sh
popd

# Setup Helm - TODO move to the infra image

wget https://get.helm.sh/helm-v3.15.2-linux-amd64.tar.gz
tar -zxvf helm-v3.15.2-linux-amd64.tar.gz
mv linux-amd64/helm "${HELM_BIN}"

# Add prometheus and KEDA helm repos
"${HELM_BIN}" repo add prometheus-community https://prometheus-community.github.io/helm-charts
"${HELM_BIN}" repo add kedacore https://kedacore.github.io/charts
"${HELM_BIN}" repo update

# Install Prometheus-community
"${HELM_BIN}" install prometheus prometheus-community/kube-prometheus-stack -n default -f values.yaml
kubectl wait deployment.apps/prometheus-grafana --for condition=available --timeout=600s
kubectl wait deployment.apps/prometheus-kube-prometheus-operator --for condition=available --timeout=600s
kubectl wait deployment.apps/prometheus-kube-state-metrics --for condition=available --timeout=600s

# Install KEDA
"${HELM_BIN}" install keda kedacore/keda --namespace "${KEDA_NS}" --create-namespace
kubectl wait deployment.apps/keda-admission-webhooks -n "${KEDA_NS}" --for condition=available --timeout=600s
kubectl wait deployment.apps/keda-operator -n "${KEDA_NS}" --for condition=available --timeout=600s
kubectl wait deployment.apps/keda-operator-metrics-apiserver -n "${KEDA_NS}" --for condition=available --timeout=600s

#Setup Autoscaler KEDA
ko resolve -f ./config  | sed "s/namespace: knative-serving/namespace: ${SYSTEM_NAMESPACE}/" | kubectl apply -f-

# Wait for the Autoscaler KEDA deployment to be available
kubectl wait deployments.apps/autoscaler-keda -n "${SYSTEM_NAMESPACE}" --for condition=available --timeout=600s

pushd serving
# Run the HPA tests
header "Running HPA tests"

# Needed for HPA Mem test, see https://keda.sh/docs/2.14/scalers/memory/#prerequisites
toggle_feature queueproxy.resource-defaults "enabled" config-features
go_test_e2e -timeout=30m -tags=hpa ./test/e2e "${E2E_TEST_FLAGS[@]}" || failed=1

git apply ../hack/patches/conformance.patch
# Run conformance tests
# set pod-autoscaler-class to hpa.autoscaling.knative.dev
kubectl patch cm config-autoscaler -n ${SYSTEM_NAMESPACE} -p '{"data":{"pod-autoscaler-class": "hpa.autoscaling.knative.dev"}}'
header "Running conformance tests"
E2E_TEST_FLAGS+=("-enable-alpha" "-enable-beta")
go_test_e2e -timeout=30m \
  "${GO_TEST_FLAGS[@]}" \
  ./test/conformance/api/... \
  ./test/conformance/runtime/... \
  "${E2E_TEST_FLAGS[@]}" || failed=1
git apply -R ../hack/patches/conformance.patch
# restore deault pod-autoscaler-class
kubectl patch cm config-autoscaler -n ${SYSTEM_NAMESPACE} -p '{"data":{"pod-autoscaler-class": "kpa.autoscaling.knative.dev"}}'
popd

# run e2e tests in this repo
header "Running tests in this repo"

echo ">> Uploading e2e test images..."
ko resolve --jobs=4 -RBf ./test/test_images/metrics-test > /dev/null

kubectl apply -f ./test/resources -n serving-tests
go_test_e2e -timeout=30m -tags=e2e ./test/e2e "${E2E_TEST_FLAGS[@]}" || failed=1

(( failed )) && fail_test

success
