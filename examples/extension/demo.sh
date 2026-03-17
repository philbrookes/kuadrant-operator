#!/usr/bin/env bash
#
# Interactive demo for the RegisterUpstreamMethod feature.
#
# This script demonstrates how the UpstreamPolicy extension registers an
# external gRPC service (Authorino) as an extension-managed upstream, and how
# the operator creates the corresponding Envoy cluster and wasm service entry.
#
# Prerequisites:
#   make local-setup                    # Kind cluster with Istio + Kuadrant
#   make apply-extensions-manifests     # Install extension CRDs
#   make local-apply-extensions         # Build, load, and deploy extensions
#   kubectl apply -f examples/toystore/kuadrant.yaml -n kuadrant-system
#   # Wait for Kuadrant to be ready:
#   kubectl wait kuadrant/kuadrant-sample -n kuadrant-system --for=condition=Ready --timeout=300s
#
# Usage:
#   bash examples/extension/demo.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GATEWAY_NS="gateway-system"
GATEWAY_NAME="kuadrant-ingressgateway"
KUADRANT_NS="kuadrant-system"
EXPECTED_CLUSTER_PREFIX="ext-authorino"

wait_for_input() {
    echo ""
    echo ">>> Press Enter to continue..."
    read -r
    echo ""
}

header() {
    echo ""
    echo "================================================================"
    echo "  $1"
    echo "================================================================"
    echo ""
}

# Get the istio gateway proxy pod name
get_proxy_pod() {
    kubectl get pods -n "${GATEWAY_NS}" -l app=kuadrant-ingressgateway-istio -o jsonpath='{.items[0].metadata.name}' 2>/dev/null
}

# ---------------------------------------------------------------------------
header "RegisterUpstreamMethod Demo"
echo "This demo shows how the UpstreamPolicy extension uses RegisterUpstreamMethod"
echo "to register Authorino's gRPC endpoint as an extension-managed upstream."
echo ""
echo "We will:"
echo "  1. Apply an UpstreamPolicy targeting Authorino's gRPC address"
echo "  2. Verify that an ext- prefixed Envoy cluster is created and healthy"
echo "  3. Apply an AuthPolicy to trigger WasmPlugin creation"
echo "  4. Verify that an ext- prefixed wasm service entry appears in the WasmPlugin"
echo "  5. Clean up everything and verify removal"
wait_for_input

# ---------------------------------------------------------------------------
header "Step 1: Apply the UpstreamPolicy"
echo "The UpstreamPolicy registers Authorino's gRPC endpoint:"
echo "  grpc://authorino-authorino-authorization.kuadrant-system.svc.cluster.local:50051"
echo ""
echo "Applying: examples/extension/upstream-policy.yaml"
kubectl apply -f "${SCRIPT_DIR}/upstream-policy.yaml"
echo ""
echo "Waiting for the UpstreamPolicy to be accepted..."
sleep 5
kubectl get upstreampolicy -n "${KUADRANT_NS}" demo-upstream -o wide 2>/dev/null || true
echo ""
echo "UpstreamPolicy status:"
kubectl get upstreampolicy -n "${KUADRANT_NS}" demo-upstream -o jsonpath='{.status.conditions}' 2>/dev/null | python3 -m json.tool 2>/dev/null || echo "(status not yet available)"
wait_for_input

# ---------------------------------------------------------------------------
header "Step 2: Verify Envoy cluster creation"
echo "The operator should have created an EnvoyFilter with an ext- prefixed cluster"
echo "for the registered upstream."
echo ""
echo "Looking for EnvoyFilters with ext- prefix..."
kubectl get envoyfilters -n "${GATEWAY_NS}" 2>/dev/null || echo "(no envoyfilters found)"
echo ""

PROXY_POD=$(get_proxy_pod)
if [ -n "${PROXY_POD}" ]; then
    echo "Checking Envoy proxy clusters for ext- prefixed entries..."
    echo ""
    kubectl exec -n "${GATEWAY_NS}" "${PROXY_POD}" -c istio-proxy -- \
        curl -s "http://localhost:15000/clusters" 2>/dev/null \
        | grep "^${EXPECTED_CLUSTER_PREFIX}" \
        | head -20 || echo "(no ext-authorino clusters found yet — the operator may still be reconciling)"
else
    echo "(could not find gateway proxy pod)"
fi
wait_for_input

