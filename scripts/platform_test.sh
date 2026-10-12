#!/usr/bin/env bash
# Tests for platform.sh with a fake helmfile and kubectl. Run: scripts/platform_test.sh
set -u
here=$(cd "$(dirname "$0")" && pwd)
# A copy of the script in a fixture tree, so the sealed-Secrets directory can be
# present or missing without touching the repo.
fixture_root=$(mktemp -d)
mkdir -p "$fixture_root/scripts"
cp "$here/platform.sh" "$fixture_root/scripts/platform.sh"
for env in staging production; do
	mkdir -p "$fixture_root/deploy/infrastructure/sealed-secrets/secrets/$env"
	printf 'kind: Kustomization\n' >"$fixture_root/deploy/infrastructure/sealed-secrets/secrets/$env/kustomization.yaml"
done
script="$fixture_root/scripts/platform.sh"
failed=0
work=$(mktemp -d)
trap 'rm -rf "$work" "$fixture_root"' EXIT

# Both fakes append each call to $FAKE_LOG, one line per call.
cat >"$work/helmfile" <<'FAKE'
#!/usr/bin/env bash
echo "helmfile $*" >>"$FAKE_LOG"
FAKE
cat >"$work/kubectl" <<'FAKE'
#!/usr/bin/env bash
echo "kubectl $*" >>"$FAKE_LOG"
FAKE
chmod +x "$work/helmfile" "$work/kubectl"

ok() { echo "ok   $1"; }
fail() {
	echo "FAIL $1"
	failed=1
}
check() { # name, command that succeeds when the behaviour holds
	if eval "$2"; then ok "$1"; else fail "$1"; fi
}

run() { # args...; sets $out and $code, calls are in $log
	log="$work/calls.log"
	rm -f "$log"
	out=$(FAKE_LOG="$log" HELMFILE="$work/helmfile" KUBECTL="$work/kubectl" "$script" "$@" 2>&1)
	code=$?
}
calls() { cat "$log" 2>/dev/null; }
# steps prints one word per call: the phase for helmfile, what was applied for kubectl.
steps() {
	calls | sed -e 's/^helmfile .*--selector phase=\([a-z]*\).*/\1/' \
		-e 's#^kubectl .* apply -k .*/sealed-secrets/secrets/.*#secrets#' \
		-e 's#^kubectl .* apply -f .*/issuers$#issuers#' \
		-e 's#^kubectl .* apply -f .*/k3s-helmchartconfig.yaml$#traefik#' | tr '\n' ' '
}

run
check "refuses without a context" '[ "$code" -eq 2 ] && echo "$out" | grep -q -- "--context is required"'
run --context k3s --env staging --namespace shogun
check "refuses an unknown argument" '[ "$code" -eq 2 ] && echo "$out" | grep -q "unknown argument"'
run --context k3s
check "refuses without an env" '[ "$code" -eq 2 ] && echo "$out" | grep -q -- "--env must be"'
run --context k3s --env qa
check "refuses an unknown env" '[ "$code" -eq 2 ] && echo "$out" | grep -q -- "--env must be"'
check "nothing ran when it refused" '[ -z "$(calls)" ]'

run --context k3s --env staging
check "a full run succeeds" '[ "$code" -eq 0 ]'
check "bootstrap, sealed Secrets, platform, then issuers and the Traefik redirect" \
	'[ "$(steps)" = "bootstrap secrets platform issuers traefik " ]'
check "every helmfile call pins the file, env and context" \
	'[ "$(calls | grep -c -- "^helmfile --file .*/helmfile.yaml.gotmpl --environment staging --kube-context k3s ")" -eq 2 ]'
check "every helmfile call applies one release at a time" '[ "$(calls | grep -c " apply --concurrency 1 ")" -eq 2 ]'
check "every kubectl call pins the context" '[ "$(calls | grep -c "^kubectl --context k3s ")" -eq 3 ]'
check "the sealed Secrets come from the env's directory" \
	'calls | grep -q "apply -k .*/deploy/infrastructure/sealed-secrets/secrets/staging$"'

rm "$fixture_root/deploy/infrastructure/sealed-secrets/secrets/staging/kustomization.yaml"
run --context k3s --env staging
check "stops with exit 3 when no Secrets are sealed" '[ "$code" -eq 3 ]'
check "tells which commands to run" 'echo "$out" | grep -q "scripts/seal-secrets.sh --env staging --fetch-cert --context k3s"'
check "stops after the controller" '[ "$(steps)" = "bootstrap " ]'

run --context k3s --env production
check "production uses its own environment and Secrets" \
	'calls | head -n 1 | grep -q -- "--environment production " &&
	 calls | grep -q "apply -k .*/sealed-secrets/secrets/production$"'

exit "$failed"
