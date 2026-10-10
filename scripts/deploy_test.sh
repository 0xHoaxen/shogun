#!/usr/bin/env bash
# Tests for deploy.sh with a fake helm. Run: scripts/deploy_test.sh
set -u
here=$(cd "$(dirname "$0")" && pwd)
script="$here/deploy.sh"
failed=0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# The fake appends each call to $FAKE_LOG, one line per call.
cat >"$work/helm" <<'FAKE'
#!/usr/bin/env bash
echo "$*" >>"$FAKE_LOG"
FAKE
chmod +x "$work/helm"

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
	out=$(FAKE_LOG="$log" HELM="$work/helm" "$script" "$@" 2>&1)
	code=$?
}
calls() { cat "$log" 2>/dev/null; }
release_order() { calls | awk '{ print $3 }' | tr '\n' ' '; }
staging_tag() { sed -n 's/^  tag: "\(.*\)"/\1/p' "$here/../deploy/helm/values/staging/$1.yaml"; }

base=(--context kind-shogun --namespace shogun --env staging)

run --namespace shogun --env staging
check "refuses without a context" '[ "$code" -eq 2 ] && echo "$out" | grep -q -- "--context is required"'
run --context kind-shogun --env staging
check "refuses without a namespace" '[ "$code" -eq 2 ] && echo "$out" | grep -q -- "--namespace is required"'
run --context kind-shogun --namespace shogun
check "refuses without an env" '[ "$code" -eq 2 ] && echo "$out" | grep -q -- "--env must be"'
run --context kind-shogun --namespace shogun --env dev
check "refuses an unknown env" '[ "$code" -eq 2 ]'
run "${base[@]}" --versions-file "$work/missing.env"
check "refuses a missing versions file" '[ "$code" -eq 2 ] && echo "$out" | grep -q "versions file not found"'
check "helm was never called when it refused" '[ -z "$(calls)" ]'

if [ ! -f "$here/../deploy/helm/values/production/web.yaml" ]; then
	run --context kind-shogun --namespace shogun --env production
	check "production refuses while web.yaml is missing" '[ "$code" -eq 2 ] && echo "$out" | grep -q "missing .*production/web.yaml"'
	check "production touched nothing" '[ -z "$(calls)" ]'
fi

run "${base[@]}"
check "a full run succeeds" '[ "$code" -eq 0 ]'
check "installs postgres, soroban, the services, then web" \
	'[ "$(release_order)" = "postgres shogun-soroban shogun-kagami shogun-fude shogun-tsubame shogun-taiko shogun-dojo shogun-katana shogun-shinobi shogun-sensei shogun-torii shogun-web " ]'
check "every call is upgrade --install" '[ "$(calls | grep -c "^upgrade --install ")" -eq 12 ]'
check "every call pins the context and namespace" \
	'[ "$(calls | grep -c -- "--kube-context kind-shogun --namespace shogun")" -eq 12 ]'
check "every call waits" '[ "$(calls | grep -c -- "--wait")" -eq 12 ]'
check "postgres comes from the checkout, not the registry" \
	'calls | head -n 1 | grep -q "deploy/helm/postgres" && ! calls | head -n 1 | grep -q "oci://"'
check "services come from the registry" \
	'calls | grep "^upgrade --install shogun-kagami " | grep -q "oci://ghcr.io/0xhoaxen/charts/shogun-kagami"'
check "web comes from the registry" \
	'calls | grep "^upgrade --install shogun-web " | grep -q "oci://ghcr.io/0xhoaxen/charts/shogun-web"'
check "uses the values file of the env" \
	'calls | grep "shogun-kagami " | grep -q -- "-f .*/values/staging/kagami.yaml"'
check "chart version and image tag are the values-file tag" \
	'tag=$(staging_tag kagami);
	 calls | grep "shogun-kagami " | grep -q -- "--version $tag " && calls | grep "shogun-kagami " | grep -q -- "--set image.tag=$tag "'
check "passes the ghcr pull secret to services" \
	'[ "$(calls | grep -c -- "--set imagePullSecrets\[0\].name=ghcr")" -eq 11 ]'
check "does not pass the pull secret to postgres" '! calls | head -n 1 | grep -q imagePullSecrets'

printf 'KAGAMI=9.9.9\nWEB=8.8.8\nKAGAMI=7.7.7\n' >"$work/versions.env"
run "${base[@]}" --versions-file "$work/versions.env"
check "versions file overrides the values file (last line wins)" \
	'calls | grep "shogun-kagami " | grep -q -- "--version 7.7.7 " && calls | grep "shogun-kagami " | grep -q -- "--set image.tag=7.7.7 "'
check "versions file applies to web" 'calls | grep "shogun-web " | grep -q -- "--version 8.8.8 "'
check "services missing from the versions file keep their values-file tag" \
	'tag=$(staging_tag torii); calls | grep "shogun-torii " | grep -q -- "--version $tag "'

run "${base[@]}" --pull-secret ""
check "an empty --pull-secret turns it off" '[ "$code" -eq 0 ] && ! calls | grep -q imagePullSecrets'
run "${base[@]}" --pull-secret registry-creds
check "a named pull secret is passed through" \
	'[ "$(calls | grep -c -- "--set imagePullSecrets\[0\].name=registry-creds")" -eq 11 ]'

# A values file without a tag stops the run before anything is installed.
fixture="$work/repo"
mkdir -p "$fixture/scripts" "$fixture/deploy/helm/postgres" "$fixture/deploy/helm/values/staging"
cp "$script" "$fixture/scripts/deploy.sh"
for n in soroban kagami fude tsubame taiko dojo katana shinobi sensei torii web; do
	printf 'name: %s\nimage:\n  tag: "1.0.0"\n' "$n" >"$fixture/deploy/helm/values/staging/$n.yaml"
done
printf 'name: sensei\nenv:\n  ENVIRONMENT: staging\n' >"$fixture/deploy/helm/values/staging/sensei.yaml"
log="$work/helm.log"
rm -f "$log"
out=$(FAKE_LOG="$log" HELM="$work/helm" "$fixture/scripts/deploy.sh" "${base[@]}" 2>&1)
code=$?
check "refuses when a service has no version" '[ "$code" -eq 2 ] && echo "$out" | grep -q "no version for sensei"'
check "nothing was installed before the version check failed" '[ -z "$(calls)" ]'

exit "$failed"
