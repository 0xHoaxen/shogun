#!/usr/bin/env bash
# Seals the Secrets a cluster needs with kubeseal, so they can be committed:
# postgres (read by deploy/helm/apps/postgres) and <service> for the ten services
# (read by the Helm chart with envFrom), in namespace shogun, and grafana-admin
# (Grafana's login, read by the monitoring chart) in namespace monitoring. With
# GHCR_USER and GHCR_TOKEN set it also seals the pull secret "ghcr" (a token with
# read:packages) for private ghcr.io images.
#
# Usage:
#   scripts/seal-secrets.sh --fetch-cert  --context <ctx> --env <env>
#   scripts/seal-secrets.sh --backup-key  --context <ctx> --env <env>
#   scripts/seal-secrets.sh --env <env> [--env-file <file>] [--cert <file>] [--only <name>]...
#
# --fetch-cert writes the controller's public certificate to
# deploy/k8s/<env>/sealed-secrets.pem (safe to commit). --backup-key saves the
# controller's private key to .secrets/<env>-sealed-secrets-key.yaml (never commit
# it): without it, a rebuilt cluster cannot decrypt what is committed.
# Sealing needs only the certificate, not the cluster, and writes
# deploy/k8s/<env>/secrets/<name>.yaml plus a kustomization.yaml that lists them.
# scripts/platform.sh applies that directory.
#
# Values come from the env file (default .secrets/<env>.env, git-ignored, KEY=value
# lines, no quoting). Anything generated is written back to that file, so a second
# run keeps every value: Postgres creates its roles once, so passwords must not
# move. kubeseal encrypts with a fresh random nonce, so a rerun rewrites every
# sealed file; use --only to touch one. See deploy/k8s/secrets.example.env for what
# you supply yourself. Values are never printed, only key and Secret names.
set -euo pipefail

SERVICES="torii kagami tsubame fude taiko dojo katana shinobi sensei soroban"
KUBECTL="${KUBECTL:-kubectl}"
KUBESEAL="${KUBESEAL:-kubeseal}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP_NAMESPACE="shogun"
MONITORING_NAMESPACE="monitoring"
# kubeseal's own defaults; scripts/platform.sh installs the controller under these.
CONTROLLER_NAME="sealed-secrets-controller"
CONTROLLER_NAMESPACE="kube-system"
GRAFANA_ADMIN_USER="admin"
PLACEHOLDER_ID="replace-me.apps.googleusercontent.com"
PLACEHOLDER_SECRET="replace-me"
PLACEHOLDER_EMAIL="you@example.com"
SEED_BYTES=32

die() {
	echo "seal-secrets: $*" >&2
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
	env_set_default "$file" GRAFANA_ADMIN_PASSWORD "$(openssl rand -hex 24)"
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
		replace-me*) echo "seal-secrets: $key is a placeholder; the service starts but Google sign-in or Gmail will not work" >&2 ;;
		esac
	done
	if [ "$(env_get "$file" TORII_ALLOWED_EMAILS)" = "$PLACEHOLDER_EMAIL" ]; then
		echo "seal-secrets: TORII_ALLOWED_EMAILS is a placeholder; nobody can sign in" >&2
	fi
	if [ -z "$(env_get "$file" GHCR_TOKEN)" ]; then
		echo "seal-secrets: GHCR_TOKEN is not set; no pull secret is created, so private ghcr.io images cannot be pulled" >&2
	fi
	for key in ANTHROPIC_API_KEY KATANA_GITHUB_USER KATANA_GITHUB_TOKEN; do
		if [ -z "$(env_get "$file" "$key")" ]; then
			echo "seal-secrets: $key is not set; the services that use it start, but drafts or GitHub sync will fail" >&2
		fi
	done
}

