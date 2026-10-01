#!/usr/bin/env bash
# Render scripts/templates/service into a directory.
# Usage: scripts/render-service.sh NAME DEST
set -euo pipefail

name="${1:?usage: render-service.sh NAME DEST}"
dest="${2:?usage: render-service.sh NAME DEST}"
root="$(cd "$(dirname "$0")/.." && pwd)"
template="$root/scripts/templates/service"

if [[ ! "$name" =~ ^[a-z][a-z0-9]*$ ]]; then
	echo "render-service: NAME must match ^[a-z][a-z0-9]*\$, got '$name'" >&2
	exit 1
fi
if [[ -e "$dest" ]]; then
	echo "render-service: $dest already exists" >&2
	exit 1
fi

while IFS= read -r src; do
	rel="${src#"$template"/}"
	rel="${rel//__NAME__/$name}"
	out="$dest/${rel%.tmpl}"
	mkdir -p "$(dirname "$out")"
	sed "s/__NAME__/$name/g" "$src" >"$out"
done < <(find "$template" -type f -name '*.tmpl' | sort)
