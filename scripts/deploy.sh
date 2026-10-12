#!/usr/bin/env bash
# Installs or upgrades the whole stack on a cluster from the registry: Postgres,
# then soroban, then the other services, then the web app.
#
# Usage: scripts/deploy.sh --context <ctx> --namespace <ns> --env <staging|production>
#                          [--versions-file <file>]
#
# Every app is installed with deploy/charts/microservice from the checkout, with
# deploy/apps/common.yaml, deploy/apps/<name>/values.yaml and
# values-<env>.yaml. The image tag is image.tag in values-<env>.yaml, so run this
# from a checkout of the git ref you are deploying. A versions file (KAGAMI=1.2.3
# lines, upper-case service name, WEB for the web app) overrides it.
#
# Postgres is installed from deploy/helm/apps/postgres. Create the Secrets with
# scripts/seal-secrets.sh first; it also seals the "ghcr" pull secret that
# deploy/apps/common.yaml names.
set -euo pipefail

SERVICES="soroban kagami fude tsubame taiko dojo katana shinobi sensei torii"
HELM="${HELM:-helm}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APPS_DIR="$ROOT/deploy/apps"
CHART="$ROOT/deploy/charts/microservice"
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

# resolve_version ENV VERSIONS_FILE NAME prints the version to deploy.
resolve_version() {
	local env=$1 versions_file=$2 name=$3 version=""
	if [ -n "$versions_file" ]; then
		version=$(versions_get "$versions_file" "$(upper "$name")")
	fi
	if [ -z "$version" ]; then
		version=$(values_tag "$APPS_DIR/$name/values-$env.yaml")
	fi
	[ -n "$version" ] || die "no version for $name: set image.tag in deploy/apps/$name/values-$env.yaml or $(upper "$name") in the versions file"
	printf '%s' "$version"
}

# install_postgres CONTEXT NAMESPACE VALUES_DIR installs the chart from the checkout.
install_postgres() {
	local context=$1 namespace=$2 values_dir=$3
	local args=(--kube-context "$context" --namespace "$namespace" --wait --timeout "$POSTGRES_TIMEOUT")
	if [ -f "$values_dir/postgres.yaml" ]; then
		args+=(-f "$values_dir/postgres.yaml")
	fi
	"$HELM" upgrade --install postgres "$ROOT/deploy/helm/apps/postgres" "${args[@]}"
	echo "installed postgres (local chart)"
}

# install_app CONTEXT NAMESPACE ENV NAME VERSION installs one app with the shared chart.
install_app() {
	local context=$1 namespace=$2 env=$3 name=$4 version=$5
	"$HELM" upgrade --install "$name" "$CHART" \
		--kube-context "$context" --namespace "$namespace" \
		-f "$APPS_DIR/common.yaml" -f "$APPS_DIR/$name/values.yaml" -f "$APPS_DIR/$name/values-$env.yaml" \
		--set "image.tag=$version" \
		--wait --timeout "$SERVICE_TIMEOUT"
	echo "installed $name $version"
}

main() {
	local context="" namespace="" env="" versions_file=""
	while [ $# -gt 0 ]; do
		case $1 in
		--context) context=${2:-}; shift 2 ;;
		--namespace) namespace=${2:-}; shift 2 ;;
		--env) env=${2:-}; shift 2 ;;
		--versions-file) versions_file=${2:-}; shift 2 ;;
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
		[ -f "$APPS_DIR/$name/values-$env.yaml" ] || die "missing deploy/apps/$name/values-$env.yaml"
		version=$(resolve_version "$env" "$versions_file" "$name")
		plan+=("$name=$version")
	done

	install_postgres "$context" "$namespace" "$values_dir"
	for entry in "${plan[@]}"; do
		install_app "$context" "$namespace" "$env" "${entry%%=*}" "${entry#*=}"
	done
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
	main "$@"
fi
