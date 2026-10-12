"""The env file a cluster's Secrets are sealed from, and what goes in each Secret.

The file (default .secrets/<env>.env, git-ignored, KEY=value lines, no quoting)
is the source of truth. Anything generated is written back to it, so a second
run keeps every value: Postgres creates its roles once, so passwords must not
move. Values are never printed, only key and Secret names.
"""

import base64
import os
import re
import secrets
import subprocess
from pathlib import Path

from shogun_deploy import console

SERVICES = (
    "torii",
    "kagami",
    "tsubame",
    "fude",
    "taiko",
    "dojo",
    "katana",
    "shinobi",
    "sensei",
    "soroban",
)
PLACEHOLDER_ID = "replace-me.apps.googleusercontent.com"
PLACEHOLDER_SECRET = "replace-me"
PLACEHOLDER_EMAIL = "you@example.com"
SEED_BYTES = 32
# PKCS8 prefix of an Ed25519 private key; the 32-byte seed follows it.
ED25519_PKCS8_PREFIX = bytes.fromhex("302e020100300506032b657004220420")

# What each service reads beyond DATABASE_URL and IDENTITY_SIGNING_KEY (see .env.example).
SERVICE_KEYS = {
    "torii": (
        "TORII_PUBLIC_URL", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "TORII_ALLOWED_EMAILS",
    ),
    "fude": ("FUDE_HANKO_SIGNING_KEY", "FUDE_HANKO_KEY_ID", "ANTHROPIC_API_KEY"),
    "tsubame": (
        "TSUBAME_TOKEN_MASTER_KEY", "TSUBAME_TOKEN_KEY_ID", "TSUBAME_GMAIL_CLIENT_ID",
        "TSUBAME_GMAIL_CLIENT_SECRET", "TSUBAME_GMAIL_REDIRECT_URL", "TSUBAME_HANKO_VERIFY_KEYS",
        "ANTHROPIC_API_KEY",
    ),
    "katana": ("KATANA_GITHUB_USER", "KATANA_GITHUB_TOKEN", "ANTHROPIC_API_KEY"),
    "shinobi": ("ANTHROPIC_API_KEY",),
}  # fmt: skip
DB_PASSWORD = re.compile(r"^(POSTGRES_PASSWORD|[A-Z]+_DB_PASSWORD)$")


def _hex(nbytes: int) -> str:
    return secrets.token_hex(nbytes)


def _b64(nbytes: int) -> str:
    return base64.b64encode(secrets.token_bytes(nbytes)).decode()


def defaults() -> list[tuple[str, str]]:
    """Generated values and placeholders, in the order they are appended."""
    passwords = [(f"{svc.upper()}_DB_PASSWORD", _hex(24)) for svc in SERVICES]
    return [
        ("POSTGRES_PASSWORD", _hex(24)),
        *passwords,
        ("GRAFANA_ADMIN_PASSWORD", _hex(24)),
        ("IDENTITY_SIGNING_KEY", _hex(32)),
        ("TSUBAME_TOKEN_MASTER_KEY", _b64(32)),
        ("FUDE_HANKO_SIGNING_KEY", _b64(32)),
        ("FUDE_HANKO_KEY_ID", "fude-1"),
        ("TSUBAME_TOKEN_KEY_ID", "tsubame-1"),
        ("TORII_PUBLIC_URL", "http://localhost:3000"),
        ("TSUBAME_GMAIL_REDIRECT_URL", "http://localhost:3000/mail/callback"),
        ("GOOGLE_CLIENT_ID", PLACEHOLDER_ID),
        ("GOOGLE_CLIENT_SECRET", PLACEHOLDER_SECRET),
        ("TORII_ALLOWED_EMAILS", PLACEHOLDER_EMAIL),
        ("TSUBAME_GMAIL_CLIENT_ID", PLACEHOLDER_ID),
        ("TSUBAME_GMAIL_CLIENT_SECRET", PLACEHOLDER_SECRET),
    ]


def read(path: Path) -> dict[str, str]:
    """KEY=value lines; the last value of a key wins."""
    values: dict[str, str] = {}
    for line in path.read_text().splitlines():
        key, sep, value = line.partition("=")
        if sep and key and not key.startswith("#"):
            values[key] = value
    return values


