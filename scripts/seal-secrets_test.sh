#!/usr/bin/env bash
# Tests seal-secrets.sh with a fake kubectl and kubeseal. Run: scripts/seal-secrets_test.sh
set -u
here=$(cd "$(dirname "$0")" && pwd)
failed=0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# The script writes into deploy/k8s of the repo it lives in, so test a copy.
fixture="$work/repo"
mkdir -p "$fixture/scripts" "$fixture/deploy/k8s/staging" "$fixture/deploy/k8s/production"
cp "$here/seal-secrets.sh" "$fixture/scripts/seal-secrets.sh"
script="$fixture/scripts/seal-secrets.sh"

# The fake kubectl keeps each Secret's env file under $FAKE_DIR so the test can read
# it, and prints a plain Secret manifest. It must be the only place plaintext goes.
cat >"$work/kubectl" <<'FAKE'
#!/usr/bin/env bash
case " $* " in
*" create secret generic "*)
	name="" file="" prev="" ns=""
	for arg in "$@"; do
		[ "$prev" = generic ] && name=$arg
		[ "$prev" = --namespace ] && ns=$arg
		case $arg in
		--from-env-file=*) file=${arg#--from-env-file=} ;;
		--from-file=*) file=${arg#*=.dockerconfigjson=}; file=${file#--from-file=} ;;
		esac
		prev=$arg
	done
	cp "$file" "$FAKE_DIR/$name.env"
	echo "$ns" >"$FAKE_DIR/$name.ns"
	printf 'kind: Secret\nmetadata:\n  name: %s\n  namespace: %s\n' "$name" "$ns"
	;;
*" get secret "*) echo "kind: List" ;;
esac
FAKE
# The fake kubeseal turns stdin into a SealedSecret with a fresh nonce on every
# call, like the real one, and never repeats the input.
cat >"$work/kubeseal" <<'FAKE'
#!/usr/bin/env bash
case " $* " in
*" --fetch-cert "*) echo "-----FAKE CERT-----" ;;
*)
	echo "$*" >>"$FAKE_DIR/kubeseal.args"
	name=$(sed -n 's/^  name: //p')
	printf 'kind: SealedSecret\nmetadata:\n  name: %s\nspec:\n  encryptedData:\n    blob: %s%s\n' "$name" "$RANDOM" "$RANDOM"
	;;
esac
FAKE
chmod +x "$work/kubectl" "$work/kubeseal"

ok() { echo "ok   $1"; }
fail() {
	echo "FAIL $1"
	failed=1
}
check() { # name, command that succeeds when the behaviour holds
	if eval "$2"; then ok "$1"; else fail "$1"; fi
}

# run args... ; sets $out and $code. Runs in $work, so .secrets/ lands there.
run() {
	mkdir -p "$work/fake"
	out=$(cd "$work" && FAKE_DIR="$work/fake" KUBECTL="$work/kubectl" KUBESEAL="$work/kubeseal" "$script" "$@" 2>&1)
	code=$?
}
reset() { rm -rf "$work/fake" "$fixture/deploy/k8s/staging/secrets"; }
secrets_dir="$fixture/deploy/k8s/staging/secrets"
cert="$fixture/deploy/k8s/staging/sealed-secrets.pem"

# A key the user supplies, with a value that must never reach the output or a sealed file.
sentinel="sentinel-value-must-not-print"
env_file="$work/staging.env"
printf 'ANTHROPIC_API_KEY=%s\nKATANA_GITHUB_USER=octocat\nKATANA_GITHUB_TOKEN=%s\n' "$sentinel" "$sentinel" >"$env_file"

run --env-file "$env_file"
check "refuses without an env" '[ "$code" -eq 2 ] && echo "$out" | grep -q -- "--env must be"'
run --env qa --env-file "$env_file"
check "refuses an unknown env" '[ "$code" -eq 2 ] && echo "$out" | grep -q -- "--env must be"'
run --env staging --env-file "$env_file"
check "refuses to seal without a certificate" '[ "$code" -eq 2 ] && echo "$out" | grep -q -- "--fetch-cert first"'
check "nothing was sealed when it refused" '[ -z "$(ls "$work/fake" 2>/dev/null)" ]'
run --env staging --fetch-cert
check "fetch-cert refuses without a context" '[ "$code" -eq 2 ] && echo "$out" | grep -q -- "--context is required"'

reset
run --env staging --fetch-cert --context kind-shogun
check "fetch-cert writes the certificate" '[ "$code" -eq 0 ] && grep -q "FAKE CERT" "$cert"'
run --env production --fetch-cert --context kind-prod
check "each env has its own certificate" '[ -s "$fixture/deploy/k8s/production/sealed-secrets.pem" ]'
run --env staging --backup-key --context kind-shogun
check "backup-key writes a private file under .secrets" \
	'[ "$code" -eq 0 ] && [ "$(stat -f %Lp "$work/.secrets/staging-sealed-secrets-key.yaml" 2>/dev/null || stat -c %a "$work/.secrets/staging-sealed-secrets-key.yaml")" = 600 ]'

reset
run --env staging --env-file "$env_file"
check "a full run succeeds" '[ "$code" -eq 0 ]'
check "seals 12 Secrets" '[ "$(ls "$secrets_dir"/*.yaml | grep -vc kustomization)" -eq 12 ]'
check "never prints a value" '! echo "$out" | grep -q "$sentinel"'
check "no sealed file holds a plaintext value" '! grep -rq "$sentinel" "$secrets_dir"'
check "the env file is private" '[ "$(stat -f %Lp "$env_file" 2>/dev/null || stat -c %a "$env_file")" = 600 ]'
check "every Secret is sealed with strict scope against the committed certificate" \
	'[ "$(grep -c -- "--scope strict" "$work/fake/kubeseal.args")" -eq 12 ] && grep -q -- "--cert $cert" "$work/fake/kubeseal.args"'
