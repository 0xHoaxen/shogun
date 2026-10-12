"""The small parts: tag resolution, redaction, plan, status and backup."""

from pathlib import Path

import pytest

from shogun_deploy.runner import REDACTED, redact
from shogun_deploy.versions import VersionError, parse_overrides, resolve


def write(path: Path, text: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text)


def test_resolve_reads_the_values_tag_and_skips_apps_without_env_values(tmp_path):
    write(tmp_path / "kagami/values-staging.yaml", 'image:\n  tag: "1.2.3"\n')
    versions, skipped = resolve(tmp_path, "staging", {})
    assert [(v.app, v.tag, v.overridden) for v in versions] == [("kagami", "1.2.3", False)]
    assert "web" in skipped


def test_resolve_prefers_the_override(tmp_path):
    write(tmp_path / "kagami/values-staging.yaml", 'image:\n  tag: "1.2.3"\n')
    versions, _ = resolve(tmp_path, "staging", {"kagami": "2.0.0"})
    assert (versions[0].tag, versions[0].overridden) == ("2.0.0", True)


def test_resolve_refuses_an_override_for_an_app_the_env_does_not_have(tmp_path):
    write(tmp_path / "kagami/values-staging.yaml", 'image:\n  tag: "1.2.3"\n')
    with pytest.raises(VersionError, match="not in staging: web"):
        resolve(tmp_path, "staging", {"web": "1.0.0"})


@pytest.mark.parametrize("pair", ["kagami", "kagami=", "=1.0", "nope=1.0"])
def test_parse_overrides_refuses_malformed_pairs(pair):
    with pytest.raises(VersionError):
        parse_overrides([pair])


def test_redact_hides_values_after_set_flags():
    argv = ["helm", "--set", "a=secret", "--set-string", "b=secret", "--wait"]
    assert redact(argv) == ["helm", "--set", REDACTED, "--set-string", REDACTED, "--wait"]


def test_plan_diffs_every_helmfile_phase_and_changes_nothing(fakes, cli):
    result = cli("plan", "--env", "staging", "--context", "k3s")
    assert result.exit_code == 0, result.output
    diffs = [c for c in fakes.helmfile_calls() if " diff " in c]
    assert [d.split("--selector phase=")[1].split()[0] for d in diffs] == [
        "bootstrap",
        "platform",
        "data",
        "apps",
    ]
    assert all("--detailed-exitcode" in d for d in diffs)
    assert fakes.applies() == []


def test_status_reads_releases_pods_and_backups(fakes, cli):
    result = cli("status", "--env", "staging", "--context", "k3s")
    assert result.exit_code == 0, result.output
    calls = fakes.calls()
    assert "helm --kube-context k3s list --all-namespaces --output json" in calls
    assert any("-n shogun get jobs -l app.kubernetes.io/component=backup" in c for c in calls)
    assert "no backup has run yet" in result.output


def test_backup_now_creates_a_job_from_the_cronjob_and_waits(fakes, cli):
    result = cli("backup", "now", "--env", "staging", "--context", "k3s")
    assert result.exit_code == 0, result.output
    calls = fakes.calls()
    assert any("create job --from=cronjob/postgres-backup backup-now-" in c for c in calls)
    assert any("wait --for=condition=complete job/backup-now-" in c for c in calls)
