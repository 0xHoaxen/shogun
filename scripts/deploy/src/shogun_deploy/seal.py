"""Sealing a cluster's Secrets with kubeseal, so they can be committed.

Sealing needs only the controller's public certificate, not the cluster.
Output: deploy/infrastructure/sealed-secrets/secrets/<env>/<name>.yaml plus a
kustomization.yaml listing them, which phase secrets applies. kubeseal encrypts
with a fresh nonce, so a rerun rewrites every sealed file; --only touches one.
"""

import base64
import json
import tempfile
from collections.abc import Iterable
from dataclasses import dataclass
from pathlib import Path

from shogun_deploy import console, envfile
from shogun_deploy.runner import Runner
from shogun_deploy.settings import Tools

APP_NAMESPACE = "shogun"
MONITORING_NAMESPACE = "monitoring"
# kubeseal's own defaults; deploy/infrastructure/sealed-secrets/values.yaml keeps them.
CONTROLLER_NAME = "sealed-secrets-controller"
CONTROLLER_NAMESPACE = "kube-system"
KEY_LABEL = "sealedsecrets.bitnami.com/sealed-secrets-key"


@dataclass(frozen=True)
class Paths:
    root: Path
    env: str
    # Another certificate to seal against; default certs/<env>-pub-cert.pem.
    cert_override: Path | None = None

    @property
    def sealed_root(self) -> Path:
        return self.root / "deploy" / "infrastructure" / "sealed-secrets"

    @property
    def cert(self) -> Path:
        return self.cert_override or self.sealed_root / "certs" / f"{self.env}-pub-cert.pem"

    @property
    def outdir(self) -> Path:
        return self.sealed_root / "secrets" / self.env

    @property
    def env_file(self) -> Path:
        return self.root / ".secrets" / f"{self.env}.env"

    @property
    def key_backup(self) -> Path:
        return self.root / ".secrets" / f"{self.env}-sealed-secrets-key.yaml"


def _write_atomic(path: Path, text: str, mode: int = 0o644) -> None:
    """Whole or not at all, so a failed run keeps the old file."""
    path.parent.mkdir(parents=True, exist_ok=True)
    partial = path.with_name(path.name + ".new")
    partial.write_text(text)
    partial.chmod(mode)
    partial.replace(path)


def fetch_cert(tools: Tools, runner: Runner, paths: Paths, context: str) -> None:
    cert = runner.secret(
        [tools.kubeseal, "--context", context, "--controller-name", CONTROLLER_NAME,
         "--controller-namespace", CONTROLLER_NAMESPACE, "--fetch-cert"],
        "fetch the sealing certificate",
    )  # fmt: skip
    if not cert.strip():
        raise RuntimeError("the controller returned no certificate")
    _write_atomic(paths.cert, cert)
    console.ok(f"wrote {console.shown(paths.cert, paths.root)} (public, safe to commit)")


def backup_key(tools: Tools, runner: Runner, paths: Paths, context: str) -> None:
    key = runner.secret(
        [tools.kubectl, "--context", context, "-n", CONTROLLER_NAMESPACE, "get", "secret",
         "-l", KEY_LABEL, "-o", "yaml"],
        "back up the sealing key",
    )  # fmt: skip
    if not key.strip():
        raise RuntimeError(f"no sealing key found in {CONTROLLER_NAMESPACE}")
    _write_atomic(paths.key_backup, key, 0o600)
    console.ok(
        f"wrote {console.shown(paths.key_backup, paths.root)} "
        "(private: keep it out of git, store it safely)"
    )


def _seal(tools: Tools, runner: Runner, paths: Paths, manifest: str, name: str) -> None:
    sealed = runner.secret(
        [tools.kubeseal, "--cert", str(paths.cert), "--scope", "strict", "--format", "yaml"],
        f"seal {name}",
        stdin=manifest,
    )
    _write_atomic(paths.outdir / f"{name}.yaml", sealed)


def seal_env_secret(
    tools: Tools, runner: Runner, paths: Paths, tmp: Path, namespace: str, name: str,
    pairs: Iterable[tuple[str, str]],
) -> None:  # fmt: skip
    pairs = list(pairs)
    env_path = tmp / f"{name}.env"
    env_path.write_text("".join(f"{k}={v}\n" for k, v in pairs))
    env_path.chmod(0o600)
    manifest = runner.secret(
        [tools.kubectl, "create", "secret", "generic", name, "--namespace", namespace,
         f"--from-env-file={env_path}", "--dry-run=client", "-o", "yaml"],
        f"build {name}",
    )  # fmt: skip
    _seal(tools, runner, paths, manifest, name)
    console.ok(f"sealed {name} in {namespace} ({len(pairs)} keys)")


def seal_pull_secret(
    tools: Tools, runner: Runner, paths: Paths, tmp: Path, user: str, token: str
) -> None:
    """The ghcr.io pull secret, from a file so the token never appears on a command line."""
    auth = base64.b64encode(f"{user}:{token}".encode()).decode()
    config = tmp / "dockerconfigjson"
    config.write_text(
        json.dumps({"auths": {"ghcr.io": {"username": user, "password": token, "auth": auth}}})
    )
    config.chmod(0o600)
    manifest = runner.secret(
        [tools.kubectl, "create", "secret", "generic", "ghcr", "--namespace", APP_NAMESPACE,
         "--type=kubernetes.io/dockerconfigjson", f"--from-file=.dockerconfigjson={config}",
         "--dry-run=client", "-o", "yaml"],
        "build ghcr",
    )  # fmt: skip
    _seal(tools, runner, paths, manifest, "ghcr")
    console.ok(f"sealed ghcr in {APP_NAMESPACE} (pull secret for ghcr.io)")


def write_kustomization(outdir: Path) -> None:
    names = sorted(p.name for p in outdir.glob("*.yaml") if p.name != "kustomization.yaml")
    text = (
        "# Written by shogun-deploy secrets seal. Applied in phase secrets.\n"
        "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n"
        + "".join(f"  - {name}\n" for name in names)
    )
    _write_atomic(outdir / "kustomization.yaml", text)


def seal_all(tools: Tools, runner: Runner, paths: Paths, env_file: Path, only: list[str]) -> None:
    if not paths.cert.exists():
        raise FileNotFoundError(f"no certificate at {paths.cert} (run secrets fetch-cert first)")
    envfile.require_openssl3()
    values = envfile.fill(env_file)
    envfile.warn_unset(values)

    def wanted(name: str) -> bool:
        return not only or name in only

    with tempfile.TemporaryDirectory(prefix="shogun-seal-") as tmp_name:
        tmp = Path(tmp_name)
        if wanted("postgres"):
            seal_env_secret(
                tools, runner, paths, tmp, APP_NAMESPACE, "postgres", envfile.postgres_env(values)
            )
        for svc in envfile.SERVICES:
            if wanted(svc):
                seal_env_secret(
                    tools, runner, paths, tmp, APP_NAMESPACE, svc, envfile.service_env(values, svc)
                )
        if wanted("grafana-admin"):
            seal_env_secret(
                tools,
                runner,
                paths,
                tmp,
                MONITORING_NAMESPACE,
                "grafana-admin",
                envfile.grafana_env(values),
            )
        user, token = values.get("GHCR_USER", ""), values.get("GHCR_TOKEN", "")
        if wanted("ghcr") and user and token:
            seal_pull_secret(tools, runner, paths, tmp, user, token)
    write_kustomization(paths.outdir)
