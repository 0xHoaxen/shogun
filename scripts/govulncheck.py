#!/usr/bin/env python3
"""Run govulncheck on a module and fail on reachable vulnerabilities, except the
few listed in .govulncheck-allow.json.

    scripts/govulncheck.py <module-dir>

govulncheck over-approximates calls through interfaces, so the first frame of a
trace says little. An entry is therefore allowed on a structural test instead:
the vulnerable package may be imported only by the package named in the entry's
"only_imported_by" (the test database helper). If any other package in the
module imports it, directly or not, the finding fails the run, so the exemption
cannot hide a real exposure.
"""
import json
import os
import subprocess
import sys

ALLOW_FILE = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".govulncheck-allow.json")


def findings(module_dir):
    proc = subprocess.run(
        ["govulncheck", "-format", "json", "./..."],
        cwd=module_dir, capture_output=True, text=True,
    )
    if proc.returncode not in (0, 3) and not proc.stdout.strip():
        sys.exit(f"govulncheck failed to run in {module_dir}:\n{proc.stderr}")
    decoder, text, out = json.JSONDecoder(), proc.stdout, []
    pos = 0
    while pos < len(text):
        while pos < len(text) and text[pos].isspace():
            pos += 1
        if pos >= len(text):
            break
        message, pos = decoder.raw_decode(text, pos)
        finding = message.get("finding")
        # A trace that starts at a function is reachable; a package or module
        # level trace is only imported or required.
        if finding and finding["trace"][0].get("function"):
            out.append(finding)
    return out


def importers(module_dir, vulnerable_package):
    """Import paths of the packages in the module that depend on vulnerable_package."""
    proc = subprocess.run(
        ["go", "list", "-f", "{{.ImportPath}} {{join .Deps \" \"}}", "./..."],
        cwd=module_dir, capture_output=True, text=True, check=True,
    )
    found = set()
    for line in proc.stdout.splitlines():
        path, *deps = line.split()
        if vulnerable_package in deps or path == vulnerable_package:
            found.add(path)
    return found


def main():
    module_dir = sys.argv[1]
    allow = {e["id"]: e for e in json.load(open(ALLOW_FILE))}
    allowed, unexpected = {}, set()
    for f in findings(module_dir):
        osv, vulnerable = f["osv"], f["trace"][0]
        entry = allow.get(osv)
        if entry and importers(module_dir, vulnerable["package"]) <= {entry["only_imported_by"]}:
            allowed[osv] = entry
        else:
            unexpected.add((osv, vulnerable.get("module"), vulnerable["package"]))
    for osv, entry in sorted(allowed.items()):
        print(f"allowed {osv} in {module_dir}: {entry['reason']}")
    for osv, module, package in sorted(unexpected):
        print(f"VULNERABLE {osv}: {package} ({module})", file=sys.stderr)
    if unexpected:
        sys.exit(1)
    print(f"{module_dir}: no reachable vulnerabilities beyond the allowed ones")


if __name__ == "__main__":
    main()
