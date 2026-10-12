#!/usr/bin/env bash
# Tests for deploy.sh with a fake helmfile. Run: scripts/deploy_test.sh
set -u
here=$(cd "$(dirname "$0")" && pwd)
script="$here/deploy.sh"
failed=0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# The fake appends each call to $FAKE_LOG, one line per call.
cat >"$work/helmfile" <<'FAKE'
#!/usr/bin/env bash
echo "$*" >>"$FAKE_LOG"
FAKE
chmod +x "$work/helmfile"

ok() { echo "ok   $1"; }
fail() {
	echo "FAIL $1"
	failed=1
}
check() { # name, command that succeeds when the behaviour holds
	if eval "$2"; then ok "$1"; else fail "$1"; fi
}

run() { # args...; sets $out and $code, calls are in $log
	log="$work/helmfile.log"
	rm -f "$log"
	out=$(FAKE_LOG="$log" HELMFILE="$work/helmfile" "$script" "$@" 2>&1)
	code=$?
}
calls() { cat "$log" 2>/dev/null; }
phases() { calls | sed -n 's/.*--selector phase=\([a-z]*\).*/\1/p' | tr '\n' ' '; }

base=(--context kind-shogun --env staging)

run --env staging
check "refuses without a context" '[ "$code" -eq 2 ] && echo "$out" | grep -q -- "--context is required"'
run --context kind-shogun
check "refuses without an env" '[ "$code" -eq 2 ] && echo "$out" | grep -q -- "--env must be"'
run --context kind-shogun --env dev
check "refuses an unknown env" '[ "$code" -eq 2 ]'
run "${base[@]}" --namespace shogun
check "refuses an unknown argument" '[ "$code" -eq 2 ] && echo "$out" | grep -q "unknown argument"'
run "${base[@]}" --versions-file "$work/missing.env"
check "refuses a missing versions file" '[ "$code" -eq 2 ] && echo "$out" | grep -q "versions file not found"'
check "helmfile was never called when it refused" '[ -z "$(calls)" ]'

run "${base[@]}"
check "a full run succeeds" '[ "$code" -eq 0 ]'
check "applies phase data, then phase apps" '[ "$(phases)" = "data apps " ]'
check "every call is an apply, one release at a time" '[ "$(calls | grep -c " apply --concurrency 1 ")" -eq 2 ]'
check "every call pins the file, env and context" \
	'[ "$(calls | grep -c -- "--file .*/helmfile.yaml.gotmpl --environment staging --kube-context kind-shogun ")" -eq 2 ]'
check "no tag override without a versions file" '! calls | grep -q -- "--state-values-set"'

if [ ! -f "$here/../deploy/apps/web/values-production.yaml" ]; then
	run --context kind-shogun --env production
	check "production skips web while it has no production values" \
		'[ "$code" -eq 0 ] && echo "$out" | grep -q "web has no values-production.yaml"'
fi

printf 'KAGAMI=9.9.9\nWEB=8.8.8\nKAGAMI=7.7.7\n' >"$work/versions.env"
run "${base[@]}" --versions-file "$work/versions.env"
check "versions file overrides the values file (last line wins)" \
	'calls | grep "phase=apps" | grep -q -- "--state-values-set tags.kagami=7.7.7 "'
check "versions file applies to web" 'calls | grep "phase=apps" | grep -q -- "--state-values-set tags.web=8.8.8 "'
check "only the apps phase gets overrides" '! calls | grep "phase=data" | grep -q -- "--state-values-set"'
check "apps missing from the versions file keep their values-file tag" '! calls | grep -q "tags.torii="'

# A values file without a tag stops the run before anything is installed.
fixture="$work/repo"
mkdir -p "$fixture/scripts"
cp "$script" "$fixture/scripts/deploy.sh"
for n in soroban kagami fude tsubame taiko dojo katana shinobi sensei torii web; do
	mkdir -p "$fixture/deploy/apps/$n"
	printf 'image:\n  tag: "1.0.0"\n' >"$fixture/deploy/apps/$n/values-staging.yaml"
done
printf 'env:\n  ENVIRONMENT: staging\n' >"$fixture/deploy/apps/sensei/values-staging.yaml"
log="$work/helmfile.log"
rm -f "$log"
out=$(FAKE_LOG="$log" HELMFILE="$work/helmfile" "$fixture/scripts/deploy.sh" "${base[@]}" 2>&1)
code=$?
check "refuses when an app has no version" '[ "$code" -eq 2 ] && echo "$out" | grep -q "no version for sensei"'
check "nothing was installed before the version check failed" '[ -z "$(calls)" ]'

exit "$failed"
