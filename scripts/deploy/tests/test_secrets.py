"""shogun-deploy secrets: the checks seal-secrets_test.sh made, with the same fakes."""

import os
import stat
import zlib
from pathlib import Path

import pytest

from shogun_deploy import cli, console, envfile, seal
from shogun_deploy.runner import Runner
from shogun_deploy.settings import EXIT_USAGE, EXIT_WAITING, Target, Tools

# The fake kubectl keeps each Secret's env file under $FAKE_DIR so the test can
# read it, and prints a plain Secret manifest. It must be the only place
# plaintext goes.
FAKE_KUBECTL = r"""#!/usr/bin/env bash
case " $* " in
*" create secret generic "*)
	name="" file="" prev="" ns=""
	for arg in "$@"; do
		[ "$prev" = generic ] && name=$arg
		[ "$prev" = --namespace ] && ns=$arg
		case $arg in
		--from-env-file=*) file=${arg#--from-env-file=} ;;
		--from-file=*) file=${arg#--from-file=.dockerconfigjson=} ;;
		esac
		prev=$arg
	done
	cp "$file" "$FAKE_DIR/$name.env"
	echo "$ns" >"$FAKE_DIR/$name.ns"
	printf 'kind: Secret\nmetadata:\n  name: %s\n  namespace: %s\n' "$name" "$ns"
	;;
*" get secret "*) echo "kind: List" ;;
*" config get-contexts "*) echo k3s ;;
*) echo "kubectl $*" >>"$FAKE_LOG" ;;
esac
"""
# The fake kubeseal turns stdin into a SealedSecret with a fresh nonce on every
# call, like the real one, and never repeats the input.
FAKE_KUBESEAL = r"""#!/usr/bin/env bash
case " $* " in
*" --fetch-cert "*) echo "-----FAKE CERT-----" ;;
*)
	echo "$*" >>"$FAKE_DIR/kubeseal.args"
	name=$(sed -n 's/^  name: //p')
	printf 'kind: SealedSecret\nmetadata:\n  name: %s\n' "$name"
	printf 'spec:\n  encryptedData:\n    blob: %s%s\n' "$RANDOM" "$RANDOM"
	;;
esac
"""
SENTINEL = "sentinel-value-must-not-print"
DEV_SEED = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="
DEV_PUBLIC = "A6EHv/POEL4dcN0Y50vAmWfk1jCbpQ1fHdyGZBJVMbg="


def mode(path: Path) -> int:
    return stat.S_IMODE(path.stat().st_mode)


def checksum(path: Path) -> int:
    return zlib.crc32(path.read_bytes())


class Sealer:
    def __init__(self, root: Path, fake_dir: Path, env_file: Path) -> None:
        self.root, self.fake_dir, self.env_file = root, fake_dir, env_file
        self.paths = seal.Paths(root, "staging")

    def lines(self, name: str) -> list[str]:
        return (self.fake_dir / f"{name}.env").read_text().splitlines()

    def keys(self, name: str) -> list[str]:
        return sorted(line.split("=", 1)[0] for line in self.lines(name))

    def value(self, name: str, key: str) -> str:
        return next(
            line.split("=", 1)[1] for line in self.lines(name) if line.startswith(f"{key}=")
        )

    def namespace(self, name: str) -> str:
        return (self.fake_dir / f"{name}.ns").read_text().strip()

    def sealed(self) -> list[Path]:
        return sorted(p for p in self.paths.outdir.glob("*.yaml") if p.name != "kustomization.yaml")

    def listed(self) -> list[str]:
        text = (self.paths.outdir / "kustomization.yaml").read_text()
        return [line[4:] for line in text.splitlines() if line.startswith("  - ")]


@pytest.fixture
def sealer(fakes, tmp_path, monkeypatch) -> Sealer:
    fake_dir = tmp_path / "fake"
    fake_dir.mkdir()
    for tool, script in (("kubectl", FAKE_KUBECTL), ("kubeseal", FAKE_KUBESEAL)):
        path = tmp_path / "bin" / tool
        path.write_text(script)
        path.chmod(0o755)
    monkeypatch.setenv("FAKE_DIR", str(fake_dir))
    env_file = tmp_path / "staging.env"
    env_file.write_text(
        f"ANTHROPIC_API_KEY={SENTINEL}\nKATANA_GITHUB_USER=octocat\nKATANA_GITHUB_TOKEN={SENTINEL}\n"
    )
    # The conftest tree has sealed kustomizations; start with nothing sealed here.
    for env in ("staging", "production"):
        (
            fakes.root / f"deploy/infrastructure/sealed-secrets/secrets/{env}/kustomization.yaml"
        ).unlink()
    return Sealer(fakes.root, fake_dir, env_file)


