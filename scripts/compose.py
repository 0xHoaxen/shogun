#!/usr/bin/env python3
"""Maintain the per-service entries of the local compose stack.

Usage: scripts/compose.py add NAME

Adds a service block to deploy/compose/compose.yaml (next free host ports), its
<NAME>_ADDR line, its password pass-through for Postgres, and its dev password
to .env.example. Idempotent: does nothing if the service is already present.
"""
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
COMPOSE = ROOT / "deploy/compose/compose.yaml"
ENV_EXAMPLE = ROOT / ".env.example"
FIRST_HTTP_PORT = 8081
FIRST_GRPC_PORT = 9091

STANZA = """  {name}:
    <<: *service
    image: shogun-{name}:local
    build:
      <<: *build
      args:
        <<: *build-args
        SERVICE: {name}
    environment:
      <<: *env
      DATABASE_URL: postgres://{name}:${{{upper}_DB_PASSWORD:?copy .env.example to .env}}@postgres:5432/shogun
    ports:
      - "127.0.0.1:{http}:8080"
      - "127.0.0.1:{grpc}:9090"

"""


def insert_before(text: str, marker: str, block: str) -> str:
    if marker not in text:
        sys.exit(f"compose.py: marker {marker!r} missing from {COMPOSE}")
    return text.replace(marker, block + marker, 1)


def add(name: str) -> None:
    if not re.fullmatch(r"[a-z][a-z0-9]*", name):
        sys.exit(f"compose.py: invalid service name {name!r}")
    upper = name.upper()
    text = COMPOSE.read_text()
    if re.search(rf"^  {name}:$", text, re.M):
        print(f"compose.py: {name} already in compose.yaml")
        return

    http_ports = [int(p) for p in re.findall(r'"127\.0\.0\.1:(\d+):8080"', text)]
    grpc_ports = [int(p) for p in re.findall(r'"127\.0\.0\.1:(\d+):9090"', text)]
    http = max(http_ports, default=FIRST_HTTP_PORT - 1) + 1
    grpc = max(grpc_ports, default=FIRST_GRPC_PORT - 1) + 1

    text = insert_before(text, "  # services end\n", STANZA.format(name=name, upper=upper, http=http, grpc=grpc))
    text = insert_before(text, "  # addr-env end\n", f"  {upper}_ADDR: {name}:9090\n")
    text = insert_before(
        text, "      # postgres-env end\n", f"      {upper}_DB_PASSWORD: ${{{upper}_DB_PASSWORD:?copy .env.example to .env}}\n"
    )
    COMPOSE.write_text(text)

    env = ENV_EXAMPLE.read_text()
    line = f"{upper}_DB_PASSWORD=local-dev-{name}\n"
    if line not in env:
        ENV_EXAMPLE.write_text(env.rstrip("\n") + "\n" + line)
    print(f"compose.py: added {name} (http {http}, grpc {grpc})")


def main() -> None:
    if len(sys.argv) != 3 or sys.argv[1] != "add":
        sys.exit(__doc__)
    add(sys.argv[2])


if __name__ == "__main__":
    main()
