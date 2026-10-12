"""Fixtures: a fixture repo tree and fake helmfile, helm and kubectl that log their calls."""

import os
import shutil
import stat
from dataclasses import dataclass
from pathlib import Path

import pytest
from typer.testing import CliRunner

from shogun_deploy.cli import app

REPO = Path(__file__).resolve().parents[3]

# Every fake logs "<tool> <args>" and answers the few calls whose output matters.
# FAKE_FAIL=<text> makes any call whose line contains <text> exit 1.
FAKE = """#!/usr/bin/env bash
line="$(basename "$0") $*"
echo "$line" >>"$FAKE_LOG"
if [ -n "${FAKE_FAIL:-}" ] && [[ "$line" == *"$FAKE_FAIL"* ]]; then
  echo "boom: $line" >&2
  exit 1
fi
case "$line" in
  "kubectl config get-contexts -o name") printf 'k3s\\nkind-shogun\\n' ;;
  "helm plugin list") printf 'NAME VERSION\\ndiff 3.15.15\\n' ;;
  "helmfile "*" list --output json") echo '[]' ;;
esac
exit 0
"""


@dataclass(frozen=True)
class Fakes:
    root: Path
    log: Path

    def calls(self) -> list[str]:
        return self.log.read_text().splitlines() if self.log.exists() else []

    def helmfile_calls(self) -> list[str]:
        return [c for c in self.calls() if c.startswith("helmfile ")]

    def applies(self) -> list[str]:
        """One word per apply: the helmfile phase or what kubectl applied."""
        steps = []
        for call in self.calls():
            if call.startswith("helmfile ") and " apply " in call:
                selector = call.split("--selector ", 1)[1].split(" ", 1)[0]
                steps.append(selector.removeprefix("phase="))
            elif call.startswith("kubectl ") and " apply " in call:
                steps.append(call.rsplit("/", 1)[-1])
        return steps


@pytest.fixture
def fakes(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> Fakes:
    root = tmp_path / "repo"
    shutil.copytree(REPO / "deploy" / "apps", root / "deploy" / "apps")
    (root / "helmfile.yaml.gotmpl").write_text("# fixture\n")
    for env in ("staging", "production"):
        sealed = root / "deploy" / "infrastructure" / "sealed-secrets" / "secrets" / env
        sealed.mkdir(parents=True)
        (sealed / "kustomization.yaml").write_text("kind: Kustomization\n")

    bin_dir = tmp_path / "bin"
    bin_dir.mkdir()
    for tool in ("helmfile", "helm", "kubectl", "kubeseal"):
        path = bin_dir / tool
        path.write_text(FAKE)
        path.chmod(path.stat().st_mode | stat.S_IEXEC)
        monkeypatch.setenv(tool.upper(), str(path))

    log = tmp_path / "calls.log"
    monkeypatch.setenv("FAKE_LOG", str(log))
    monkeypatch.setenv("SHOGUN_ROOT", str(root))
    monkeypatch.delenv("FAKE_FAIL", raising=False)
    return Fakes(root, log)


@pytest.fixture
def cli():
    runner = CliRunner()

    def invoke(*args: str, env: dict[str, str] | None = None):
        return runner.invoke(app, list(args), env={**os.environ, **(env or {})})

    return invoke
