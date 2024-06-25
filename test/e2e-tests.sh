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
cd serving
rm ./test/config/chaosduck/chaosduck.yaml

# Disable hpa manifests
sed -e '/SERVING_HPA_YAML/ s/^#*/#/' -i ./test/e2e-common.sh
sed -e '/\/serving-hpa.yaml/ s/^#*/#/' -i ./test/e2e-common.sh
sed -e '/serving hpa file/ s/^#*/#/' -i ./test/e2e-common.sh
sed -e 's/{REPO_ROOT_DIR}/{REPO_ROOT_DIR}\/serving/g' -i ./test/e2e-common.sh
sed -e 's/{REPO_ROOT_DIR}/{REPO_ROOT_DIR}\/serving/g' -i ./test/e2e-networking-library.sh

source ./test/e2e-common.sh

knative_setup

echo ">> Uploading test images..."
ko resolve --jobs=4 -RBf ./test/test_images/autoscale > /dev/null

# Setup Helm - TODO move to the infra image

wget https://get.helm.sh/helm-v3.15.2-linux-amd64.tar.gz
tar -zxvf helm-v3.15.2-linux-amd64.tar.gz
mv linux-amd64/helm "${HELM_BIN}"

# Add prometheus and KEDA helm repos
"${HELM_BIN}" repo add prometheus-community https://prometheus-community.github.io/helm-charts
"${HELM_BIN}" repo add kedacore https://kedacore.github.io/charts
"${HELM_BIN}" repo update

# Install Prometheus-community
"${HELM_BIN}" install prometheus prometheus-community/kube-prometheus-stack -n default -f ../values.yaml
kubectl wait deployment.apps/prometheus-grafana --for condition=available --timeout=600s
kubectl wait deployment.apps/prometheus-kube-prometheus-operator --for condition=available --timeout=600s
kubectl wait deployment.apps/prometheus-kube-state-metrics --for condition=available --timeout=600s

# Install KEDA
"${HELM_BIN}" install keda kedacore/keda --namespace "${KEDA_NS}" --create-namespace
kubectl wait deployment.apps/keda-admission-webhooks -n "${KEDA_NS}" --for condition=available --timeout=600s
kubectl wait deployment.apps/keda-operator -n "${KEDA_NS}" --for condition=available --timeout=600s
kubectl wait deployment.apps/keda-operator-metrics-apiserver -n "${KEDA_NS}" --for condition=available --timeout=600s

#Setup Autoscaler KEDA
cd ..
ko resolve -f ./config  | sed "s/namespace: knative-serving/namespace: ${SYSTEM_NAMESPACE}/" | kubectl apply -f-

# Wait for the Autoscaler KEDA deployment to be available
kubectl wait deployments.apps/autoscaler-keda -n "${SYSTEM_NAMESPACE}" --for condition=available --timeout=600s

# Run the HPA tests
header "Running HPA tests"
cd serving

go_test_e2e -timeout=30m -tags=hpa ./test/e2e "${E2E_TEST_FLAGS[@]}" || failed=1

(( failed )) && fail_test

# Remove the kail log file if the test flow passes.
# This is for preventing too many large log files to be uploaded to GCS in CI.
rm "${ARTIFACTS}/k8s.log-$(basename "${E2E_SCRIPT}").txt"

success
