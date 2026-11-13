#!/bin/bash

# Serving-free and adoption legs for AI Safety.
#
# The TrustyAIService has to reconcile on a cluster where the InferenceService
# CRD has never been installed, keep reporting that state through a minute of
# requeues without restarting, and adopt the CRD when it appears later without a
# restart or a redeploy.
#
# Ready is asserted for stability rather than for True: IsAllReady includes
# RouteReady, and a Kind cluster has no OpenShift router, so the operand can
# never report ready here whatever serving does. The claim that serving's
# absence does not affect readiness is what matters, and it is asserted by
# requiring the Ready value to survive every leg unchanged. Ready=True with no
# serving CRD is asserted in the controller suite instead
# (controllers/tas/serving_ready_test.go), which drives route, PVC and
# deployment readiness directly.
#
# test_smoke.sh creates and deletes its own namespace in the same run, so these
# legs bring their own rather than depend on another script's leftovers.

set -uo pipefail

NAMESPACE="${SERVING_FREE_NAMESPACE:-serving-free}"
TAS="trustyai-service"
CR_MANIFEST="./tests/smoke/manifests/trustyai-cr.yaml"
SERVING_CRD="./tests/crds/serving.kserve.io_inferenceservices.yaml"
WORKLOAD_NAMESPACE="${WORKLOAD_NAMESPACE:-system}"
WORKLOAD_LABEL="control-plane=trustyai-service-operator"

fail() {
    echo "❌ $1"
    kubectl get trustyaiservice "$TAS" -n "$NAMESPACE" -o yaml || true
    kubectl logs -n "$WORKLOAD_NAMESPACE" -l "$WORKLOAD_LABEL" --tail=80 || true
    exit 1
}

restarts() {
    kubectl get pods -n "$WORKLOAD_NAMESPACE" -l "$WORKLOAD_LABEL" \
        -o jsonpath='{.items[*].status.containerStatuses[*].restartCount}' | tr -d ' '
}

# condition prints one field of one status condition, or nothing when the
# condition or the object is not there yet.
condition() {
    kubectl get trustyaiservice "$TAS" -n "$NAMESPACE" \
        -o jsonpath="{.status.conditions[?(@.type==\"$1\")].$2}" 2>/dev/null || true
}

# await_reason waits for the InferenceServicesPresent reason to become $1.
await_reason() {
    for _ in $(seq 1 "${2:-60}"); do
        [ "$(condition InferenceServicesPresent reason)" = "$1" ] && return 0
        sleep 1
    done
    return 1
}

cleanup() {
    kubectl delete namespace "$NAMESPACE" --wait=false >/dev/null 2>&1 || true
}
trap cleanup EXIT

# The operand needs its TLS material before the CR is applied, exactly as
# test_smoke.sh seeds it.
kubectl create namespace "$NAMESPACE"
openssl req -x509 -nodes -newkey rsa:2048 -keyout /tmp/tas.key -out /tmp/tas.crt \
    -days 1 -subj '/CN=trustyai-service-tls' 2>/dev/null
kubectl create secret generic trustyai-service-internal \
    --from-file=/tmp/tas.crt --from-file=/tmp/tas.key -n "$NAMESPACE"
kubectl create secret generic trustyai-service-tls \
    --from-file=/tmp/tas.crt --from-file=/tmp/tas.key -n "$NAMESPACE"
kubectl apply -f "$CR_MANIFEST" -n "$NAMESPACE"

# --- leg 1: no serving CRD ---------------------------------------------------
# Wait for a serving verdict rather than for Ready: readiness is bounded by the
# operand and by the router, the serving condition is not.
echo "waiting for a serving verdict on a serving-free cluster"
for _ in $(seq 1 180); do
    [ -n "$(condition InferenceServicesPresent reason)" ] && break
    sleep 1
done
[ "$(condition InferenceServicesPresent status)" = "False" ] \
    || fail "InferenceServicesPresent must be False with no serving CRD"
