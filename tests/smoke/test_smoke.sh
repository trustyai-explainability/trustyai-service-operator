#!/bin/bash

# Define the namespace
NAMESPACE=test-1

# Define the path to the CRD
CRD_PATH="./tests/smoke/manifests/trustyai-cr.yaml"

# Define the expected names
PVC_NAME="trustyai-service-pvc"
SERVICE_NAME_1="trustyai-service"
SERVICE_NAME_2="trustyai-service-tls"

# Apply the CRD
kubectl apply -f "$CRD_PATH"

# Function to check resource existence
check_resource() {
    local resource_type=$1
    local resource_name=$2
    local namespace=$3

    if ! kubectl get "$resource_type" -n "$namespace" | grep -q "$resource_name"; then
        echo "❌ Failed to find $resource_type: $resource_name in namespace $namespace"
        exit 1
    else
        echo "✅ $resource_type: $resource_name found in namespace $namespace"
    fi
}

sleep 10

# Check for the creation of services
check_resource service "$SERVICE_NAME_1" "$NAMESPACE"
check_resource service "$SERVICE_NAME_2" "$NAMESPACE"

# Check for the PVC creation
check_resource pvc "$PVC_NAME" "$NAMESPACE"

echo "All tests passed successfully."
