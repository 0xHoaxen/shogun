"""Image tags: image.tag in deploy/apps/<app>/values-<env>.yaml, or a --tag override."""

from dataclasses import dataclass
from pathlib import Path

import yaml

from shogun_deploy.settings import APPS


class VersionError(ValueError):
    """A tag is missing or an override is malformed."""


@dataclass(frozen=True)
class AppVersion:
    app: str
    tag: str
    overridden: bool


def parse_overrides(pairs: list[str]) -> dict[str, str]:
    """--tag kagami=1.2.3 pairs; the last one for an app wins."""
    overrides: dict[str, str] = {}
    for pair in pairs:
        app, sep, tag = pair.partition("=")
        if not sep or not tag or app not in APPS:
            raise VersionError(f"--tag must be <app>=<version> with a known app, got {pair!r}")
        overrides[app] = tag
    return overrides


def values_tag(path: Path) -> str:
    values = yaml.safe_load(path.read_text()) or {}
    image = values.get("image") or {}
    return str(image.get("tag") or "")


def resolve(
    apps_dir: Path, env: str, overrides: dict[str, str]
) -> tuple[list[AppVersion], list[str]]:
    """The version of every app in env, and the apps env does not have.

    Resolves all of them before anything is installed, so a missing tag stops the
    run while the cluster is still untouched.
    """
    versions, skipped = [], []
    for app in APPS:
        path = apps_dir / app / f"values-{env}.yaml"
        if not path.exists():
            skipped.append(app)
            continue
        tag = overrides.get(app) or values_tag(path)
        if not tag:
            raise VersionError(
                f"no version for {app}: set image.tag in deploy/apps/{app}/values-{env}.yaml "
                f"or pass --tag {app}=<version>"
            )
        versions.append(AppVersion(app, tag, app in overrides))
    unknown = set(overrides) - {v.app for v in versions}
    if unknown:
        raise VersionError(f"--tag for an app not in {env}: {', '.join(sorted(unknown))}")
    return versions, skipped
