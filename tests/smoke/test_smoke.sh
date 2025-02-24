#!/bin/bash

# Define the namespace
NAMESPACE="test-1"

# Define the path to the CRD
CRD_PATH="./tests/smoke/manifests/trustyai-cr.yaml"

# Define the expected names
PVC_NAME="trustyai-service-pvc"
SERVICE_NAME_1="trustyai-service"
SERVICE_NAME_2="trustyai-service-tls"
DEPLOYMENT_NAME="trustyai-service-operator"

EXPECTED_IMAGE="smoke/operator:pr-${{ github.event.pull_request.number || env.PR_NUMBER }}"


log_success() {
    local message=$1
    echo "✅ $message"
}

log_failure() {
    local message=$1
    echo "❌ $message"
    exit 1
}

# Function to check resource existence
check_resource() {
    local resource_type=$1
    local resource_name=$2
    local namespace=$3

    if ! kubectl get "$resource_type" -n "$namespace" | grep -q "$resource_name"; then
        log_failure "Failed to find $resource_type: $resource_name in namespace $namespace"
    else
        log_success "$resource_type: $resource_name found in namespace $namespace"
    fi
}

gen_cert() {
    openssl req -x509 -nodes -newkey rsa:2048 -keyout tls.key -days 365 -out tls.crt -subj '/CN=trustyai-service-tls'
    oc create secret generic trustyai-service-internal  --from-file=./tls.crt --from-file=./tls.key -n "$NAMESPACE"
    oc create secret generic trustyai-service-tls  --from-file=./tls.crt --from-file=./tls.key -n "$NAMESPACE"
    rm tls.key tls.crt
}

# Apply the CRD
kubectl create namespace "$NAMESPACE"
gen_cert
kubectl apply -f "$CRD_PATH" -n "$NAMESPACE"

sleep 10

# Check for the creation of services
check_resource service "$SERVICE_NAME_1" "$NAMESPACE"
check_resource service "$SERVICE_NAME_2" "$NAMESPACE"

# Check for the PVC creation
check_resource pvc "$PVC_NAME" "$NAMESPACE"

# Check for correct Operator image assignment
OPERATOR_IMAGE=$(kubectl get deployment "${DEPLOYMENT_NAME}" -n system \
  -o jsonpath='{.spec.template.spec.containers[0].image}')

if [[ "${OPERATOR_IMAGE}" == "${EXPECTED_IMAGE}" ]]; then
  log_success "Operator image is correct: ${OPERATOR_IMAGE}"
else
  log_failure "Operator image mismatch! Expected ${EXPECTED_IMAGE}, got ${OPERATOR_IMAGE}"
fi

kubectl delete namespace "$NAMESPACE"

sleep 10

if kubectl get namespace "$NAMESPACE" &> /dev/null; then
    log_failure "Namespace $NAMESPACE was not deleted successfully"
else
    log_success "Namespace $NAMESPACE has been deleted successfully"
fi

echo "All tests passed successfully."
