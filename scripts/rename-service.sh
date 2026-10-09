#!/usr/bin/env bash
# Rename a service everywhere it is referenced today.
# Usage: scripts/rename-service.sh OLD NEW
set -euo pipefail

old="${1:?usage: rename-service.sh OLD NEW}"
new="${2:?usage: rename-service.sh OLD NEW}"
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

name_pattern='^[a-z][a-z0-9]*$'
for n in "$old" "$new"; do
	if [[ ! "$n" =~ $name_pattern ]]; then
		echo "rename-service: '$n' must match $name_pattern" >&2
		exit 1
	fi
done
if [[ ! -d "services/$old" ]]; then
	echo "rename-service: services/$old does not exist" >&2
	exit 1
fi
if [[ -e "services/$new" || -e "proto/shogun/$new" ]]; then
	echo "rename-service: $new already exists" >&2
	exit 1
fi

# git mv keeps history for tracked paths; plain mv covers a service that was
# generated but not yet committed.
move() { git mv "$1" "$2" 2>/dev/null || mv "$1" "$2"; }

move "services/$old" "services/$new"
for env in staging production; do
	[[ -f "deploy/helm/values/$env/$old.yaml" ]] && move "deploy/helm/values/$env/$old.yaml" "deploy/helm/values/$env/$new.yaml"
done
[[ -d "deploy/helm/$old" ]] && move "deploy/helm/$old" "deploy/helm/$new"
move "services/$new/cmd/$old" "services/$new/cmd/$new"
if [[ -d "proto/shogun/$old" ]]; then
	move "proto/shogun/$old" "proto/shogun/$new"
	move "proto/shogun/$new/v1/$old.proto" "proto/shogun/$new/v1/$new.proto"
	[[ -d "gen/go/shogun/$old" ]] && move "gen/go/shogun/$old" "gen/go/shogun/$new"
fi

# Names are lowercase words, so a word-boundary rewrite covers module paths,
# the proto package, the schema, depguard rules and comments. The service
# prefix of environment variables is the upper-case name.
old_upper="$(tr '[:lower:]' '[:upper:]' <<<"$old")"
new_upper="$(tr '[:lower:]' '[:upper:]' <<<"$new")"
targets=()
for path in "services/$new" "proto/shogun/$new" "gen/go/shogun/$new" .golangci.yml go.work .env.example deploy; do
	[[ -e "$path" ]] && targets+=("$path")
done
find "${targets[@]}" -type f ! -name go.sum -exec perl -pi -e \
	"s/\\b${old}\\b/${new}/g; s/\\b${old_upper}_/${new_upper}_/g; s/\\b\u${old}Service\\b/\u${new}Service/g" {} +

(cd "services/$new" && GOWORK=off go mod tidy)
if [[ -d "proto/shogun/$new" ]]; then
	make proto
fi

echo "rename-service: $old -> $new"
