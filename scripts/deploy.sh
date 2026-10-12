#!/usr/bin/env bash
# Installs or upgrades the data and apps of a cluster, in the order of
# helmfile.yaml.gotmpl: phase data (Postgres), then phase apps (soroban, the
# other services, web). Run scripts/platform.sh first.
#
# Usage: scripts/deploy.sh --context <ctx> --env <staging|production> [--versions-file <file>]
#
# The image tag of each app is image.tag in deploy/apps/<name>/values-<env>.yaml,
# so run this from a checkout of the git ref you are deploying. A versions file
# (KAGAMI=1.2.3 lines, upper-case app name, WEB for the web app) overrides it.
# An app without values-<env>.yaml is not part of that environment.
set -euo pipefail

APPS="soroban kagami fude tsubame taiko dojo katana shinobi sensei torii web"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HELMFILE="${HELMFILE:-$ROOT/bin/helmfile}"
APPS_DIR="$ROOT/deploy/apps"

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

# phase CONTEXT ENV NAME [ARGS...] applies the releases labelled phase=NAME.
phase() {
	local context=$1 env=$2 name=$3
	shift 3
	"$HELMFILE" --file "$ROOT/helmfile.yaml.gotmpl" --environment "$env" --kube-context "$context" \
		--selector "phase=$name" "$@" apply --concurrency 1 --suppress-diff
	echo "applied phase $name"
}

main() {
	local context="" env="" versions_file="" name file version override
	while [ $# -gt 0 ]; do
		case $1 in
		--context) context=${2:-}; shift 2 ;;
		--env) env=${2:-}; shift 2 ;;
		--versions-file) versions_file=${2:-}; shift 2 ;;
		*) die "unknown argument: $1" ;;
		esac
	done
	[ -n "$context" ] || die "--context is required (never deploys to the current context by default)"
	case $env in
	staging | production) ;;
	*) die "--env must be staging or production" ;;
	esac
	if [ -n "$versions_file" ] && [ ! -f "$versions_file" ]; then
		die "versions file not found: $versions_file"
	fi

	# Resolve every version before the first install, so a missing one stops the
	# run while the cluster is still untouched.
	local -a sets=()
	for name in $APPS; do
		file="$APPS_DIR/$name/values-$env.yaml"
		if [ ! -f "$file" ]; then
			echo "deploy: $name has no values-$env.yaml; it is not part of $env" >&2
			continue
		fi
		override=""
		[ -z "$versions_file" ] || override=$(versions_get "$versions_file" "$(upper "$name")")
		version=${override:-$(values_tag "$file")}
		[ -n "$version" ] || die "no version for $name: set image.tag in deploy/apps/$name/values-$env.yaml or $(upper "$name") in the versions file"
		[ -z "$override" ] || sets+=(--state-values-set "tags.$name=$override")
	done

	phase "$context" "$env" data
	phase "$context" "$env" apps "${sets[@]+"${sets[@]}"}"
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
	main "$@"
fi
