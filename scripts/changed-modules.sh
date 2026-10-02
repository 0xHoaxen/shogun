#!/usr/bin/env bash
# Print the workspace modules affected by a change as a JSON matrix for CI.
# Usage: scripts/changed-modules.sh <base-ref>
#   {"dir":["services/kagami"]}
# A change under pkg/, gen/, proto/ or to go.work, go.work.sum, .golangci.yml or
# .tool-versions selects every module. CHANGED_FILES (newline separated) replaces
# the git diff, which is how the test drives it.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

base="${1:?usage: changed-modules.sh <base-ref>}"

if [[ -n "${CHANGED_FILES+x}" ]]; then
	changed="$CHANGED_FILES"
else
	merge_base="$(git merge-base "$base" HEAD)"
	changed="$(git diff --name-only "$merge_base" HEAD)"
fi

# Module directories relative to the repo root, in workspace order.
modules=()
while IFS= read -r dir; do
	modules+=("${dir#"$root"/}")
done < <(go list -m -f '{{.Dir}}')

selected=()
select_all() { selected=("${modules[@]}"); }

while IFS= read -r file; do
	[[ -z "$file" ]] && continue
	case "$file" in
	pkg/* | gen/* | proto/* | go.work | go.work.sum | .golangci.yml | .tool-versions)
		select_all
		break
		;;
	esac
	for module in "${modules[@]}"; do
		if [[ "$file" == "$module"/* ]] && [[ " ${selected[*]-} " != *" $module "* ]]; then
			selected+=("$module")
		fi
	done
done <<<"$changed"

json="" sep=""
# Emit in workspace order so the output is stable.
for module in "${modules[@]}"; do
	[[ " ${selected[*]-} " == *" $module "* ]] || continue
	json+="$sep\"$module\""
	sep=","
done
printf '{"dir":[%s]}\n' "$json"
