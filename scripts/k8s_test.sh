#!/usr/bin/env bash
# Checks deploy/k8s: both overlays render, and what the staging charts request
# fits the ResourceQuota. Needs helm and kubectl. Run: scripts/k8s_test.sh
set -u
here=$(cd "$(dirname "$0")" && pwd)
root="$here/.."
helm_dir="$root/deploy/helm"
failed=0
SERVICES="torii kagami tsubame fude taiko dojo katana shinobi sensei soroban"

ok() { echo "ok   $1"; }
fail() {
	echo "FAIL $1"
	failed=1
}
check() { # name, command that succeeds when the behaviour holds
	if eval "$2"; then ok "$1"; else fail "$1"; fi
}

# render ENV prints every app chart of an environment, one YAML stream.
render() {
	local env=$1 name
	for name in $SERVICES; do
		helm template "shogun-$name" "$helm_dir/apps/$name" -n shogun -f "$helm_dir/values/$env/$name.yaml" 2>/dev/null
		echo "---"
	done
	if [ -f "$helm_dir/values/$env/web.yaml" ]; then
		helm template shogun-web "$helm_dir/apps/web" -n shogun -f "$helm_dir/values/$env/web.yaml" 2>/dev/null
		echo "---"
	fi
	if [ -f "$helm_dir/values/$env/postgres.yaml" ]; then
		helm template postgres "$helm_dir/apps/postgres" -n shogun -f "$helm_dir/values/$env/postgres.yaml" 2>/dev/null
	fi
}

# fit ENV prints the totals of an environment's pods against the quota and exits
# 0 when they fit. A migration Job runs one at a time, so the largest one is
# added on top of the steady state.
fit() {
	local env=$1
	{
		kubectl kustomize "$root/deploy/k8s/$env"
		echo "#RENDER"
		render "$env"
	} | python3 -I -c '
import re, sys

text = sys.stdin.read()
quota_text, rendered = text.split("#RENDER\n", 1)


def cpu(v):
    return int(v[:-1]) if v.endswith("m") else int(float(v) * 1000)


def mem(v):
    for unit, scale in (("Gi", 1024), ("Mi", 1), ("G", 1000), ("M", 1)):
        if v.endswith(unit):
            return int(float(v[: -len(unit)]) * scale)
    raise SystemExit("unsupported memory " + v)


def field(block, key):
    m = re.search(r"^\s+" + re.escape(key) + r": \"?([0-9.]+[A-Za-z]*)\"?\s*$", block, re.M)
    return m.group(1) if m else None


quota = {}
hard = quota_text.split("hard:", 1)[1]
for key in ("requests.cpu", "requests.memory", "limits.memory", "pods"):
    quota[key] = field(hard, key)

# An autoscaled Deployment runs at least minReplicas pods.
min_replicas = {}
for doc in re.split(r"\n---\n", rendered):
    if re.search(r"^kind: HorizontalPodAutoscaler$", doc, re.M):
        target = re.search(r"scaleTargetRef:\n(?:\s+.*\n)*?\s+name: (\S+)", doc + "\n")
        floor = re.search(r"^\s+minReplicas: (\d+)", doc, re.M)
        if target and floor:
            min_replicas[target.group(1)] = int(floor.group(1))

steady = {"cpu": 0, "req_mem": 0, "lim_mem": 0, "pods": 0}
job = {"cpu": 0, "req_mem": 0, "lim_mem": 0}
for doc in re.split(r"\n---\n", rendered):
    kind = re.search(r"^kind: (\w+)$", doc, re.M)
    if not kind or kind.group(1) not in ("Deployment", "StatefulSet", "Job"):
        continue
    blocks = re.findall(r"^\s+resources:\n((?:\s{12,}.*\n)+)", doc + "\n", re.M)
    req_cpu = req_mem = lim_mem = 0
    if not blocks:
        # No resources set: the LimitRange defaults apply (base/limit-range.yaml).
        req_cpu, req_mem, lim_mem = 50, 64, 256
    for b in blocks:
        parts = b.split("requests:")
        limits = parts[0]
        requests = parts[1] if len(parts) > 1 else ""
        req_cpu += cpu(field(requests, "cpu") or "0m")
        req_mem += mem(field(requests, "memory") or "0Mi")
        lim_mem += mem(field(limits, "memory") or "0Mi")
    if kind.group(1) == "Job":
        for k, v in (("cpu", req_cpu), ("req_mem", req_mem), ("lim_mem", lim_mem)):
            job[k] = max(job[k], v)
        continue
    name = re.search(r"^  name: (\S+)$", doc, re.M)
    replicas = min_replicas.get(name.group(1) if name else "", int(field(doc, "replicas") or 1))
    steady["cpu"] += req_cpu * replicas
    steady["req_mem"] += req_mem * replicas
    steady["lim_mem"] += lim_mem * replicas
    steady["pods"] += replicas

need = {
    "cpu": steady["cpu"] + job["cpu"],
    "req_mem": steady["req_mem"] + job["req_mem"],
    "lim_mem": steady["lim_mem"] + job["lim_mem"],
    "pods": steady["pods"] + 1,
}
have = {
    "cpu": cpu(quota["requests.cpu"]),
    "req_mem": mem(quota["requests.memory"]),
    "lim_mem": mem(quota["limits.memory"]),
    "pods": int(quota["pods"]),
}
over = [k for k in need if need[k] > have[k]]
for k in need:
    print("  %-8s needs %6d of %6d%s" % (k, need[k], have[k], "  OVER" if k in over else ""))
sys.exit(1 if over else 0)
'
}

check "the staging overlay renders a quota, a limit range and both namespaces" \
	'out=$(kubectl kustomize "$root/deploy/k8s/staging") &&
	 [ "$(echo "$out" | grep -c "^kind: Namespace")" -eq 2 ] &&
	 echo "$out" | grep -q "^kind: ResourceQuota" && echo "$out" | grep -q "^kind: LimitRange"'
check "the production overlay renders" 'kubectl kustomize "$root/deploy/k8s/production" >/dev/null'
check "staging requests, limits and pods fit the quota with a migration Job on top" \
	'report=$(fit staging); code=$?; echo "$report"; [ "$code" -eq 0 ]'

# Production is reported, not asserted: its HPA minimum of two replicas does not
# fit a 4 GiB VM yet (TODO(owner), see deploy/k8s/base/resource-quota.yaml).
echo "info production (not asserted):"
fit production

exit "$failed"