check "the kustomization lists the 12 sealed files and not itself" \
	'[ "$(grep -c "^  - " "$secrets_dir/kustomization.yaml")" -eq 12 ] && ! grep -q "kustomization.yaml$" "$secrets_dir/kustomization.yaml" && grep -q "^  - grafana-admin.yaml" "$secrets_dir/kustomization.yaml"'

keys() { sed 's/=.*//' "$work/fake/$1.env" | sort | tr '\n' ' '; }
check "postgres holds eleven passwords" '[ "$(keys postgres | wc -w | tr -d " ")" -eq 11 ]'
check "torii keys" '[ "$(keys torii)" = "DATABASE_URL GOOGLE_CLIENT_ID GOOGLE_CLIENT_SECRET IDENTITY_SIGNING_KEY TORII_ALLOWED_EMAILS TORII_PUBLIC_URL " ]'
check "fude keys" '[ "$(keys fude)" = "ANTHROPIC_API_KEY DATABASE_URL FUDE_HANKO_KEY_ID FUDE_HANKO_SIGNING_KEY IDENTITY_SIGNING_KEY " ]'
check "tsubame keys" '[ "$(keys tsubame)" = "ANTHROPIC_API_KEY DATABASE_URL IDENTITY_SIGNING_KEY TSUBAME_GMAIL_CLIENT_ID TSUBAME_GMAIL_CLIENT_SECRET TSUBAME_GMAIL_REDIRECT_URL TSUBAME_HANKO_VERIFY_KEYS TSUBAME_TOKEN_KEY_ID TSUBAME_TOKEN_MASTER_KEY " ]'
check "katana keys" '[ "$(keys katana)" = "ANTHROPIC_API_KEY DATABASE_URL IDENTITY_SIGNING_KEY KATANA_GITHUB_TOKEN KATANA_GITHUB_USER " ]'
check "kagami has only the common keys" '[ "$(keys kagami)" = "DATABASE_URL IDENTITY_SIGNING_KEY " ]'

value() { sed -n "s/^$2=//p" "$work/fake/$1.env"; }
check "every service shares one identity key" '[ "$(value torii IDENTITY_SIGNING_KEY)" = "$(value soroban IDENTITY_SIGNING_KEY)" ]'
check "a DATABASE_URL uses its own role and password" '
	pw=$(value postgres KAGAMI_DB_PASSWORD)
	[ "$(value kagami DATABASE_URL)" = "postgres://kagami:$pw@postgres:5432/shogun" ]'

# source the script for its functions (it only runs main when executed)
. "$script"
dev_seed="AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="
dev_public="A6EHv/POEL4dcN0Y50vAmWfk1jCbpQ1fHdyGZBJVMbg="
check "the Hanko public key derivation matches the dev pair in .env.example" '[ "$(hanko_public_key "$dev_seed")" = "$dev_public" ]'
check "tsubame verifies with the public half of the fude key" '
	seed=$(value fude FUDE_HANKO_SIGNING_KEY)
	[ "$(value tsubame TSUBAME_HANKO_VERIFY_KEYS)" = "fude-1=$(hanko_public_key "$seed")" ]'

check "grafana-admin holds the login keys Grafana reads" '[ "$(keys grafana-admin)" = "admin-password admin-user " ]'
check "grafana-admin is sealed for monitoring and the rest for shogun" '[ "$(cat "$work/fake/grafana-admin.ns")" = monitoring ] && [ "$(cat "$work/fake/kagami.ns")" = shogun ]'
check "the Grafana password is generated and never printed" '
	pw=$(value grafana-admin admin-password)
	[ ${#pw} -ge 32 ] && [ "$pw" = "$(sed -n "s/^GRAFANA_ADMIN_PASSWORD=//p" "$env_file")" ] && ! echo "$out" | grep -q "$pw"'

check "no pull secret without a GHCR token" '[ ! -e "$secrets_dir/ghcr.yaml" ]'
printf 'GHCR_USER=octocat\nGHCR_TOKEN=%s\n' "$sentinel" >>"$env_file"
run --env staging --env-file "$env_file"
check "a GHCR token seals the pull secret" '[ "$code" -eq 0 ] && [ -s "$secrets_dir/ghcr.yaml" ]'
check "the pull secret is a dockerconfigjson for ghcr.io" 'grep -q "\"ghcr.io\"" "$work/fake/ghcr.env" && grep -q "octocat" "$work/fake/ghcr.env"'
check "the token is never printed or written sealed in the clear" '! echo "$out" | grep -q "$sentinel" && ! grep -rq "$sentinel" "$secrets_dir"'
check "the kustomization now lists 13 files" '[ "$(grep -c "^  - " "$secrets_dir/kustomization.yaml")" -eq 13 ]'

before=$(cksum <"$env_file")
run --env staging --env-file "$env_file"
check "a second run changes nothing in the env file" '[ "$before" = "$(cksum <"$env_file")" ]'

kagami_before=$(cksum <"$secrets_dir/kagami.yaml")
torii_before=$(cksum <"$secrets_dir/torii.yaml")
run --env staging --env-file "$env_file" --only torii
check "--only reseals just that Secret" \
	'[ "$code" -eq 0 ] && [ "$kagami_before" = "$(cksum <"$secrets_dir/kagami.yaml")" ] && [ "$torii_before" != "$(cksum <"$secrets_dir/torii.yaml")" ]'
check "--only keeps every file in the kustomization" '[ "$(grep -c "^  - " "$secrets_dir/kustomization.yaml")" -eq 13 ]'

exit $failed
