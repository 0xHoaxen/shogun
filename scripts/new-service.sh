#!/usr/bin/env bash
# Generate a service skeleton.
# Usage: scripts/new-service.sh NAME
set -euo pipefail

name="${1:?usage: new-service.sh NAME}"
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

if [[ -e "services/$name" || -e "proto/shogun/$name" ]]; then
	echo "new-service: $name already exists" >&2
	exit 1
fi

scripts/render-service.sh "$name" "services/$name"
go work use "./services/$name"
(cd "services/$name" && GOWORK=off go mod tidy)
python3 scripts/depguard.py add "$name"
python3 scripts/compose.py add "$name"

title="$(tr '[:lower:]' '[:upper:]' <<<"${name:0:1}")${name:1}"
proto_dir="proto/shogun/$name/v1"
mkdir -p "$proto_dir"
cat >"$proto_dir/$name.proto" <<PROTO
syntax = "proto3";

package shogun.$name.v1;

// The RPCs of $name are added with its first feature task.
service ${title}Service {}
PROTO

echo "new-service: created services/$name (helm entries arrive in P3.7)"