[ "$(condition InferenceServicesPresent reason)" = "ServingCRDNotFound" ] \
    || fail "expected reason ServingCRDNotFound, got '$(condition InferenceServicesPresent reason)'"
echo "✅ serving-free: $(condition InferenceServicesPresent message)"

# Absence is a supported configuration, not a restart loop: hold the observation
# for a minute of requeues and require the operator and its verdict to be
# unchanged.
before=$(restarts)
ready_before=$(condition Ready status)
sleep 60
[ "$before" = "$(restarts)" ] \
    || fail "the workload operator restarted without a serving CRD: $before -> $(restarts)"
[ "$(condition InferenceServicesPresent reason)" = "ServingCRDNotFound" ] \
    || fail "the serving verdict changed while serving stayed absent: '$(condition InferenceServicesPresent reason)'"
[ "$ready_before" = "$(condition Ready status)" ] \
    || fail "Ready changed (${ready_before} -> $(condition Ready status)) without a cause"
echo "✅ no restart and no change over a minute of requeues"

# --- leg 2: serving appears --------------------------------------------------
echo "installing the serving CRD"
kubectl apply -f "$SERVING_CRD"
await_reason NoInferenceServices 60 \
    || fail "serving adoption did not take effect; reason is '$(condition InferenceServicesPresent reason)'"
[ "$before" = "$(restarts)" ] \
    || fail "the workload operator restarted when serving appeared: $before -> $(restarts)"
[ "$ready_before" = "$(condition Ready status)" ] \
    || fail "Ready changed (${ready_before} -> $(condition Ready status)) when serving appeared"
# The accepted version is named in the message, which is how an administrator
# tells "serving is usable" from "serving is installed but unusable".
case "$(condition InferenceServicesPresent message)" in
    *serving.kserve.io/v1beta1*) ;;
    *) fail "the condition message does not name the adopted version: '$(condition InferenceServicesPresent message)'" ;;
esac
echo "✅ adopted serving without a restart: $(condition InferenceServicesPresent message)"

# --- leg 3: serving is removed under the running operator ---------------------
# The CRD can also go away from under a live operator, which is what the RFE
# asks about. Nothing watches serving, so the verdict can only change on the
# next requeue, and the bound is a minute.
echo "removing the serving CRD under the running operator"
kubectl delete crd inferenceservices.serving.kserve.io
await_reason ServingCRDNotFound 60 \
    || fail "removing serving did not take effect; reason is '$(condition InferenceServicesPresent reason)'"
[ "$before" = "$(restarts)" ] \
    || fail "the workload operator restarted when serving was removed: $before -> $(restarts)"
[ "$ready_before" = "$(condition Ready status)" ] \
    || fail "Ready changed (${ready_before} -> $(condition Ready status)) when serving was removed"
case "$(condition InferenceServicesPresent message)" in
    *inferenceservices.serving.kserve.io*) ;;
    *) fail "the condition message does not name the missing CRD: '$(condition InferenceServicesPresent message)'" ;;
esac
echo "✅ survived serving removal: $(condition InferenceServicesPresent message)"

# Re-adding it takes effect on the next pass too, so adoption above is not a
# one-shot: an administrator can install, remove and reinstall serving without a
# restart or a redeploy of AI Safety.
echo "reinstalling the serving CRD"
kubectl apply -f "$SERVING_CRD"
await_reason NoInferenceServices 60 \
    || fail "re-adoption did not take effect; reason is '$(condition InferenceServicesPresent reason)'"
[ "$before" = "$(restarts)" ] \
    || fail "the workload operator restarted when serving came back: $before -> $(restarts)"
[ "$ready_before" = "$(condition Ready status)" ] \
    || fail "Ready changed (${ready_before} -> $(condition Ready status)) when serving came back"
echo "✅ re-adopted serving without a restart: $(condition InferenceServicesPresent message)"