def seal_run(invoke, sealer: Sealer, *extra: str):
    return invoke("secrets", "seal", "--env", "staging", "--env-file", str(sealer.env_file), *extra)


def fetched(invoke, sealer: Sealer) -> None:
    result = invoke("secrets", "fetch-cert", "--env", "staging", "--context", "kind-shogun")
    assert result.exit_code == 0, result.output


def test_refuses_without_an_env(sealer, cli):
    assert cli("secrets", "seal", "--env-file", str(sealer.env_file)).exit_code == EXIT_USAGE


def test_refuses_an_unknown_env(sealer, cli):
    result = cli("secrets", "seal", "--env", "qa", "--env-file", str(sealer.env_file))
    assert result.exit_code == EXIT_USAGE
    assert "--env must be one of" in result.output


def test_refuses_to_seal_without_a_certificate_and_seals_nothing(sealer, cli):
    result = seal_run(cli, sealer)
    assert result.exit_code == EXIT_USAGE
    assert "fetch-cert first" in result.output
    assert list(sealer.fake_dir.iterdir()) == []


def test_fetch_cert_refuses_without_a_context(sealer, cli):
    assert cli("secrets", "fetch-cert", "--env", "staging").exit_code == EXIT_USAGE


def test_fetch_cert_writes_the_certificate_per_env(sealer, cli):
    fetched(cli, sealer)
    assert "FAKE CERT" in sealer.paths.cert.read_text()
    assert (
        cli("secrets", "fetch-cert", "--env", "production", "--context", "kind-prod").exit_code == 0
    )
    assert seal.Paths(sealer.root, "production").cert.stat().st_size > 0


def test_backup_key_writes_a_private_file_under_secrets(sealer, cli):
    assert (
        cli("secrets", "backup-key", "--env", "staging", "--context", "kind-shogun").exit_code == 0
    )
    assert mode(sealer.paths.key_backup) == 0o600


def test_a_full_run_seals_12_secrets_and_never_shows_a_value(sealer, cli):
    fetched(cli, sealer)
    result = seal_run(cli, sealer)
    assert result.exit_code == 0, result.output
    assert len(sealer.sealed()) == 12
    assert SENTINEL not in result.output
    assert all(SENTINEL not in p.read_text() for p in sealer.paths.outdir.iterdir())
    logs = list((sealer.root / ".deploy-logs").glob("*.log"))
    assert logs
    assert all(SENTINEL not in p.read_text() for p in logs)
    assert mode(sealer.env_file) == 0o600


def test_every_secret_is_sealed_with_strict_scope_against_the_certificate(sealer, cli):
    fetched(cli, sealer)
    seal_run(cli, sealer)
    args = (sealer.fake_dir / "kubeseal.args").read_text().splitlines()
    assert len(args) == 12
    assert all("--scope strict" in a and f"--cert {sealer.paths.cert}" in a for a in args)


def test_the_kustomization_lists_the_sealed_files_and_not_itself(sealer, cli):
    fetched(cli, sealer)
    seal_run(cli, sealer)
    listed = sealer.listed()
    assert len(listed) == 12
    assert "kustomization.yaml" not in listed
    assert "grafana-admin.yaml" in listed


def test_each_secret_holds_the_keys_its_service_reads(sealer, cli):
    fetched(cli, sealer)
    seal_run(cli, sealer)
    assert len(sealer.keys("postgres")) == 11
    assert sealer.keys("torii") == [
        "DATABASE_URL", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "IDENTITY_SIGNING_KEY",
        "TORII_ALLOWED_EMAILS", "TORII_PUBLIC_URL",
    ]  # fmt: skip
    assert sealer.keys("fude") == [
        "ANTHROPIC_API_KEY", "DATABASE_URL", "FUDE_HANKO_KEY_ID", "FUDE_HANKO_SIGNING_KEY",
        "IDENTITY_SIGNING_KEY",
    ]  # fmt: skip
    assert sealer.keys("tsubame") == [
        "ANTHROPIC_API_KEY", "DATABASE_URL", "IDENTITY_SIGNING_KEY", "TSUBAME_GMAIL_CLIENT_ID",
        "TSUBAME_GMAIL_CLIENT_SECRET", "TSUBAME_GMAIL_REDIRECT_URL", "TSUBAME_HANKO_VERIFY_KEYS",
        "TSUBAME_TOKEN_KEY_ID", "TSUBAME_TOKEN_MASTER_KEY",
    ]  # fmt: skip
    assert sealer.keys("katana") == [
        "ANTHROPIC_API_KEY", "DATABASE_URL", "IDENTITY_SIGNING_KEY", "KATANA_GITHUB_TOKEN",
        "KATANA_GITHUB_USER",
    ]  # fmt: skip
    assert sealer.keys("kagami") == ["DATABASE_URL", "IDENTITY_SIGNING_KEY"]


