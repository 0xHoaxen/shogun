#!/usr/bin/env python3
"""Keep the per-service depguard rules in .golangci.yml in sync.

Usage: scripts/depguard.py add NAME

Adds a svc-NAME rule that denies every other service, and adds NAME to the deny
list of every existing svc-* rule. Safe to run twice.
"""
import re
import sys
from pathlib import Path

CONFIG = Path(__file__).resolve().parent.parent / ".golangci.yml"
MODULE = "github.com/0xHoaxen/shogun/services"
RULE_INDENT = " " * 8
HEADER = re.compile(r"^ {8}svc-([a-z][a-z0-9]*):$")


def deny_entry(name: str) -> list[str]:
    return [
        f'            - pkg: "{MODULE}/{name}"',
        "              desc: services must not import other services; use gRPC",
    ]


def find_blocks(lines: list[str]) -> dict[str, tuple[int, int]]:
    """Map service name to the [start, end) line range of its rule."""
    blocks: dict[str, tuple[int, int]] = {}
    start, current = -1, ""
    for i, line in enumerate(lines + [""]):
        header = HEADER.match(line)
        ends_block = line and not line.startswith(" " * 9)
        if current and ends_block:
            blocks[current] = (start, i)
            current = ""
        if header:
            start, current = i, header.group(1)
    return blocks


def add(name: str) -> None:
    lines = CONFIG.read_text().splitlines()
    blocks = find_blocks(lines)
    entry = f'{MODULE}/{name}"'

    # Insert bottom-up so the recorded line ranges stay valid.
    for other, (start, stop) in sorted(blocks.items(), key=lambda kv: -kv[1][0]):
        if other != name and entry not in "\n".join(lines[start:stop]):
            lines[stop:stop] = deny_entry(name)

    if name not in blocks:
        insert_at = find_blocks(lines)[max(blocks, key=lambda n: blocks[n][0])][1]
        rule = [
            f"{RULE_INDENT}svc-{name}:",
            "          list-mode: lax",
            "          files:",
            f'            - "**/services/{name}/**"',
            "          deny:",
        ]
        for other in blocks:
            rule += deny_entry(other)
        lines[insert_at:insert_at] = rule

    CONFIG.write_text("\n".join(lines) + "\n")


def main() -> int:
    if len(sys.argv) != 3 or sys.argv[1] != "add":
        print(__doc__, file=sys.stderr)
        return 2
    add(sys.argv[2])
    return 0


if __name__ == "__main__":
    sys.exit(main())
