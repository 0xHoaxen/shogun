"""What a run works on: the repo, the environment, the cluster and the tools."""

import os
from dataclasses import dataclass
from pathlib import Path

ENVIRONMENTS = ("staging", "production")

# Install order inside phase apps comes from the helmfile's needs; this list is
# for resolving tags and reporting.
APPS = (
    "soroban",
    "kagami",
    "fude",
    "tsubame",
    "taiko",
    "dojo",
    "katana",
    "shinobi",
    "sensei",
    "torii",
    "web",
)

# Exit codes.
EXIT_OK = 0
EXIT_FAILED = 1
EXIT_USAGE = 2
EXIT_WAITING = 3


def repo_root() -> Path:
    """The checkout this package lives in, or SHOGUN_ROOT (tests use a fixture tree)."""
    override = os.environ.get("SHOGUN_ROOT")
    if override:
        return Path(override).resolve()
    return Path(__file__).resolve().parents[4]


@dataclass(frozen=True)
class Tools:
    """Commands to run. Overridable by environment, so tests can use fakes."""

    helmfile: str
    helm: str
    kubectl: str
    kubeseal: str

    @classmethod
    def from_env(cls, root: Path) -> "Tools":
        pinned = root / "bin" / "helmfile"
        return cls(
            helmfile=os.environ.get("HELMFILE", str(pinned) if pinned.exists() else "helmfile"),
            helm=os.environ.get("HELM", "helm"),
            kubectl=os.environ.get("KUBECTL", "kubectl"),
            kubeseal=os.environ.get("KUBESEAL", "kubeseal"),
        )


@dataclass(frozen=True)
class Target:
    """One environment on one cluster."""

    root: Path
    env: str
    context: str
    tools: Tools

    @property
    def helmfile_path(self) -> Path:
        return self.root / "helmfile.yaml.gotmpl"

    @property
    def apps_dir(self) -> Path:
        return self.root / "deploy" / "apps"

    @property
    def infra_dir(self) -> Path:
        return self.root / "deploy" / "infrastructure"

    @property
    def sealed_dir(self) -> Path:
        return self.infra_dir / "sealed-secrets" / "secrets" / self.env
