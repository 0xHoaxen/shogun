#!/usr/bin/env bash
# Creates the Secrets a cluster needs: shogun-postgres (read by deploy/helm/postgres)
# and shogun-<service> for the ten services (read by the Helm chart with envFrom).
#
# Usage: scripts/k8s-secrets.sh --context <ctx> --namespace <ns> [--env-file <file>]
#
# Values come from the env file (default .secrets/k8s.env, git-ignored, KEY=value
# lines, no quoting). Anything generated is written back to that file, so a second
# run changes nothing: Postgres creates its roles once, so passwords must not move.
# See deploy/k8s/secrets.example.env for what you supply yourself. Values are never
# printed, only key and Secret names.
set -euo pipefail

SERVICES="torii kagami tsubame fude taiko dojo katana shinobi sensei soroban"
KUBECTL="${KUBECTL:-kubectl}"
DEFAULT_ENV_FILE=".secrets/k8s.env"
PLACEHOLDER_ID="replace-me.apps.googleusercontent.com"
PLACEHOLDER_SECRET="replace-me"
PLACEHOLDER_EMAIL="you@example.com"
SEED_BYTES=32

die() {
	echo "k8s-secrets: $*" >&2
	exit 2
}

upper() { printf '%s' "$1" | tr '[:lower:]' '[:upper:]'; }

# env_get FILE KEY prints the last value of KEY, or nothing.
env_get() { sed -n "s/^$2=//p" "$1" | tail -n 1; }

# env_set_default FILE KEY VALUE appends KEY=VALUE unless KEY already has a value.
env_set_default() {
	local file=$1 key=$2 value=$3
	[ -n "$(env_get "$file" "$key")" ] && return 0
	if [ -s "$file" ] && [ "$(tail -c 1 "$file" | wc -l | tr -d ' ')" = 0 ]; then
		printf '\n' >>"$file"
	fi
	printf '%s=%s\n' "$key" "$value" >>"$file"
}

require_openssl3() {
	openssl version | grep -q '^OpenSSL 3' ||
		die "OpenSSL 3 is needed to derive the Hanko public key (found: $(openssl version)); on macOS, brew install openssl and put it first on PATH"
}

# hanko_public_key SEED prints the base64 Ed25519 public key for a base64 32-byte
# seed: the PKCS8 prefix plus the seed, read back as a public key.
hanko_public_key() {
	local seed=$1 length
	length=$(printf '%s' "$seed" | base64 -d | wc -c | tr -d ' ')
	[ "$length" = "$SEED_BYTES" ] || die "FUDE_HANKO_SIGNING_KEY must be base64 of $SEED_BYTES bytes"
	{
		printf '\x30\x2e\x02\x01\x00\x30\x05\x06\x03\x2b\x65\x70\x04\x22\x04\x20'
		printf '%s' "$seed" | base64 -d
	} | openssl pkey -inform DER -pubout -outform DER | tail -c 32 | base64
}

# service_keys NAME lists the variables a service reads beyond DATABASE_URL and
# IDENTITY_SIGNING_KEY (see .env.example).
service_keys() {
	case $1 in
	torii) echo "TORII_PUBLIC_URL GOOGLE_CLIENT_ID GOOGLE_CLIENT_SECRET TORII_ALLOWED_EMAILS" ;;
	fude) echo "FUDE_HANKO_SIGNING_KEY FUDE_HANKO_KEY_ID ANTHROPIC_API_KEY" ;;
	tsubame) echo "TSUBAME_TOKEN_MASTER_KEY TSUBAME_TOKEN_KEY_ID TSUBAME_GMAIL_CLIENT_ID TSUBAME_GMAIL_CLIENT_SECRET TSUBAME_GMAIL_REDIRECT_URL TSUBAME_HANKO_VERIFY_KEYS ANTHROPIC_API_KEY" ;;
	katana) echo "KATANA_GITHUB_USER KATANA_GITHUB_TOKEN ANTHROPIC_API_KEY" ;;
	shinobi) echo "ANTHROPIC_API_KEY" ;;
	*) echo "" ;;
	esac
}

# fill_env FILE writes the generated values and the defaults that are missing.
fill_env() {
	local file=$1 svc
	env_set_default "$file" POSTGRES_PASSWORD "$(openssl rand -hex 24)"
	for svc in $SERVICES; do
		env_set_default "$file" "$(upper "$svc")_DB_PASSWORD" "$(openssl rand -hex 24)"
	done
	env_set_default "$file" IDENTITY_SIGNING_KEY "$(openssl rand -hex 32)"
	env_set_default "$file" TSUBAME_TOKEN_MASTER_KEY "$(openssl rand -base64 32)"
	env_set_default "$file" FUDE_HANKO_SIGNING_KEY "$(openssl rand -base64 32)"
	env_set_default "$file" FUDE_HANKO_KEY_ID fude-1
	env_set_default "$file" TSUBAME_TOKEN_KEY_ID tsubame-1
	env_set_default "$file" TORII_PUBLIC_URL http://localhost:3000
	env_set_default "$file" TSUBAME_GMAIL_REDIRECT_URL http://localhost:3000/mail/callback
	env_set_default "$file" GOOGLE_CLIENT_ID "$PLACEHOLDER_ID"
	env_set_default "$file" GOOGLE_CLIENT_SECRET "$PLACEHOLDER_SECRET"
	env_set_default "$file" TORII_ALLOWED_EMAILS "$PLACEHOLDER_EMAIL"
	env_set_default "$file" TSUBAME_GMAIL_CLIENT_ID "$PLACEHOLDER_ID"
	env_set_default "$file" TSUBAME_GMAIL_CLIENT_SECRET "$PLACEHOLDER_SECRET"
}