# ---------------------------------------------------------------------------
header "Step 3: Apply AuthPolicy to trigger WasmPlugin creation"
echo "The WasmPlugin is only created when ActionSets exist (from AuthPolicy or"
echo "RateLimitPolicy). Without it, the ext- wasm service entry has nowhere to"
echo "be injected."
echo ""
echo "NOTE: This is a temporary requirement. In future work, extensions will be"
echo "able to define their own ActionSets, removing the need to create a separate"
echo "AuthPolicy or RateLimitPolicy just to trigger WasmPlugin creation."
echo ""
echo "Applying: examples/extension/authpolicy.yaml"
kubectl apply -f "${SCRIPT_DIR}/authpolicy.yaml"
echo ""
echo "Waiting for the AuthPolicy to be enforced..."
sleep 10
kubectl get authpolicy -n "${GATEWAY_NS}" demo-auth -o wide 2>/dev/null || true
wait_for_input

# ---------------------------------------------------------------------------
header "Step 4: Verify wasm service entry in WasmPlugin"
echo "Now that the WasmPlugin exists, checking for ext- prefixed service entries..."
echo ""
echo "WasmPlugins in ${GATEWAY_NS}:"
kubectl get wasmplugins -n "${GATEWAY_NS}" 2>/dev/null || echo "(no wasmplugins found)"
echo ""

WASMPLUGIN=$(kubectl get wasmplugins -n "${GATEWAY_NS}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
if [ -n "${WASMPLUGIN}" ]; then
    echo "WasmPlugin '${WASMPLUGIN}' plugin config services:"
    echo ""
    kubectl get wasmplugin -n "${GATEWAY_NS}" "${WASMPLUGIN}" -o jsonpath='{.spec.pluginConfig.services}' 2>/dev/null | python3 -m json.tool 2>/dev/null || echo "(could not parse services)"
    echo ""
    echo "Note: You should see both the built-in services (auth-service, etc.)"
    echo "and an ext- prefixed service entry for the registered upstream."
else
    echo "(no wasmplugin found — AuthPolicy may still be reconciling)"
fi
wait_for_input

# ---------------------------------------------------------------------------
header "Step 5: Cleanup"
echo "Deleting the UpstreamPolicy and AuthPolicy..."
kubectl delete -f "${SCRIPT_DIR}/upstream-policy.yaml" --ignore-not-found
kubectl delete -f "${SCRIPT_DIR}/authpolicy.yaml" --ignore-not-found
echo ""
echo "Waiting for cleanup..."
sleep 10

echo "Verifying ext- cluster is removed..."
PROXY_POD=$(get_proxy_pod)
if [ -n "${PROXY_POD}" ]; then
    REMAINING=$(kubectl exec -n "${GATEWAY_NS}" "${PROXY_POD}" -c istio-proxy -- \
        curl -s "http://localhost:15000/clusters" 2>/dev/null \
        | grep "^${EXPECTED_CLUSTER_PREFIX}" \
        | head -5 || true)
    if [ -z "${REMAINING}" ]; then
        echo "  ext-authorino cluster removed successfully."
    else
        echo "  Warning: ext-authorino cluster still present (may need more time):"
        echo "  ${REMAINING}"
    fi
else
    echo "(could not find gateway proxy pod)"
fi

echo ""
echo "Verifying WasmPlugin is cleaned up..."
WASMPLUGIN=$(kubectl get wasmplugins -n "${GATEWAY_NS}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
if [ -n "${WASMPLUGIN}" ]; then
    echo "  WasmPlugin still exists (expected — it was created by AuthPolicy, now deleted)."
    echo "  Checking if ext- service entries are removed from the config..."
    SERVICES=$(kubectl get wasmplugin -n "${GATEWAY_NS}" "${WASMPLUGIN}" -o jsonpath='{.spec.pluginConfig.services}' 2>/dev/null || true)
    if echo "${SERVICES}" | grep -q "ext-"; then
        echo "  Warning: ext- service entries still present in WasmPlugin."
    else
        echo "  ext- service entries removed successfully."
    fi
else
    echo "  WasmPlugin removed (AuthPolicy was deleted)."
fi

# ---------------------------------------------------------------------------
header "Demo Complete"
echo "Summary of what was demonstrated:"
echo ""
echo "  1. UpstreamPolicy registered Authorino's gRPC endpoint via RegisterUpstreamMethod"
echo "  2. The operator created an ext- prefixed Envoy cluster (EnvoyFilter)"
echo "  3. AuthPolicy triggered WasmPlugin creation"
echo "  4. The ext- prefixed wasm service entry appeared in the WasmPlugin config"
echo "  5. Cleanup removed the cluster and wasm service entry"
echo ""
echo "The built-in auth-service was unaffected throughout — both coexisted."
