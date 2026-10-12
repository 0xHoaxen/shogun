#!/usr/bin/env bash
# Tests for platform.sh with a fake helm. Run: scripts/platform_test.sh
set -u
here=$(cd "$(dirname "$0")" && pwd)
script="$here/platform.sh"
failed=0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# The fake appends each call to $FAKE_LOG, one line per call.
cat >"$work/helm" <<'FAKE'
#!/usr/bin/env bash
echo "$*" >>"$FAKE_LOG"
FAKE
chmod +x "$work/helm"
cat >"$work/kubectl" <<'FAKE'
#!/usr/bin/env bash
echo "kubectl $*" >>"$FAKE_LOG"
FAKE
chmod +x "$work/kubectl"

ok() { echo "ok   $1"; }
fail() {
	echo "FAIL $1"
	failed=1
}
check() { # name, command that succeeds when the behaviour holds
	if eval "$2"; then ok "$1"; else fail "$1"; fi
}

run() { # args...; sets $out and $code, calls are in $log
	log="$work/helm.log"
	rm -f "$log"
	out=$(FAKE_LOG="$log" HELM="$work/helm" KUBECTL="$work/kubectl" "$script" "$@" 2>&1)
	code=$?
}
calls() { cat "$log" 2>/dev/null; }
release_order() { calls | grep '^upgrade ' | awk '{ print $3 }' | tr '\n' ' '; }

run
check "refuses without a context" '[ "$code" -eq 2 ] && echo "$out" | grep -q -- "--context is required"'
run --context k3s --env staging --namespace shogun
check "refuses an unknown argument" '[ "$code" -eq 2 ] && echo "$out" | grep -q "unknown argument"'
check "helm was never called when it refused" '[ -z "$(calls)" ]'

run --context k3s
check "refuses without an env" '[ "$code" -eq 2 ] && echo "$out" | grep -q -- "--env must be"'
run --context k3s --env qa
check "refuses an unknown env" '[ "$code" -eq 2 ] && echo "$out" | grep -q -- "--env must be"'
check "nothing ran when it refused the env" '[ -z "$(calls)" ]'
run --context k3s --env staging
check "a full run succeeds" '[ "$code" -eq 0 ]'
check "applies the cluster basics for the env before any release" \
	'calls | head -n 1 | grep -q "^kubectl --context k3s apply -k .*/deploy/k8s/staging$"'
check "installs cert-manager, ingress, then monitoring" \
	'[ "$(release_order)" = "cert-manager ingress monitoring " ]'
check "every call is upgrade --install" '[ "$(calls | grep -c "^upgrade --install ")" -eq 3 ]'
check "every call pins the context" '[ "$(calls | grep -c -- "--kube-context k3s ")" -eq 3 ]'
check "every call waits" '[ "$(calls | grep -c -- "--wait")" -eq 3 ]'
check "cert-manager is pinned and installs its CRDs" \
	'calls | grep "^upgrade --install cert-manager " | grep -q -- "--version v[0-9]" &&
	 calls | grep "^upgrade --install cert-manager " | grep -q -- "--set crds.enabled=true"'
check "cert-manager goes into its own namespace" \
	'calls | grep "^upgrade --install cert-manager " | grep -q -- "--namespace cert-manager --create-namespace"'
check "the ingress chart comes from the checkout" \
	'calls | grep "^upgrade --install ingress " | grep -q "deploy/helm/platform/ingress "'
check "the ingress release lives next to k3s Traefik" \
	'calls | grep "^upgrade --install ingress " | grep -q -- "--namespace kube-system"'
check "monitoring is the local umbrella chart, in the namespace deploy/k8s created" \
	'calls | grep "^upgrade --install monitoring " | grep -q "deploy/helm/monitoring " &&
	 calls | grep "^upgrade --install monitoring " | grep -q -- "--namespace monitoring" &&
	 ! calls | grep "^upgrade --install monitoring " | grep -q -- "--create-namespace"'
check "monitoring fetches the stack dependency and has the longer timeout" \
	'calls | grep "^upgrade --install monitoring " | grep -q -- "--dependency-update" &&
	 calls | grep "^upgrade --install monitoring " | grep -q -- "--timeout 10m"'

run --context k3s --env production
check "production applies its own overlay" \
	'calls | head -n 1 | grep -q "^kubectl --context k3s apply -k .*/deploy/k8s/production$"'

exit "$failed"
