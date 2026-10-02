#!/usr/bin/env python3
"""Maintain the per-service Helm values.

Usage: scripts/helm.py add NAME

Creates deploy/helm/values/{staging,production}/NAME.yaml and adds NAME to the
chart's peers list, so every service learns its address. Idempotent: existing
values files are left alone.
"""
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
CHART_VALUES = ROOT / "deploy/helm/service/values.yaml"
VALUES_DIR = ROOT / "deploy/helm/values"

STAGING = """# {name}, staging. image.tag is bumped by .github/workflows/deploy.yml.
name: {name}
image:
  tag: "0.0.0"
env:
  ENVIRONMENT: staging
"""

PRODUCTION = """# {name}, production. image.tag is bumped by .github/workflows/deploy.yml.
name: {name}
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


def add(name: str) -> None:
    if not re.fullmatch(r"[a-z][a-z0-9]*", name):
        sys.exit(f"helm.py: invalid service name {name!r}")

    for env, template in (("staging", STAGING), ("production", PRODUCTION)):
        path = VALUES_DIR / env / f"{name}.yaml"
        path.parent.mkdir(parents=True, exist_ok=True)
        if not path.exists():
            path.write_text(template.format(name=name))

    text = CHART_VALUES.read_text()
    marker = "  # peers end\n"
    if marker not in text:
        sys.exit(f"helm.py: marker {marker.strip()!r} missing from {CHART_VALUES}")
    entry = f"  - {name}\n"
    if entry not in text:
        CHART_VALUES.write_text(text.replace(marker, entry + marker, 1))
    print(f"helm.py: added {name}")


def main() -> None:
    if len(sys.argv) != 3 or sys.argv[1] != "add":
        sys.exit(__doc__)
    add(sys.argv[2])


if __name__ == "__main__":
    main()