def test_services_share_one_identity_key_and_use_their_own_role(sealer, cli):
    fetched(cli, sealer)
    seal_run(cli, sealer)
    identity = sealer.value("torii", "IDENTITY_SIGNING_KEY")
    assert identity == sealer.value("soroban", "IDENTITY_SIGNING_KEY")
    password = sealer.value("postgres", "KAGAMI_DB_PASSWORD")
    url = f"postgres://kagami:{password}@postgres:5432/shogun"
    assert sealer.value("kagami", "DATABASE_URL") == url


def test_the_hanko_derivation_matches_the_dev_pair_in_env_example():
    assert envfile.hanko_public_key(DEV_SEED) == DEV_PUBLIC


def test_tsubame_verifies_with_the_public_half_of_the_fude_key(sealer, cli):
    fetched(cli, sealer)
    seal_run(cli, sealer)
    public = envfile.hanko_public_key(sealer.value("fude", "FUDE_HANKO_SIGNING_KEY"))
    assert sealer.value("tsubame", "TSUBAME_HANKO_VERIFY_KEYS") == f"fude-1={public}"


def test_grafana_admin_is_sealed_for_monitoring_with_a_generated_password(sealer, cli):
    fetched(cli, sealer)
    result = seal_run(cli, sealer)
    assert sealer.keys("grafana-admin") == ["admin-password", "admin-user"]
    assert sealer.namespace("grafana-admin") == "monitoring"
    assert sealer.namespace("kagami") == "shogun"
    password = sealer.value("grafana-admin", "admin-password")
    assert len(password) >= 32
    assert envfile.read(sealer.env_file)["GRAFANA_ADMIN_PASSWORD"] == password
    assert password not in result.output


def test_the_pull_secret_needs_a_token_and_never_shows_it(sealer, cli):
    fetched(cli, sealer)
    seal_run(cli, sealer)
    assert not (sealer.paths.outdir / "ghcr.yaml").exists()
    with sealer.env_file.open("a") as env_file:
        env_file.write(f"GHCR_USER=octocat\nGHCR_TOKEN={SENTINEL}\n")
    result = seal_run(cli, sealer)
    assert result.exit_code == 0, result.output
    assert (sealer.paths.outdir / "ghcr.yaml").stat().st_size > 0
    config = (sealer.fake_dir / "ghcr.env").read_text()
    assert '"ghcr.io"' in config
    assert "octocat" in config
    assert SENTINEL not in result.output
    assert all(SENTINEL not in p.read_text() for p in sealer.paths.outdir.iterdir())
    assert len(sealer.listed()) == 13


def test_a_second_run_keeps_every_value(sealer, cli):
    fetched(cli, sealer)
    seal_run(cli, sealer)
    before = checksum(sealer.env_file)
    seal_run(cli, sealer)
    assert checksum(sealer.env_file) == before


def test_only_reseals_just_that_secret_and_keeps_the_kustomization(sealer, cli):
    fetched(cli, sealer)
    seal_run(cli, sealer)
    outdir = sealer.paths.outdir
    kagami, torii = checksum(outdir / "kagami.yaml"), checksum(outdir / "torii.yaml")
    result = seal_run(cli, sealer, "--only", "torii")
    assert result.exit_code == 0, result.output
    assert checksum(outdir / "kagami.yaml") == kagami
    assert checksum(outdir / "torii.yaml") != torii
    assert len(sealer.listed()) == 12


def test_up_without_a_terminal_stops_with_the_secrets_commands(sealer, cli):
    result = cli("up", "--env", "staging", "--context", "k3s", "--yes")
    assert result.exit_code == EXIT_WAITING
    assert "secrets fetch-cert --env staging --context k3s" in result.output
    assert "--from secrets" in result.output


def test_up_offers_to_seal_on_a_new_cluster(sealer, monkeypatch):
    monkeypatch.setattr(console, "interactive", lambda: True)
    monkeypatch.setattr(console, "confirm", lambda question: True)
    monkeypatch.setattr(seal.Paths, "env_file", property(lambda self: sealer.env_file))
    target = Target(sealer.root, "staging", "k3s", Tools.from_env(sealer.root))
    assert cli._seal_now(target, Runner(sealer.root, "staging"), yes=False)
    assert "FAKE CERT" in sealer.paths.cert.read_text()
    assert sealer.paths.key_backup.exists()
    assert len(sealer.sealed()) == 12


def test_the_env_file_is_created_private(tmp_path):
    path = tmp_path / "new" / "staging.env"
    previous = os.umask(0o022)
    try:
        envfile.fill(path)
    finally:
        os.umask(previous)
    assert mode(path) == 0o600
