#!/usr/bin/env bash
# Installs or upgrades the cluster-wide pieces the app releases rely on, in the
# order of helmfile.yaml.gotmpl: phase bootstrap (namespaces and quotas, the
# sealed-secrets controller), the sealed Secrets, phase platform (cert-manager,
# kube-prometheus-stack, Shogun's rules and dashboard), then the ClusterIssuers
# and the k3s Traefik redirect. Run it once per cluster before scripts/deploy.sh,
# whose ServiceMonitors need the Prometheus Operator CRDs, and again after
# changing a version or a value in deploy/infrastructure.
#
# Usage: scripts/platform.sh --context <ctx> --env <staging|production>
#
# The first run on a new cluster stops after the controller (exit 3): seal the
# Secrets with scripts/seal-secrets.sh, commit them, run it again. Grafana reads
# its login from the sealed Secret grafana-admin. Written for k3s, which bundles
# Traefik.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HELMFILE="${HELMFILE:-$ROOT/bin/helmfile}"
KUBECTL="${KUBECTL:-kubectl}"
INFRA="$ROOT/deploy/infrastructure"
EXIT_NOT_SEALED=3

die() {
	echo "platform: $*" >&2
	exit 2
}

# phase CONTEXT ENV NAME applies the releases labelled phase=NAME, one at a time.
phase() {
	local context=$1 env=$2 name=$3
	"$HELMFILE" --file "$ROOT/helmfile.yaml.gotmpl" --environment "$env" --kube-context "$context" \
		--selector "phase=$name" apply --concurrency 1 --suppress-diff
	echo "applied phase $name"
}

# apply_secrets CONTEXT ENV applies the sealed Secrets, or stops with the steps to
# create them when none are committed yet.
apply_secrets() {
	local context=$1 env=$2 dir="$INFRA/sealed-secrets/secrets/$2"
	if [ ! -f "$dir/kustomization.yaml" ]; then
		cat >&2 <<EOF
platform: no sealed Secrets in deploy/infrastructure/sealed-secrets/secrets/$env yet. The controller is installed; now:
  scripts/seal-secrets.sh --env $env --fetch-cert --context $context
  scripts/seal-secrets.sh --env $env --backup-key --context $context
  scripts/seal-secrets.sh --env $env
  git add deploy/infrastructure/sealed-secrets && git commit, then run this script again.
EOF
		exit "$EXIT_NOT_SEALED"
	fi
	"$KUBECTL" --context "$context" apply -k "$dir" >/dev/null
	echo "applied the sealed Secrets"
}

# apply_ingress CONTEXT applies the ClusterIssuers (they need cert-manager's
# webhook) and the k3s Traefik redirect.
apply_ingress() {
	local context=$1
	"$KUBECTL" --context "$context" apply -f "$INFRA/cert-manager/issuers" >/dev/null
	"$KUBECTL" --context "$context" apply -f "$INFRA/traefik/k3s-helmchartconfig.yaml" >/dev/null
	echo "applied the ClusterIssuers and the Traefik redirect"
}

main() {
	local context="" env=""
	while [ $# -gt 0 ]; do
		case $1 in
		--context) context=${2:-}; shift 2 ;;
		--env) env=${2:-}; shift 2 ;;
		*) die "unknown argument: $1" ;;
		esac
	done
	[ -n "$context" ] || die "--context is required (never installs to the current context by default)"
	case $env in
	staging | production) ;;
	*) die "--env must be staging or production" ;;
	esac

	phase "$context" "$env" bootstrap
	apply_secrets "$context" "$env"
	phase "$context" "$env" platform
	apply_ingress "$context"
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
	main "$@"
fi
