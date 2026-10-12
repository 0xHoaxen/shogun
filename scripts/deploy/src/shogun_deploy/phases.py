"""The install order: helmfile phases with the plain manifests applied between them."""

from dataclasses import dataclass

from shogun_deploy import console
from shogun_deploy.runner import Runner
from shogun_deploy.settings import Target


@dataclass(frozen=True)
class Phase:
    name: str
    title: str
    # "helmfile": the releases labelled phase=<name>; "secrets" and "ingress":
    # kubectl apply of the plain manifests.
    kind: str


PHASES = (
    Phase("bootstrap", "namespaces and the sealed-secrets controller", "helmfile"),
    Phase("secrets", "sealed Secrets", "secrets"),
    Phase("platform", "cert-manager and monitoring", "helmfile"),
    Phase("ingress", "ClusterIssuers and the k3s Traefik redirect", "ingress"),
    Phase("data", "Postgres", "helmfile"),
    Phase("apps", "the services and the web app", "helmfile"),
)
NAMES = tuple(p.name for p in PHASES)
HELMFILE_PHASES = tuple(p.name for p in PHASES if p.kind == "helmfile")


class SecretsMissing(Exception):
    """No sealed Secrets are committed for the environment yet."""


def select(start: str | None) -> list[Phase]:
    """Every phase from start (or the first) to the end."""
    if start is None:
        return list(PHASES)
    if start not in NAMES:
        raise ValueError(f"--from must be one of {', '.join(NAMES)}")
    return list(PHASES[NAMES.index(start) :])


def helmfile_argv(
    target: Target,
    selectors: list[str],
    command: list[str],
    tags: dict[str, str] | None = None,
) -> list[str]:
    argv = [
        target.tools.helmfile,
        "--file", str(target.helmfile_path),
        "--environment", target.env,
        "--kube-context", target.context,
    ]  # fmt: skip
    for selector in selectors:
        argv += ["--selector", selector]
    for app, tag in sorted((tags or {}).items()):
        argv += ["--state-values-set", f"tags.{app}={tag}"]
    return argv + command


def apply_helmfile(
    target: Target,
    runner: Runner,
    phase: Phase,
    tags: dict[str, str],
    releases: list[str] | None = None,
) -> None:
    # One release at a time: a small node spikes when charts install in parallel.
    selectors = [f"name={r}" for r in releases] if releases else [f"phase={phase.name}"]
    argv = helmfile_argv(
        target,
        selectors,
        ["apply", "--concurrency", "1", "--suppress-diff"],
        tags if phase.name == "apps" else None,
    )
    runner.must(argv, f"helmfile apply phase {phase.name}")


def apply_secrets(target: Target, runner: Runner) -> None:
    if not (target.sealed_dir / "kustomization.yaml").exists():
        raise SecretsMissing(str(target.sealed_dir))
    argv = [
        target.tools.kubectl,
        "--context",
        target.context,
        "apply",
        "-k",
        str(target.sealed_dir),
    ]
    runner.must(argv, "kubectl apply the sealed Secrets")


def apply_ingress(target: Target, runner: Runner) -> None:
    for path, what in (
        (target.infra_dir / "cert-manager" / "issuers", "the ClusterIssuers"),
        (target.infra_dir / "traefik" / "k3s-helmchartconfig.yaml", "the Traefik redirect"),
    ):
        argv = [target.tools.kubectl, "--context", target.context, "apply", "-f", str(path)]
        runner.must(argv, f"kubectl apply {what}")


def run(
    target: Target,
    runner: Runner,
    phases: list[Phase],
    tags: dict[str, str],
    releases: list[str] | None = None,
) -> None:
    """Applies the phases in order. Raises StepFailed or SecretsMissing on the first problem."""
    for index, phase in enumerate(phases, 1):
        console.step(index, len(phases), f"{phase.name}: {phase.title}")
        if phase.kind == "helmfile":
            apply_helmfile(target, runner, phase, tags, releases)
        elif phase.kind == "secrets":
            apply_secrets(target, runner)
        else:
            apply_ingress(target, runner)
