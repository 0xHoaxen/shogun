"""What an apply would do: the releases, their versions, and which phases would change."""

import json
from dataclasses import dataclass

from shogun_deploy import console
from shogun_deploy.phases import HELMFILE_PHASES, Phase, helmfile_argv
from shogun_deploy.runner import Runner, StepFailed
from shogun_deploy.settings import Target
from shogun_deploy.versions import AppVersion

# helmfile diff --detailed-exitcode: 0 no change, 2 changes, anything else an error.
NO_CHANGES, CHANGES = 0, 2


@dataclass(frozen=True)
class Release:
    name: str
    namespace: str
    chart: str
    version: str
    phase: str


def releases(target: Target, runner: Runner, versions: list[AppVersion]) -> list[Release]:
    result = runner.must(
        helmfile_argv(target, [], ["list", "--output", "json"]), "helmfile list", capture=True
    )
    tags = {v.app: v.tag + (" (--tag)" if v.overridden else "") for v in versions}
    found = []
    for item in json.loads("\n".join(result.output) or "[]"):
        labels = dict(
            pair.split(":", 1) for pair in item.get("labels", "").split(",") if ":" in pair
        )
        name = item["name"]
        found.append(
            Release(
                name=name,
                namespace=item.get("namespace", ""),
                chart=item.get("chart", ""),
                version=tags.get(name) or item.get("version", "") or "local",
                phase=labels.get("phase", ""),
            )
        )
    return found


def diff_phase(target: Target, runner: Runner, phase: str, tags: dict[str, str]) -> str:
    argv = helmfile_argv(
        target,
        [f"phase={phase}"],
        ["diff", "--detailed-exitcode", "--suppress-secrets", "--concurrency", "1"],
        tags if phase == "apps" else None,
    )
    result = runner.run(argv, f"diffing phase {phase}")
    if result.code == NO_CHANGES:
        return "no change"
    if result.code == CHANGES:
        return "changes"
    console.fail(f"helmfile diff phase {phase} (exit {result.code})")
    console.tail(list(result.output))
    console.info(f"full output: {runner.log_path}")
    raise StepFailed(f"diff {phase}")


def show(
    target: Target,
    runner: Runner,
    phases: list[Phase],
    versions: list[AppVersion],
    tags: dict[str, str],
) -> dict[str, str]:
    """Prints the plan table and returns the change per phase."""
    wanted = [p.name for p in phases if p.name in HELMFILE_PHASES]
    changes = {phase: diff_phase(target, runner, phase, tags) for phase in wanted}
    rows = [
        [r.phase, r.name, r.namespace, r.chart, r.version, _mark(changes.get(r.phase, "skipped"))]
        for r in releases(target, runner, versions)
        if r.phase in wanted
    ]
    rows.sort(key=lambda row: (HELMFILE_PHASES.index(row[0]), row[1]))
    console.table(
        f"Plan for {target.env} on {target.context} (details in {runner.log_path.name})",
        ["phase", "release", "namespace", "chart", "version", "phase diff"],
        rows,
    )
    return changes


def _mark(change: str) -> str:
    return {"changes": "[yellow]changes[/]", "no change": "[dim]no change[/]"}.get(change, change)
