"""up and apps: the checks that deploy_test.sh and platform_test.sh made, and more."""

from shogun_deploy.settings import EXIT_FAILED, EXIT_USAGE, EXIT_WAITING

STAGING = ("--env", "staging", "--context", "k3s")
ALL_STEPS = [
    "bootstrap",
    "staging",
    "platform",
    "issuers",
    "k3s-helmchartconfig.yaml",
    "data",
    "apps",
]


def test_refuses_without_a_context_when_not_interactive(fakes, cli):
    result = cli("up", "--env", "staging", "--yes")
    assert result.exit_code == EXIT_USAGE
    assert "--context is required" in result.output
    assert fakes.calls() == []


def test_refuses_an_unknown_env(fakes, cli):
    result = cli("up", "--env", "qa", "--context", "k3s", "--yes")
    assert result.exit_code == EXIT_USAGE
    assert "--env must be one of" in result.output
    assert fakes.calls() == []


def test_refuses_an_unknown_option(fakes, cli):
    assert cli("up", *STAGING, "--namespace", "shogun").exit_code == EXIT_USAGE
    assert fakes.calls() == []


def test_refuses_an_unknown_phase(fakes, cli):
    result = cli("up", *STAGING, "--from", "nope", "--yes")
    assert result.exit_code == EXIT_USAGE
    assert fakes.calls() == []


def test_a_full_run_applies_every_phase_in_order(fakes, cli):
    result = cli("up", *STAGING, "--yes")
    assert result.exit_code == 0, result.output
    assert fakes.applies() == ALL_STEPS


def test_every_helmfile_apply_pins_the_target_and_runs_one_release_at_a_time(fakes, cli):
    cli("up", *STAGING, "--yes")
    applies = [c for c in fakes.helmfile_calls() if " apply " in c]
    assert len(applies) == 4
    for call in applies:
        assert (
            f"--file {fakes.root}/helmfile.yaml.gotmpl --environment staging --kube-context k3s"
            in call
        )
        assert call.endswith("apply --concurrency 1 --suppress-diff")


def test_every_kubectl_apply_pins_the_context(fakes, cli):
    cli("up", *STAGING, "--yes")
    applies = [c for c in fakes.calls() if c.startswith("kubectl ") and " apply " in c]
    assert len(applies) == 3
    assert all(c.startswith("kubectl --context k3s apply ") for c in applies)


def test_preflight_runs_before_anything_is_applied(fakes, cli):
    cli("up", *STAGING, "--yes")
    calls = fakes.calls()
    first_apply = next(i for i, c in enumerate(calls) if " apply " in c)
    assert any("get --raw /readyz" in c for c in calls[:first_apply])
    assert any(c == "helm plugin list" for c in calls[:first_apply])


def test_no_tag_override_without_tag(fakes, cli):
    cli("up", *STAGING, "--yes")
    assert not any("--state-values-set" in c for c in fakes.calls())


def test_tags_override_the_values_file_and_only_reach_phase_apps(fakes, cli):
    result = cli(
        "up",
        *STAGING,
        "--yes",
        "--tag",
        "kagami=9.9.9",
        "--tag",
        "web=8.8.8",
        "--tag",
        "kagami=7.7.7",
    )
    assert result.exit_code == 0, result.output
    apps = next(c for c in fakes.helmfile_calls() if "phase=apps" in c)
    assert "--state-values-set tags.kagami=7.7.7" in apps
    assert "--state-values-set tags.web=8.8.8" in apps
    assert "tags.torii" not in apps
    others = [c for c in fakes.helmfile_calls() if "phase=apps" not in c]
    assert not any("--state-values-set" in c for c in others)


def test_a_malformed_tag_is_refused_before_any_call(fakes, cli):
    result = cli("up", *STAGING, "--yes", "--tag", "kagami")
    assert result.exit_code == EXIT_USAGE
    assert fakes.calls() == []


def test_refuses_when_an_app_has_no_version(fakes, cli):
    (fakes.root / "deploy/apps/sensei/values-staging.yaml").write_text(
        "env:\n  ENVIRONMENT: staging\n"
    )
    result = cli("up", *STAGING, "--yes")
    assert result.exit_code == EXIT_USAGE
    assert "no version for sensei" in result.output
    assert fakes.calls() == []


def test_production_skips_an_app_without_production_values(fakes, cli):
    (fakes.root / "deploy/apps/web/values-production.yaml").unlink(missing_ok=True)
    result = cli("up", "--env", "production", "--context", "k3s", "--yes")
    assert result.exit_code == 0, result.output
    assert "web has no values-production.yaml" in result.output


def test_from_starts_at_that_phase(fakes, cli):
    cli("up", *STAGING, "--yes", "--from", "data")
    assert fakes.applies() == ["data", "apps"]


def test_without_yes_and_no_terminal_nothing_is_applied(fakes, cli):
    result = cli("up", *STAGING)
    assert result.exit_code == EXIT_USAGE
    assert "pass --yes" in result.output
    assert fakes.applies() == []


def test_stops_with_exit_3_and_the_next_steps_when_nothing_is_sealed(fakes, cli):
    (
        fakes.root / "deploy/infrastructure/sealed-secrets/secrets/staging/kustomization.yaml"
    ).unlink()
    result = cli("up", *STAGING, "--yes")
    assert result.exit_code == EXIT_WAITING
    assert "--fetch-cert --context k3s" in result.output
    assert "--from secrets" in result.output
    assert fakes.applies() == ["bootstrap"]


def test_production_applies_its_own_sealed_secrets(fakes, cli):
    cli("up", "--env", "production", "--context", "k3s", "--yes")
    assert any("sealed-secrets/secrets/production" in c for c in fakes.calls())


def test_a_failed_preflight_applies_nothing(fakes, cli):
    result = cli("up", *STAGING, "--yes", env={"FAKE_FAIL": "get --raw /readyz"})
    assert result.exit_code == EXIT_FAILED
    assert "preflight failed" in result.output
    assert fakes.applies() == []


def test_a_failed_phase_stops_the_run_and_shows_the_output(fakes, cli):
    result = cli("up", *STAGING, "--yes", env={"FAKE_FAIL": "phase=platform"})
    assert result.exit_code == EXIT_FAILED
    assert "boom:" in result.output
    assert "full output:" in result.output
    assert fakes.applies() == ["bootstrap", "staging", "platform"]


def test_every_run_writes_a_log_file(fakes, cli):
    cli("up", *STAGING, "--yes")
    logs = list((fakes.root / ".deploy-logs").glob("*-staging.log"))
    assert len(logs) == 1
    assert "phase=apps" in logs[0].read_text()


def test_apps_applies_only_the_named_releases(fakes, cli):
    result = cli("apps", "kagami", "torii", *STAGING, "--yes")
    assert result.exit_code == 0, result.output
    (call,) = [c for c in fakes.helmfile_calls() if " apply " in c]
    assert "--selector name=kagami --selector name=torii" in call
    assert fakes.applies() == ["name=kagami"]


def test_apps_refuses_an_unknown_app(fakes, cli):
    result = cli("apps", "nope", *STAGING, "--yes")
    assert result.exit_code == EXIT_USAGE
    assert fakes.applies() == []
