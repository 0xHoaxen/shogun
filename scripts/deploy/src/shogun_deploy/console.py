"""Everything the user sees: step headers, results, tables and questions."""

import sys

from rich.console import Console
from rich.prompt import Confirm, Prompt
from rich.table import Table

# Progress and results go to stderr, so stdout stays clean for tables a script
# might read.
err = Console(stderr=True, highlight=False)
out = Console(highlight=False)


def interactive() -> bool:
    return sys.stdin.isatty() and sys.stderr.isatty()


def step(index: int, total: int, title: str) -> None:
    err.print(f"\n[bold cyan][{index}/{total}][/] [bold]{title}[/]")


def ok(message: str) -> None:
    err.print(f"  [green]ok[/]    {message}")


def warn(message: str) -> None:
    err.print(f"  [yellow]warn[/]  {message}")


def fail(message: str) -> None:
    err.print(f"  [red]fail[/]  {message}")


def info(message: str) -> None:
    err.print(f"  [dim]{message}[/]")


def tail(lines: list[str]) -> None:
    """The last lines of a failed command, indented under its step."""
    for line in lines:
        err.print(f"  [dim]│[/] {line}", markup=False, highlight=False)


def table(title: str, columns: list[str], rows: list[list[str]]) -> None:
    grid = Table(title=title, title_justify="left", header_style="bold")
    for column in columns:
        grid.add_column(column)
    for row in rows:
        grid.add_row(*row)
    out.print(grid)


def choose(question: str, choices: list[str]) -> str:
    for number, choice in enumerate(choices, 1):
        err.print(f"  {number}. {choice}")
    picked = Prompt.ask(
        question,
        choices=[str(n) for n in range(1, len(choices) + 1)],
        console=err,
    )
    return choices[int(picked) - 1]


def confirm(question: str) -> bool:
    return Confirm.ask(question, console=err, default=False)


def type_to_confirm(question: str, expected: str) -> bool:
    answer = Prompt.ask(f"{question} Type [bold]{expected}[/] to go on", console=err)
    return answer.strip() == expected
