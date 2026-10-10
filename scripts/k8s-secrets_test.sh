#!/usr/bin/env bash
# Tests for k8s-secrets.sh with a fake kubectl. Run: scripts/k8s-secrets_test.sh
set -u
here=$(cd "$(dirname "$0")" && pwd)
script="$here/k8s-secrets.sh"
failed=0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# The fake keeps each Secret's env file under $FAKE_DIR so the test can read it.
cat >"$work/kubectl" <<'FAKE'
#!/usr/bin/env bash
case " $* " in
*" create secret generic "*)
	name="" file="" prev=""
	for arg in "$@"; do
		[ "$prev" = generic ] && name=$arg
		case $arg in
		--from-env-file=*) file=${arg#--from-env-file=} ;;
		--from-file=*) file=${arg#*=.dockerconfigjson=}; file=${file#--from-file=} ;;
		esac
		prev=$arg
	done
	cp "$file" "$FAKE_DIR/$name.env"
	echo "kind: Secret" ;;
*" create namespace "*) echo "kind: Namespace" ;;
*" apply "*) cat >/dev/null ;;
esac
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

run() { # args...; sets $out and $code
	rm -rf "$work/fake"
	mkdir -p "$work/fake"
	out=$(FAKE_DIR="$work/fake" KUBECTL="$work/kubectl" "$script" "$@" 2>&1)
	code=$?
}

# A key the user supplies, with a value that must never reach the output.
sentinel="sentinel-value-must-not-print"
env_file="$work/k8s.env"
printf 'ANTHROPIC_API_KEY=%s\nKATANA_GITHUB_USER=octocat\nKATANA_GITHUB_TOKEN=%s\n' "$sentinel" "$sentinel" >"$env_file"

run --namespace shogun --env-file "$env_file"
check "refuses without a context" '[ "$code" -eq 2 ] && echo "$out" | grep -q -- "--context is required"'
run --context kind-shogun --env-file "$env_file"
check "refuses without a namespace" '[ "$code" -eq 2 ] && echo "$out" | grep -q -- "--namespace is required"'
check "nothing was created when it refused" '[ -z "$(ls "$work/fake" 2>/dev/null)" ]'

run --context kind-shogun --namespace shogun --env-file "$env_file"
check "a full run succeeds" '[ "$code" -eq 0 ]'
check "creates 11 Secrets" '[ "$(ls "$work/fake" | wc -l | tr -d " ")" -eq 11 ]'
check "never prints a value" '! echo "$out" | grep -q "$sentinel"'
check "the env file is private" '[ "$(stat -f %Lp "$env_file" 2>/dev/null || stat -c %a "$env_file")" = 600 ]'

keys() { sed 's/=.*//' "$work/fake/$1.env" | sort | tr '\n' ' '; }
check "postgres holds eleven passwords" '[ "$(keys shogun-postgres | wc -w | tr -d " ")" -eq 11 ]'
check "torii keys" '[ "$(keys shogun-torii)" = "DATABASE_URL GOOGLE_CLIENT_ID GOOGLE_CLIENT_SECRET IDENTITY_SIGNING_KEY TORII_ALLOWED_EMAILS TORII_PUBLIC_URL " ]'
check "fude keys" '[ "$(keys shogun-fude)" = "ANTHROPIC_API_KEY DATABASE_URL FUDE_HANKO_KEY_ID FUDE_HANKO_SIGNING_KEY IDENTITY_SIGNING_KEY " ]'
check "tsubame keys" '[ "$(keys shogun-tsubame)" = "ANTHROPIC_API_KEY DATABASE_URL IDENTITY_SIGNING_KEY TSUBAME_GMAIL_CLIENT_ID TSUBAME_GMAIL_CLIENT_SECRET TSUBAME_GMAIL_REDIRECT_URL TSUBAME_HANKO_VERIFY_KEYS TSUBAME_TOKEN_KEY_ID TSUBAME_TOKEN_MASTER_KEY " ]'
check "katana keys" '[ "$(keys shogun-katana)" = "ANTHROPIC_API_KEY DATABASE_URL IDENTITY_SIGNING_KEY KATANA_GITHUB_TOKEN KATANA_GITHUB_USER " ]'
check "kagami has only the common keys" '[ "$(keys shogun-kagami)" = "DATABASE_URL IDENTITY_SIGNING_KEY " ]'

value() { sed -n "s/^$2=//p" "$work/fake/$1.env"; }
check "every service shares one identity key" '[ "$(value shogun-torii IDENTITY_SIGNING_KEY)" = "$(value shogun-soroban IDENTITY_SIGNING_KEY)" ]'
check "a DATABASE_URL uses its own role and password" '
	pw=$(value shogun-postgres KAGAMI_DB_PASSWORD)
	[ "$(value shogun-kagami DATABASE_URL)" = "postgres://kagami:$pw@shogun-postgres:5432/shogun" ]'

# source the script for its functions (it only runs main when executed)
. "$script"
dev_seed="AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="
dev_public="A6EHv/POEL4dcN0Y50vAmWfk1jCbpQ1fHdyGZBJVMbg="
check "the Hanko public key derivation matches the dev pair in .env.example" '[ "$(hanko_public_key "$dev_seed")" = "$dev_public" ]'
check "tsubame verifies with the public half of the fude key" '
	seed=$(value shogun-fude FUDE_HANKO_SIGNING_KEY)
	[ "$(value shogun-tsubame TSUBAME_HANKO_VERIFY_KEYS)" = "fude-1=$(hanko_public_key "$seed")" ]'

check "no pull secret without a GHCR token" '[ ! -e "$work/fake/ghcr.env" ]'
printf 'GHCR_USER=octocat\nGHCR_TOKEN=%s\n' "$sentinel" >>"$env_file"
run --context kind-shogun --namespace shogun --env-file "$env_file"
check "a GHCR token creates the pull secret" '[ "$code" -eq 0 ] && [ -s "$work/fake/ghcr.env" ]'
check "the pull secret is a dockerconfigjson for ghcr.io" 'grep -q "\"ghcr.io\"" "$work/fake/ghcr.env" && grep -q "octocat" "$work/fake/ghcr.env"'
check "the token is never printed" '! echo "$out" | grep -q "$sentinel"'

before=$(cksum <"$env_file")
run --context kind-shogun --namespace shogun --env-file "$env_file"
check "a second run changes nothing in the env file" '[ "$before" = "$(cksum <"$env_file")" ]'

exit $failed
