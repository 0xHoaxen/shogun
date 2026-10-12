"""shogun-deploy: install and run Shogun on a cluster, one environment per cluster."""

from typing import Annotated

import typer

from shogun_deploy import cli_secrets, console, ops, phases, plan, preflight, seal
from shogun_deploy.runner import Runner, StepFailed
from shogun_deploy.settings import (
    ENVIRONMENTS,
    EXIT_FAILED,
    EXIT_USAGE,
    EXIT_WAITING,
    Target,
    Tools,
    repo_root,
)
from shogun_deploy.versions import AppVersion, VersionError, parse_overrides, resolve

app = typer.Typer(
    help=__doc__,
    no_args_is_help=True,
    add_completion=False,
    pretty_exceptions_enable=False,
)
backup_app = typer.Typer(help="Postgres backups on the cluster.", no_args_is_help=True)
app.add_typer(backup_app, name="backup")
app.add_typer(cli_secrets.app, name="secrets")

Env = Annotated[str, typer.Option("--env", "-e", help="staging or production.")]
Context = Annotated[
    str | None,
    typer.Option("--context", "-c", help="Kube context. Asked for when missing; never assumed."),
]
Verbose = Annotated[bool, typer.Option("--verbose", "-v", help="Stream command output.")]
Yes = Annotated[bool, typer.Option("--yes", "-y", help="No questions; for CI.")]
From = Annotated[
    str | None, typer.Option("--from", help=f"Start at a phase: {', '.join(phases.NAMES)}.")
]
Tags = Annotated[
    list[str] | None,
    typer.Option(
        "--tag", "-t", help="Override an image tag, for example kagami=1.2.3. Repeatable."
    ),
]


def _exit(code: int, message: str) -> typer.Exit:
    console.fail(message)
    return typer.Exit(code)


def _context(context: str | None, yes: bool, runner: Runner, tools: Tools) -> str:
    if context:
        return context
    if yes or not console.interactive():
        raise _exit(EXIT_USAGE, "--context is required (the current context is never assumed)")
    found = runner.run(
        [tools.kubectl, "config", "get-contexts", "-o", "name"], "listing contexts", capture=True
    )
    names = [line.strip() for line in found.output if line.strip()]
    if not names:
        raise _exit(EXIT_USAGE, "no kube contexts found; pass --context")
    return console.choose("Which cluster?", names)


def _target(env: str, context: str | None, yes: bool, verbose: bool) -> tuple[Target, Runner]:
    if env not in ENVIRONMENTS:
        raise _exit(EXIT_USAGE, f"--env must be one of {', '.join(ENVIRONMENTS)}")
    root = repo_root()
    runner = Runner(root, env, verbose)
    tools = Tools.from_env(root)
    target = Target(root, env, _context(context, yes, runner, tools), tools)
    console.info(f"{env} on {target.context}; log: {runner.log_path}")
    return target, runner


def _versions(target: Target, tags: list[str] | None) -> tuple[list[AppVersion], dict[str, str]]:
    try:
        overrides = parse_overrides(tags or [])
        versions, skipped = resolve(target.apps_dir, target.env, overrides)
    except VersionError as error:
        raise _exit(EXIT_USAGE, str(error)) from error
    for app_name in skipped:
        console.warn(f"{app_name} has no values-{target.env}.yaml; it is not part of {target.env}")
    return versions, overrides


def _select(start: str | None) -> list[phases.Phase]:
    try:
        return phases.select(start)
    except ValueError as error:
        raise _exit(EXIT_USAGE, str(error)) from error


def _preflight(target: Target, runner: Runner) -> None:
    if not preflight.report(preflight.checks(target, runner)):
        raise _exit(EXIT_FAILED, f"preflight failed; details in {runner.log_path}")


def _summary(target: Target, chosen: list[phases.Phase], versions: list[AppVersion]) -> None:
    console.table(
        f"About to apply to {target.env} on {target.context}",
        ["#", "phase", "what"],
        [[str(i), p.name, p.title] for i, p in enumerate(chosen, 1)],
    )
    if any(p.name == "apps" for p in chosen):
        console.table(
            "App versions",
            ["app", "image tag"],
            [[v.app, v.tag + (" [yellow](--tag)[/]" if v.overridden else "")] for v in versions],
        )


def _confirm(target: Target, yes: bool) -> None:
    if yes:
        return
    if not console.interactive():
        raise _exit(EXIT_USAGE, "not a terminal: pass --yes to apply without asking")
    if target.env == "production":
        agreed = console.type_to_confirm("This changes production.", target.context)
    else:
        agreed = console.confirm(f"Apply to {target.env}?")
    if not agreed:
        raise _exit(EXIT_USAGE, "stopped; nothing was applied")


