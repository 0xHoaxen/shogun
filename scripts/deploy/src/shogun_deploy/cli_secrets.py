"""shogun-deploy secrets: seal a cluster's Secrets with kubeseal so they can be committed.

Each environment is its own cluster with its own sealing key. Order on a new
cluster: up stops after the controller (or offers to do these for you), then
fetch-cert, backup-key, seal, commit deploy/infrastructure/sealed-secrets, and
up --from secrets.
"""

from collections.abc import Callable
from pathlib import Path
from typing import Annotated

import typer

from shogun_deploy import console, seal
from shogun_deploy.runner import Runner, StepFailed
from shogun_deploy.settings import ENVIRONMENTS, EXIT_FAILED, EXIT_USAGE, Tools, repo_root

app = typer.Typer(help=__doc__, no_args_is_help=True)

Env = Annotated[str, typer.Option("--env", "-e", help="staging or production.")]
Context = Annotated[str, typer.Option("--context", "-c", help="Kube context of the cluster.")]


def _setup(env: str, cert: Path | None = None) -> tuple[Tools, Runner, seal.Paths]:
    if env not in ENVIRONMENTS:
        console.fail(f"--env must be one of {', '.join(ENVIRONMENTS)}")
        raise typer.Exit(EXIT_USAGE)
    root = repo_root()
    paths = seal.Paths(root, env, cert.resolve() if cert else None)
    return Tools.from_env(root), Runner(root, env), paths


def _run(action: Callable[..., None], *args: object) -> None:
    try:
        action(*args)
    except StepFailed as error:
        raise typer.Exit(EXIT_FAILED) from error
    except FileNotFoundError as error:
        console.fail(str(error))
        raise typer.Exit(EXIT_USAGE) from error
    except (OSError, RuntimeError, ValueError) as error:
        console.fail(str(error))
        raise typer.Exit(EXIT_FAILED) from error


@app.command(name="fetch-cert")
def fetch_cert(env: Env, context: Context) -> None:
    """Write the controller's public certificate to certs/<env>-pub-cert.pem (commit it)."""
    tools, runner, paths = _setup(env)
    _run(seal.fetch_cert, tools, runner, paths, context)


@app.command(name="backup-key")
def backup_key(env: Env, context: Context) -> None:
    """Save the controller's private key to .secrets/ (never commit it; store it safely)."""
    tools, runner, paths = _setup(env)
    _run(seal.backup_key, tools, runner, paths, context)


@app.command(name="seal")
def seal_command(
    env: Env,
    env_file: Annotated[
        Path | None, typer.Option(help="KEY=value file. Default .secrets/<env>.env.")
    ] = None,
    cert: Annotated[Path | None, typer.Option(help="Default certs/<env>-pub-cert.pem.")] = None,
    only: Annotated[
        list[str] | None, typer.Option(help="Reseal just this Secret. Repeatable.")
    ] = None,
) -> None:
    """Seal every Secret (or --only some) offline, from the env file and the certificate.

    Generated values (passwords, keys) are written back to the env file, so a
    rerun keeps them. See deploy/infrastructure/sealed-secrets/secrets.example.env
    for what you supply. Values are never printed.
    """
    tools, runner, paths = _setup(env, cert)
    _run(seal.seal_all, tools, runner, paths, env_file or paths.env_file, only or [])
