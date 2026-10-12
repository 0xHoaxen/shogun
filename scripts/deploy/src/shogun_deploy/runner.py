"""Runs commands, keeps their output in the run's log file, shows it on failure."""

import subprocess
import time
from collections import deque
from dataclasses import dataclass
from datetime import UTC, datetime
from pathlib import Path

from shogun_deploy import console

TAIL_LINES = 30
# Values that follow these flags never reach the log or the screen.
SECRET_FLAGS = frozenset({"--set", "--set-string", "--set-file", "--password"})
REDACTED = "<redacted>"


@dataclass(frozen=True)
class Result:
    code: int
    output: tuple[str, ...]

    @property
    def ok(self) -> bool:
        return self.code == 0


def redact(argv: list[str]) -> list[str]:
    shown, hide_next = [], False
    for arg in argv:
        shown.append(REDACTED if hide_next else arg)
        hide_next = arg in SECRET_FLAGS
    return shown


class Runner:
    """One per run. Every command's output goes to one log file under .deploy-logs/."""

    def __init__(self, root: Path, env: str, verbose: bool = False) -> None:
        stamp = datetime.now(UTC).strftime("%Y%m%dT%H%M%SZ")
        self.log_path = root / ".deploy-logs" / f"{stamp}-{env}.log"
        self.log_path.parent.mkdir(parents=True, exist_ok=True)
        self.verbose = verbose

    def _log(self, text: str) -> None:
        with self.log_path.open("a", encoding="utf-8") as log:
            log.write(text)

    def run(self, argv: list[str], title: str, *, capture: bool = False) -> Result:
        """Runs argv with a spinner (or live output with -v). capture keeps stdout only."""
        self._log(f"\n$ {' '.join(redact(argv))}\n")
        started = time.monotonic()
        last: deque[str] = deque(maxlen=TAIL_LINES)
        lines: list[str] = []
        try:
            process = subprocess.Popen(
                argv,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE if capture else subprocess.STDOUT,
                text=True,
            )
        except FileNotFoundError:
            message = f"{argv[0]} is not installed or not on PATH"
            self._log(message + "\n")
            return Result(127, (message,))

        with console.err.status(f"[dim]{title}[/]") as status:
            assert process.stdout is not None
            for line in process.stdout:
                line = line.rstrip("\n")
                self._log(line + "\n")
                last.append(line)
                if capture:
                    lines.append(line)
                if self.verbose:
                    console.info(line)
                status.update(f"[dim]{title} ({time.monotonic() - started:.0f}s)[/]")
            stderr = process.stderr.read() if capture and process.stderr else ""
            code = process.wait()
        if stderr:
            self._log(stderr)
            last.extend(stderr.splitlines())
        self._log(f"# exit {code} after {time.monotonic() - started:.1f}s\n")
        return Result(code, tuple(lines if capture else last))

    def secret(self, argv: list[str], title: str, stdin: str = "") -> str:
        """Runs argv whose input or output holds secrets: neither is logged or shown.

        Only the command line, the exit code and stderr reach the log. Returns
        stdout; raises StepFailed when the command fails.
        """
        self._log(f"\n$ {' '.join(redact(argv))}  # output not logged\n")
        try:
            done = subprocess.run(argv, input=stdin, capture_output=True, text=True, check=False)
        except FileNotFoundError as error:
            console.fail(f"{title}: {argv[0]} is not installed or not on PATH")
            raise StepFailed(title) from error
        self._log(done.stderr + f"# exit {done.returncode}\n")
        if done.returncode != 0:
            console.fail(f"{title} (exit {done.returncode})")
            console.tail(done.stderr.splitlines()[-TAIL_LINES:])
            raise StepFailed(title)
        return done.stdout

    def must(self, argv: list[str], title: str, *, capture: bool = False) -> Result:
        """Runs argv and reports it; on failure shows the tail and the log path."""
        started = time.monotonic()
        result = self.run(argv, title, capture=capture)
        if result.ok:
            console.ok(f"{title} [dim]({time.monotonic() - started:.0f}s)[/]")
            return result
        console.fail(f"{title} (exit {result.code})")
        console.tail(list(result.output)[-TAIL_LINES:])
        console.info(f"full output: {self.log_path}")
        raise StepFailed(title)


class StepFailed(Exception):
    """A command the run depends on failed; already reported."""
