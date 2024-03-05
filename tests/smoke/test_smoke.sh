#!/bin/bash

# Define the namespace
NAMESPACE="test-1"

# Define the path to the CRD
CRD_PATH="./tests/smoke/manifests/trustyai-cr.yaml"

# Define the expected names
PVC_NAME="trustyai-service-pvc"
SERVICE_NAME_1="trustyai-service"
SERVICE_NAME_2="trustyai-service-tls"

# Apply the CRD
kubectl create namespace "$NAMESPACE"
kubectl apply -f "$CRD_PATH" -n "$NAMESPACE"

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

sleep 10

# Check for the creation of services
check_resource service "$SERVICE_NAME_1" "$NAMESPACE"
check_resource service "$SERVICE_NAME_2" "$NAMESPACE"

# Check for the PVC creation
check_resource pvc "$PVC_NAME" "$NAMESPACE"

kubectl delete namespace "$NAMESPACE"

sleep 10

if kubectl get namespace "$NAMESPACE" &> /dev/null; then
    log_failure "Namespace $NAMESPACE was not deleted successfully"
else
    log_success "Namespace $NAMESPACE has been deleted successfully"
fi

echo "All tests passed successfully."
