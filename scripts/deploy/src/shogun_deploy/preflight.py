"""Checks before touching a cluster: tools, the helm-diff plugin, the context."""

from dataclasses import dataclass

from shogun_deploy import console
from shogun_deploy.runner import Runner
from shogun_deploy.settings import Target

REQUEST_TIMEOUT = "10s"


@dataclass(frozen=True)
class Check:
    name: str
    ok: bool
    detail: str


def _first_line(lines: tuple[str, ...]) -> str:
    return next((line.strip() for line in lines if line.strip()), "")


def _tool(runner: Runner, name: str, argv: list[str]) -> Check:
    result = runner.run(argv, f"checking {name}", capture=True)
    return Check(name, result.ok, _first_line(result.output) or f"exit {result.code}")


def checks(target: Target, runner: Runner) -> list[Check]:
    tools = target.tools
    found = [
        _tool(runner, "helmfile", [tools.helmfile, "--version"]),
        _tool(runner, "helm", [tools.helm, "version", "--short"]),
        _tool(runner, "kubectl", [tools.kubectl, "version", "--client"]),
    ]
    plugins = runner.run([tools.helm, "plugin", "list"], "checking helm-diff", capture=True)
    has_diff = plugins.ok and any(line.split()[:1] == ["diff"] for line in plugins.output)
    found.append(Check("helm-diff", has_diff, "installed" if has_diff else "run make tools"))

    contexts = runner.run(
        [tools.kubectl, "config", "get-contexts", "-o", "name"], "listing contexts", capture=True
    )
    known = target.context in {line.strip() for line in contexts.output}
    found.append(
        Check("context", known, target.context if known else f"no context {target.context!r}")
    )
    if known:
        ready = runner.run(
            [tools.kubectl, "--context", target.context, "--request-timeout", REQUEST_TIMEOUT,
             "get", "--raw", "/readyz"],
            "reaching the cluster",
            capture=True,
        )  # fmt: skip
        found.append(Check("cluster", ready.ok, "ready" if ready.ok else _first_line(ready.output)))
    return found


def report(found: list[Check]) -> bool:
    console.table(
        "Preflight",
        ["check", "result", "detail"],
        [[c.name, "[green]ok[/]" if c.ok else "[red]fail[/]", c.detail] for c in found],
    )
    return all(c.ok for c in found)