def _seal_now(target: Target, runner: Runner, yes: bool) -> bool:
    """On a new cluster: offers to fetch the certificate, back up the key and seal."""
    paths = seal.Paths(target.root, target.env)
    console.warn(f"nothing is sealed for {target.env} yet; the controller is installed")
    if yes or not console.interactive():
        for line in (
            f"shogun-deploy secrets fetch-cert --env {target.env} --context {target.context}",
            f"shogun-deploy secrets backup-key --env {target.env} --context {target.context}",
            f"shogun-deploy secrets seal --env {target.env}",
            "commit deploy/infrastructure/sealed-secrets, then: "
            f"shogun-deploy up --env {target.env} --context {target.context} --from secrets",
        ):
            console.info(line)
        return False
    if not console.confirm(
        f"Fetch the certificate, back up the sealing key and seal from "
        f"{console.shown(paths.env_file, target.root)} now?"
    ):
        return False
    seal.fetch_cert(target.tools, runner, paths, target.context)
    seal.backup_key(target.tools, runner, paths, target.context)
    seal.seal_all(target.tools, runner, paths, paths.env_file, [])
    console.warn("commit deploy/infrastructure/sealed-secrets when this run is done")
    return True


def _apply(
    target: Target,
    runner: Runner,
    chosen: list[phases.Phase],
    overrides: dict[str, str],
    yes: bool,
    releases: list[str] | None = None,
) -> None:
    try:
        try:
            phases.run(target, runner, chosen, overrides, releases)
        except phases.SecretsMissing as error:
            if not _seal_now(target, runner, yes):
                raise typer.Exit(EXIT_WAITING) from error
            phases.run(target, runner, phases.select("secrets"), overrides, releases)
    except typer.Exit:
        raise
    except StepFailed as error:
        raise typer.Exit(EXIT_FAILED) from error
    except (OSError, RuntimeError, ValueError) as error:
        raise _exit(EXIT_FAILED, str(error)) from error
    console.ok(f"done; log: {runner.log_path}")


@app.command(name="preflight")
def preflight_command(env: Env, context: Context = None, verbose: Verbose = False) -> None:
    """Check the tools and that the cluster answers."""
    target, runner = _target(env, context, False, verbose)
    _preflight(target, runner)


@app.command(name="plan")
def plan_command(
    env: Env,
    context: Context = None,
    start: From = None,
    tags: Tags = None,
    verbose: Verbose = False,
) -> None:
    """Show the releases and which phases an apply would change (helmfile diff)."""
    target, runner = _target(env, context, False, verbose)
    versions, overrides = _versions(target, tags)
    try:
        plan.show(target, runner, _select(start), versions, overrides)
    except StepFailed as error:
        raise typer.Exit(EXIT_FAILED) from error


@app.command()
def up(
    env: Env,
    context: Context = None,
    start: From = None,
    tags: Tags = None,
    yes: Yes = False,
    verbose: Verbose = False,
) -> None:
    """Install or upgrade everything, phase by phase."""
    target, runner = _target(env, context, yes, verbose)
    chosen = _select(start)
    versions, overrides = _versions(target, tags)
    _preflight(target, runner)
    _summary(target, chosen, versions)
    _confirm(target, yes)
    _apply(target, runner, chosen, overrides, yes)


@app.command()
def apps(
    names: Annotated[
        list[str] | None, typer.Argument(help="Only these apps. Default: all.")
    ] = None,
    env: Env = "",
    context: Context = None,
    tags: Tags = None,
    yes: Yes = False,
    verbose: Verbose = False,
) -> None:
    """Install or upgrade the apps only (phase apps)."""
    target, runner = _target(env, context, yes, verbose)
    versions, overrides = _versions(target, tags)
    known = {v.app for v in versions}
    unknown = sorted(set(names or []) - known)
    if unknown:
        raise _exit(EXIT_USAGE, f"not an app in {target.env}: {', '.join(unknown)}")
    chosen = [p for p in phases.PHASES if p.name == "apps"]
    shown = [v for v in versions if not names or v.app in names]
    _preflight(target, runner)
    _summary(target, chosen, shown)
    _confirm(target, yes)
    _apply(target, runner, chosen, overrides, yes, names or None)


@app.command()
def status(env: Env, context: Context = None, verbose: Verbose = False) -> None:
    """Releases, pods that are not ready, and the last backup."""
    target, runner = _target(env, context, False, verbose)
    try:
        ops.status(target, runner)
    except StepFailed as error:
        raise typer.Exit(EXIT_FAILED) from error


@backup_app.command()
def now(env: Env, context: Context = None, verbose: Verbose = False) -> None:
    """Take a Postgres backup now and wait for it."""
    target, runner = _target(env, context, False, verbose)
    try:
        ops.backup_now(target, runner)
    except StepFailed as error:
        raise typer.Exit(EXIT_FAILED) from error


def main() -> None:
    app()