# seal_secret CERT OUTDIR NAMESPACE NAME FILE seals one Secret built from an env
# file. The file is written whole or not at all, so a failed run keeps the old one.
seal_secret() {
	local cert=$1 outdir=$2 namespace=$3 name=$4 file=$5 keys
	keys=$(grep -c '=' "$file" || true)
	"$KUBECTL" create secret generic "$name" --namespace "$namespace" \
		--from-env-file="$file" --dry-run=client -o yaml |
		"$KUBESEAL" --cert "$cert" --scope strict --format yaml >"$outdir/$name.yaml.new"
	mv "$outdir/$name.yaml.new" "$outdir/$name.yaml"
	echo "sealed $name in $namespace ($keys keys)"
}

# seal_pull_secret CERT OUTDIR USER TOKEN TMPDIR seals the ghcr.io pull secret from
# a dockerconfigjson file, so the token never appears on a command line.
seal_pull_secret() {
	local cert=$1 outdir=$2 user=$3 token=$4 tmp=$5 auth
	case "$user$token" in
	*[\"\\]*) die "GHCR_USER and GHCR_TOKEN must not contain quotes or backslashes" ;;
	esac
	auth=$(printf '%s:%s' "$user" "$token" | base64 | tr -d '\n')
	printf '{"auths":{"ghcr.io":{"username":"%s","password":"%s","auth":"%s"}}}' \
		"$user" "$token" "$auth" >"$tmp/dockerconfigjson"
	"$KUBECTL" create secret generic ghcr --namespace "$APP_NAMESPACE" \
		--type=kubernetes.io/dockerconfigjson \
		--from-file=.dockerconfigjson="$tmp/dockerconfigjson" --dry-run=client -o yaml |
		"$KUBESEAL" --cert "$cert" --scope strict --format yaml >"$outdir/ghcr.yaml.new"
	mv "$outdir/ghcr.yaml.new" "$outdir/ghcr.yaml"
	echo "sealed ghcr in $APP_NAMESPACE (pull secret for ghcr.io)"
}

# service_env SRC DEST SERVICE writes the variables for one service Secret.
service_env() {
	local src=$1 dest=$2 svc=$3 key value
	{
		echo "DATABASE_URL=postgres://$svc:$(env_get "$src" "$(upper "$svc")_DB_PASSWORD")@postgres:5432/shogun"
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

# write_kustomization OUTDIR lists every sealed file in the directory.
write_kustomization() {
	local outdir=$1 file
	{
		echo "# Written by scripts/seal-secrets.sh. Applied by scripts/platform.sh."
		echo "apiVersion: kustomize.config.k8s.io/v1beta1"
		echo "kind: Kustomization"
		echo "resources:"
		for file in "$outdir"/*.yaml; do
			[ "$(basename "$file")" = kustomization.yaml ] && continue
			echo "  - $(basename "$file")"
		done
	} >"$outdir/kustomization.yaml"
}

# wanted NAME reports whether NAME is selected: all, or one of the --only names.
wanted() {
	local name=$1 only
	[ "${#ONLY[@]}" -eq 0 ] && return 0
	for only in "${ONLY[@]}"; do
		[ "$only" = "$name" ] && return 0
	done
	return 1
}

# fetch_cert CONTEXT CERT writes the controller's public certificate.
fetch_cert() {
	local context=$1 cert=$2
	mkdir -p "$(dirname "$cert")"
	"$KUBESEAL" --context "$context" --controller-name "$CONTROLLER_NAME" \
		--controller-namespace "$CONTROLLER_NAMESPACE" --fetch-cert >"$cert.new"
	[ -s "$cert.new" ] || die "the controller returned no certificate"
	mv "$cert.new" "$cert"
	echo "wrote $cert (public, safe to commit)"
}

# backup_key CONTEXT FILE saves the controller's private key, readable by you only.
backup_key() {
	local context=$1 file=$2
	mkdir -p "$(dirname "$file")"
	"$KUBECTL" --context "$context" -n "$CONTROLLER_NAMESPACE" get secret \
		-l sealedsecrets.bitnami.com/sealed-secrets-key -o yaml >"$file.new"
	[ -s "$file.new" ] || die "no sealing key found in $CONTROLLER_NAMESPACE"
	chmod 600 "$file.new"
	mv "$file.new" "$file"
	echo "wrote $file (private: keep it out of git, store it safely)"
}

# seal_all ENV ENV_FILE CERT seals the selected Secrets into deploy/k8s/<env>/secrets.
seal_all() {
	local env=$1 env_file=$2 cert=$3 outdir="$ROOT/deploy/k8s/$1/secrets" tmp svc ghcr_user ghcr_token
	[ -f "$cert" ] || die "no certificate at $cert (run --fetch-cert first)"
	require_openssl3

	umask 077
	mkdir -p "$(dirname "$env_file")" "$outdir"
	touch "$env_file"
	chmod 600 "$env_file"
	fill_env "$env_file"
	warn_unset "$env_file"

	tmp=$(mktemp -d)
	trap 'rm -rf "${tmp:-}"' EXIT

	if wanted postgres; then
		grep -E '^(POSTGRES_PASSWORD|[A-Z]+_DB_PASSWORD)=' "$env_file" >"$tmp/postgres.env"
		seal_secret "$cert" "$outdir" "$APP_NAMESPACE" postgres "$tmp/postgres.env"
	fi
	for svc in $SERVICES; do
		wanted "$svc" || continue
		service_env "$env_file" "$tmp/$svc.env" "$svc"
		seal_secret "$cert" "$outdir" "$APP_NAMESPACE" "$svc" "$tmp/$svc.env"
	done
	if wanted grafana-admin; then
		printf 'admin-user=%s\nadmin-password=%s\n' "$GRAFANA_ADMIN_USER" "$(env_get "$env_file" GRAFANA_ADMIN_PASSWORD)" >"$tmp/grafana-admin.env"
		seal_secret "$cert" "$outdir" "$MONITORING_NAMESPACE" grafana-admin "$tmp/grafana-admin.env"
	fi
	ghcr_user=$(env_get "$env_file" GHCR_USER)
	ghcr_token=$(env_get "$env_file" GHCR_TOKEN)
	if wanted ghcr && [ -n "$ghcr_user" ] && [ -n "$ghcr_token" ]; then
		seal_pull_secret "$cert" "$outdir" "$ghcr_user" "$ghcr_token" "$tmp"
	fi
	write_kustomization "$outdir"
}

main() {
	local context="" env="" env_file="" cert="" action=seal
	ONLY=()
	while [ $# -gt 0 ]; do
		case $1 in
		--context) context=${2:-}; shift 2 ;;
		--env) env=${2:-}; shift 2 ;;
		--env-file) env_file=${2:-}; shift 2 ;;
		--cert) cert=${2:-}; shift 2 ;;
		--only) [ -n "${2:-}" ] || die "--only needs a Secret name"; ONLY+=("$2"); shift 2 ;;
		--fetch-cert) action=fetch-cert; shift ;;
		--backup-key) action=backup-key; shift ;;
		*) die "unknown argument: $1" ;;
		esac
	done
	case $env in
	staging | production) ;;
	*) die "--env must be staging or production" ;;
	esac
	[ -n "$env_file" ] || env_file=".secrets/$env.env"
	[ -n "$cert" ] || cert="$ROOT/deploy/k8s/$env/sealed-secrets.pem"

	case $action in
	fetch-cert | backup-key)
		[ -n "$context" ] || die "--context is required (the current kubectl context is never assumed)"
		if [ "$action" = fetch-cert ]; then
			fetch_cert "$context" "$cert"
		else
			backup_key "$context" ".secrets/$env-sealed-secrets-key.yaml"
		fi
		;;
	seal) seal_all "$env" "$env_file" "$cert" ;;
	esac
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
	main "$@"
fi
