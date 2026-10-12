"""Day-two commands: what is running, and a backup on demand."""

import json
from datetime import UTC, datetime

from shogun_deploy import console
from shogun_deploy.runner import Runner
from shogun_deploy.settings import Target

NAMESPACES = ("shogun", "monitoring", "cert-manager", "kube-system")
BACKUP_CRONJOB = "postgres-backup"
BACKUP_TIMEOUT = "15m"


def _json(runner: Runner, argv: list[str], title: str) -> dict:
    result = runner.must(argv, title, capture=True)
    return json.loads("\n".join(result.output) or "{}")


def status(target: Target, runner: Runner) -> None:
    kubectl = [target.tools.kubectl, "--context", target.context]
    releases = runner.must(
        [target.tools.helm, "--kube-context", target.context, "list", "--all-namespaces",
         "--output", "json"],
        "helm list",
        capture=True,
    )  # fmt: skip
    console.table(
        f"Releases on {target.context}",
        ["release", "namespace", "chart", "app version", "status", "updated"],
        [
            [r["name"], r["namespace"], r["chart"], r.get("app_version", ""), r["status"],
             r.get("updated", "")[:19]]
            for r in json.loads("\n".join(releases.output) or "[]")
        ],
    )  # fmt: skip

    rows = []
    for namespace in NAMESPACES:
        pods = _json(
            runner, [*kubectl, "-n", namespace, "get", "pods", "-o", "json"], f"pods in {namespace}"
        )
        for pod in pods.get("items", []):
            phase = pod["status"].get("phase", "")
            ready = all(c.get("ready") for c in pod["status"].get("containerStatuses", []))
            if phase != "Succeeded" and not (phase == "Running" and ready):
                rows.append([namespace, pod["metadata"]["name"], phase or "Unknown"])
    if rows:
        console.table("Pods not ready", ["namespace", "pod", "phase"], rows)
    else:
        console.ok("every pod is running and ready")

    jobs = _json(
        runner,
        [
            *kubectl,
            "-n",
            "shogun",
            "get",
            "jobs",
            "-l",
            "app.kubernetes.io/component=backup",
            "-o",
            "json",
        ],
        "backup jobs",
    )
    finished = sorted(
        (
            j
            for j in jobs.get("items", [])
            if j["status"].get("completionTime") or j["status"].get("failed")
        ),
        key=lambda j: j["metadata"]["creationTimestamp"],
    )
    if not finished:
        console.warn("no backup has run yet")
    else:
        last = finished[-1]
        when = last["metadata"]["creationTimestamp"]
        if last["status"].get("succeeded"):
            console.ok(f"last backup {when} succeeded")
        else:
            console.warn(
                f"last backup {when} failed: kubectl -n shogun logs job/{last['metadata']['name']}"
            )


def backup_now(target: Target, runner: Runner) -> None:
    kubectl = [target.tools.kubectl, "--context", target.context, "-n", "shogun"]
    job = f"backup-now-{datetime.now(UTC).strftime('%Y%m%d%H%M%S')}"
    runner.must(
        [*kubectl, "create", "job", f"--from=cronjob/{BACKUP_CRONJOB}", job], f"create job {job}"
    )
    runner.must(
        [*kubectl, "wait", "--for=condition=complete", f"job/{job}", f"--timeout={BACKUP_TIMEOUT}"],
        "wait for the backup",
    )
    logs = runner.must([*kubectl, "logs", f"job/{job}"], "read the backup log", capture=True)
    for line in logs.output:
        if "wrote" in line:
            console.ok(line.split("backup: ", 1)[-1])
