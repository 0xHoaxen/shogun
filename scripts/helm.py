#!/usr/bin/env python3
"""Add a service to the Helm deploy values.

Usage:
  scripts/helm.py add NAME   values files for NAME and its peers entry

Every app is installed with the one chart in deploy/charts/microservice. A
service needs deploy/apps/NAME/values.yaml (what it is), values-staging.yaml and
values-production.yaml (image.tag and per-environment settings), and its name in
the peers list of deploy/apps/common.yaml so every pod gets NAME_ADDR.

Idempotent: existing values files and peers entries are left alone.
"""
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
APPS_DIR = ROOT / "deploy/apps"
COMMON = APPS_DIR / "common.yaml"

PEERS_END = "  # peers end\n"
SERVICE_NAME = re.compile(r"[a-z][a-z0-9]*")

VALUES = """# {name}: what the service is. Per-environment settings are in values-<env>.yaml.
name: {name}
env:
  LOG_LEVEL: info
"""

STAGING = """# {name}, staging. image.tag is bumped by .github/workflows/deploy.yml.
image:
  tag: "0.0.0"
env:
  ENVIRONMENT: staging
"""

PRODUCTION = """# {name}, production. image.tag is bumped by .github/workflows/deploy.yml.
image:
  tag: "0.0.0"
env:
  ENVIRONMENT: production
autoscaling:
  enabled: true
podDisruptionBudget:
  enabled: true
resources:
  requests:
    cpu: 100m
    memory: 128Mi
  limits:
    memory: 512Mi
"""

FILES = {
    "values.yaml": VALUES,
    "values-staging.yaml": STAGING,
    "values-production.yaml": PRODUCTION,
}


def fail(message: str) -> None:
    sys.exit(f"helm.py: {message}")


def check_name(name: str) -> None:
    if not SERVICE_NAME.fullmatch(name):
        fail(f"invalid service name {name!r}")


def with_peer(common: str, name: str) -> str:
    if PEERS_END not in common:
        fail(f"peers markers missing from {COMMON}")
    entry = f"  - {name}\n"
    if entry in common:
        return common
    return common.replace(PEERS_END, entry + PEERS_END, 1)


def add(name: str) -> None:
    check_name(name)
    app_dir = APPS_DIR / name
    app_dir.mkdir(parents=True, exist_ok=True)
    for file_name, template in FILES.items():
        path = app_dir / file_name
        if not path.exists():
            path.write_text(template.format(name=name))

    common = COMMON.read_text()
    updated = with_peer(common, name)
    if updated != common:
        COMMON.write_text(updated)
    print(f"helm.py: added {name}")


def main() -> None:
    args = sys.argv[1:]
    if len(args) == 2 and args[0] == "add":
        add(args[1])
    else:
        sys.exit(__doc__)


if __name__ == "__main__":
    main()
