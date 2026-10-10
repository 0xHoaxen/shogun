#!/usr/bin/env bash
# Installs or upgrades the whole stack on a cluster from the registry: Postgres,
# then soroban, then the other services, then the web app.
#
# Usage: scripts/deploy.sh --context <ctx> --namespace <ns> --env <staging|production>
#                          [--versions-file <file>] [--pull-secret <name>]
#
# Charts come from oci://ghcr.io/0xhoaxen/charts/shogun-<name> at the version to
# deploy. That version is the image.tag in deploy/helm/values/<env>/<name>.yaml,
# so run this from a checkout of the git ref you are deploying. A versions file
# (KAGAMI=1.2.3 lines, upper-case service name, WEB for the web app) overrides it.
#
# Postgres is not published to the registry: it is installed from deploy/helm/postgres
# in the checkout. Log in to the registry first (helm registry login ghcr.io) and
# create the Secrets with scripts/k8s-secrets.sh, which also creates the "ghcr" pull
# secret. --pull-secret "" turns the pull secret off (public packages).
set -euo pipefail

SERVICES="soroban kagami fude tsubame taiko dojo katana shinobi sensei torii"
HELM="${HELM:-helm}"
CHART_REGISTRY="oci://ghcr.io/0xhoaxen/charts"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEFAULT_PULL_SECRET="ghcr"
SERVICE_TIMEOUT="5m"
POSTGRES_TIMEOUT="3m"

die() {
	echo "deploy: $*" >&2
	exit 2
}

upper() { printf '%s' "$1" | tr '[:lower:]' '[:upper:]'; }

# values_tag FILE prints image.tag from a values file, or nothing.
values_tag() {
	awk '
		/^image:/ { in_image = 1; next }
		/^[^ #]/ { in_image = 0 }
		in_image && $1 == "tag:" { gsub(/["\047]/, "", $2); print $2; exit }
	' "$1"
}

# versions_get FILE KEY prints the last value of KEY in a versions file, or nothing.
versions_get() { sed -n "s/^$2=//p" "$1" | tail -n 1; }

# resolve_version VALUES_DIR VERSIONS_FILE NAME prints the version to deploy.
resolve_version() {
	local values_dir=$1 versions_file=$2 name=$3 version=""
	if [ -n "$versions_file" ]; then
		version=$(versions_get "$versions_file" "$(upper "$name")")
	fi
	if [ -z "$version" ]; then
		version=$(values_tag "$values_dir/$name.yaml")
	fi
	[ -n "$version" ] || die "no version for $name: set image.tag in $values_dir/$name.yaml or $(upper "$name") in the versions file"
	printf '%s' "$version"
}

# install_postgres CONTEXT NAMESPACE VALUES_DIR installs the chart from the checkout.
install_postgres() {
	local context=$1 namespace=$2 values_dir=$3
	local args=(--kube-context "$context" --namespace "$namespace" --wait --timeout "$POSTGRES_TIMEOUT")
	if [ -f "$values_dir/postgres.yaml" ]; then
		args+=(-f "$values_dir/postgres.yaml")
	fi
	"$HELM" upgrade --install shogun-postgres "$ROOT/deploy/helm/postgres" "${args[@]}"
	echo "installed shogun-postgres (local chart)"
}

# install_chart CONTEXT NAMESPACE VALUES_DIR NAME VERSION PULL_SECRET
install_chart() {
	local context=$1 namespace=$2 values_dir=$3 name=$4 version=$5 pull_secret=$6
	local args=(
		--version "$version"
		--kube-context "$context" --namespace "$namespace"
		-f "$values_dir/$name.yaml"
		--set "image.tag=$version"
		--wait --timeout "$SERVICE_TIMEOUT"
	)
	if [ -n "$pull_secret" ]; then
		args+=(--set "imagePullSecrets[0].name=$pull_secret")
	fi
	"$HELM" upgrade --install "shogun-$name" "$CHART_REGISTRY/shogun-$name" "${args[@]}"
	echo "installed shogun-$name $version"
}

main() {
	local context="" namespace="" env="" versions_file="" pull_secret="$DEFAULT_PULL_SECRET"
	while [ $# -gt 0 ]; do
		case $1 in
		--context) context=${2:-}; shift 2 ;;
		--namespace) namespace=${2:-}; shift 2 ;;
		--env) env=${2:-}; shift 2 ;;
		--versions-file) versions_file=${2:-}; shift 2 ;;
		--pull-secret) pull_secret=${2-}; shift 2 ;;
		*) die "unknown argument: $1" ;;
		esac
	done
	[ -n "$context" ] || die "--context is required (never deploys to the current context by default)"
	[ -n "$namespace" ] || die "--namespace is required"
	case $env in
	staging | production) ;;
	*) die "--env must be staging or production" ;;
	esac
	if [ -n "$versions_file" ] && [ ! -f "$versions_file" ]; then
		die "versions file not found: $versions_file"
	fi

	local values_dir="$ROOT/deploy/helm/values/$env" name version entry
	local -a plan=()

	# Resolve everything before the first install, so a missing file or version
	# stops the run while the cluster is still untouched.
	for name in $SERVICES web; do
		[ -f "$values_dir/$name.yaml" ] || die "missing $values_dir/$name.yaml"
		version=$(resolve_version "$values_dir" "$versions_file" "$name")
		plan+=("$name=$version")
	done

	install_postgres "$context" "$namespace" "$values_dir"
	for entry in "${plan[@]}"; do
		install_chart "$context" "$namespace" "$values_dir" "${entry%%=*}" "${entry#*=}" "$pull_secret"
	done
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
	main "$@"
fi