def fill(path: Path) -> dict[str, str]:
    """Creates the file (mode 600) if needed and appends every missing default."""
    path.parent.mkdir(parents=True, exist_ok=True)
    os.close(os.open(path, os.O_CREAT | os.O_APPEND, 0o600))
    path.chmod(0o600)
    values = read(path)
    missing = [(k, v) for k, v in defaults() if not values.get(k)]
    if missing:
        text = path.read_text()
        lead = "\n" if text and not text.endswith("\n") else ""
        with path.open("a", encoding="utf-8") as env_file:
            env_file.write(lead + "".join(f"{k}={v}\n" for k, v in missing))
    return read(path)


def warn_unset(values: dict[str, str]) -> None:
    for key in (
        "GOOGLE_CLIENT_ID",
        "GOOGLE_CLIENT_SECRET",
        "TSUBAME_GMAIL_CLIENT_ID",
        "TSUBAME_GMAIL_CLIENT_SECRET",
    ):
        if values.get(key, "").startswith("replace-me"):
            console.warn(
                f"{key} is a placeholder; the service starts but Google sign-in "
                "or Gmail will not work"
            )
    if values.get("TORII_ALLOWED_EMAILS") == PLACEHOLDER_EMAIL:
        console.warn("TORII_ALLOWED_EMAILS is a placeholder; nobody can sign in")
    if not values.get("GHCR_TOKEN"):
        console.warn(
            "GHCR_TOKEN is not set; no pull secret is sealed, so private ghcr.io images "
            "cannot be pulled"
        )
    for key in ("ANTHROPIC_API_KEY", "KATANA_GITHUB_USER", "KATANA_GITHUB_TOKEN"):
        if not values.get(key):
            console.warn(
                f"{key} is not set; the services that use it start, but drafts or "
                "GitHub sync will fail"
            )


def require_openssl3() -> None:
    try:
        version = subprocess.run(
            ["openssl", "version"], capture_output=True, text=True, check=True
        ).stdout
    except (OSError, subprocess.CalledProcessError) as error:
        raise RuntimeError("openssl is needed to derive the Hanko public key") from error
    if not version.startswith("OpenSSL 3"):
        raise RuntimeError(
            f"OpenSSL 3 is needed to derive the Hanko public key (found: {version.strip()}); "
            "on macOS, brew install openssl and put it first on PATH"
        )


def hanko_public_key(seed_b64: str) -> str:
    """The base64 Ed25519 public key for a base64 32-byte seed, through openssl."""
    seed = base64.b64decode(seed_b64)
    if len(seed) != SEED_BYTES:
        raise ValueError(f"FUDE_HANKO_SIGNING_KEY must be base64 of {SEED_BYTES} bytes")
    der = subprocess.run(
        ["openssl", "pkey", "-inform", "DER", "-pubout", "-outform", "DER"],
        input=ED25519_PKCS8_PREFIX + seed,
        capture_output=True,
        check=True,
    ).stdout
    return base64.b64encode(der[-SEED_BYTES:]).decode()


def postgres_env(values: dict[str, str]) -> list[tuple[str, str]]:
    return [(k, v) for k, v in values.items() if DB_PASSWORD.match(k)]


def service_env(values: dict[str, str], svc: str) -> list[tuple[str, str]]:
    password = values[f"{svc.upper()}_DB_PASSWORD"]
    pairs = [
        ("DATABASE_URL", f"postgres://{svc}:{password}@postgres:5432/shogun"),
        ("IDENTITY_SIGNING_KEY", values["IDENTITY_SIGNING_KEY"]),
    ]
    for key in SERVICE_KEYS.get(svc, ()):
        value = values.get(key, "")
        if key == "TSUBAME_HANKO_VERIFY_KEYS" and not value:
            public = hanko_public_key(values["FUDE_HANKO_SIGNING_KEY"])
            value = f"{values['FUDE_HANKO_KEY_ID']}={public}"
        if value:
            pairs.append((key, value))
    return pairs


def grafana_env(values: dict[str, str]) -> list[tuple[str, str]]:
    return [("admin-user", "admin"), ("admin-password", values["GRAFANA_ADMIN_PASSWORD"])]