warn_unset() {
	local file=$1 key
	for key in GOOGLE_CLIENT_ID GOOGLE_CLIENT_SECRET TSUBAME_GMAIL_CLIENT_ID TSUBAME_GMAIL_CLIENT_SECRET; do
		case "$(env_get "$file" "$key")" in
		replace-me*) echo "k8s-secrets: $key is a placeholder; the service starts but Google sign-in or Gmail will not work" >&2 ;;
		esac
	done
	if [ "$(env_get "$file" TORII_ALLOWED_EMAILS)" = "$PLACEHOLDER_EMAIL" ]; then
		echo "k8s-secrets: TORII_ALLOWED_EMAILS is a placeholder; nobody can sign in" >&2
	fi
	for key in ANTHROPIC_API_KEY KATANA_GITHUB_USER KATANA_GITHUB_TOKEN; do
		if [ -z "$(env_get "$file" "$key")" ]; then
			echo "k8s-secrets: $key is not set; the services that use it start, but drafts or GitHub sync will fail" >&2
		fi
	done
}

# apply_secret CONTEXT NAMESPACE NAME FILE creates or updates one Secret from an env file.
apply_secret() {
	local context=$1 namespace=$2 name=$3 file=$4 keys
	keys=$(grep -c '=' "$file" || true)
	"$KUBECTL" --context "$context" -n "$namespace" create secret generic "$name" \
		--from-env-file="$file" --dry-run=client -o yaml |
		"$KUBECTL" --context "$context" -n "$namespace" apply -f - >/dev/null
	echo "secret/$name applied ($keys keys)"
}

# service_env SRC DEST SERVICE writes the variables for one service Secret.
service_env() {
	local src=$1 dest=$2 svc=$3 key value
	{
		echo "DATABASE_URL=postgres://$svc:$(env_get "$src" "$(upper "$svc")_DB_PASSWORD")@shogun-postgres:5432/shogun"
		echo "IDENTITY_SIGNING_KEY=$(env_get "$src" IDENTITY_SIGNING_KEY)"
		for key in $(service_keys "$svc"); do
			value=$(env_get "$src" "$key")
			if [ "$key" = TSUBAME_HANKO_VERIFY_KEYS ] && [ -z "$value" ]; then
				value="$(env_get "$src" FUDE_HANKO_KEY_ID)=$(hanko_public_key "$(env_get "$src" FUDE_HANKO_SIGNING_KEY)")"
			fi
			if [ -n "$value" ]; then
				echo "$key=$value"
			fi
		done
	} >"$dest"
}

main() {
	local context="" namespace="" env_file="$DEFAULT_ENV_FILE" tmp svc
	while [ $# -gt 0 ]; do
		case $1 in
		--context) context=${2:-}; shift 2 ;;
		--namespace) namespace=${2:-}; shift 2 ;;
		--env-file) env_file=${2:-}; shift 2 ;;
		*) die "unknown argument: $1" ;;
		esac
	done
	[ -n "$context" ] || die "--context is required (the current kubectl context is never assumed)"
	[ -n "$namespace" ] || die "--namespace is required"
	require_openssl3

	umask 077
	mkdir -p "$(dirname "$env_file")"
	touch "$env_file"
	chmod 600 "$env_file"
	fill_env "$env_file"
	warn_unset "$env_file"

	tmp=$(mktemp -d)
	trap 'rm -rf "${tmp:-}"' EXIT

	"$KUBECTL" --context "$context" create namespace "$namespace" --dry-run=client -o yaml |
		"$KUBECTL" --context "$context" apply -f - >/dev/null

	grep -E '^(POSTGRES_PASSWORD|[A-Z]+_DB_PASSWORD)=' "$env_file" >"$tmp/shogun-postgres.env"
	apply_secret "$context" "$namespace" shogun-postgres "$tmp/shogun-postgres.env"
	for svc in $SERVICES; do
		service_env "$env_file" "$tmp/shogun-$svc.env" "$svc"
		apply_secret "$context" "$namespace" "shogun-$svc" "$tmp/shogun-$svc.env"
	done
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
	main "$@"
fi
