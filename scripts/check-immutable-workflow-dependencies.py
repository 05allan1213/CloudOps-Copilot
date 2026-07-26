#!/usr/bin/env python3
"""Audit GitHub workflow dependencies using only the Python standard library."""

from __future__ import annotations

import re
import sys
from pathlib import Path


WORKFLOW_ROOT = Path(".github/workflows")
DEPENDENCY_ROOTS = (WORKFLOW_ROOT, Path(".github/actions"))
YAML_SUFFIXES = {".yaml", ".yml"}
USES_PATTERN = re.compile(r"^\s*(?:-\s*)?uses:\s*(?P<value>.+?)\s*$")
ACTION_REF_PATTERN = re.compile(r"[^@\s]+@[0-9a-f]{40}")
DOCKER_REF_PATTERN = re.compile(r"docker://[^@\s]+@sha256:[0-9a-f]{64}")
OIDC_WRITE_PATTERN = re.compile(r"^\s*id-token:\s*write\s*(?:#.*)?$")


def yaml_files(root: Path) -> list[Path]:
    if not root.is_dir():
        return []
    return sorted(
        path
        for path in root.rglob("*")
        if path.is_file() and path.suffix.lower() in YAML_SUFFIXES
    )


def uses_value(line: str) -> str | None:
    content = line.split("#", 1)[0].rstrip()
    match = USES_PATTERN.match(content)
    if match is None:
        return None
    return match.group("value").strip().strip("'\"")


def main() -> int:
    workflow_files = yaml_files(WORKFLOW_ROOT)
    if not workflow_files:
        print("no GitHub workflow YAML files found", file=sys.stderr)
        return 1

    dependency_files = sorted(
        {path for root in DEPENDENCY_ROOTS for path in yaml_files(root)}
    )
    violations: list[str] = []
    external_dependencies = 0

    for path in dependency_files:
        for line_number, line in enumerate(
            path.read_text(encoding="utf-8").splitlines(), start=1
        ):
            value = uses_value(line)
            if value is None or value.startswith("./"):
                continue
            external_dependencies += 1
            if value.startswith("docker://"):
                if DOCKER_REF_PATTERN.fullmatch(value) is None:
                    violations.append(
                        f"{path}:{line_number}: Docker action must use @sha256:<64 hex>: {value}"
                    )
            elif ACTION_REF_PATTERN.fullmatch(value) is None:
                violations.append(
                    f"{path}:{line_number}: external action must use a 40-character commit SHA: {value}"
                )

    oidc_write_files = []
    for path in workflow_files:
        if any(
            OIDC_WRITE_PATTERN.match(line)
            for line in path.read_text(encoding="utf-8").splitlines()
        ):
            oidc_write_files.append(path)

    if len(oidc_write_files) != 1:
        rendered = ", ".join(str(path) for path in oidc_write_files) or "none"
        violations.append(
            "expected exactly one manually dispatched workflow file with id-token: write; "
            f"found {len(oidc_write_files)}: {rendered}"
        )

    if violations:
        print("immutable workflow dependency audit failed:", file=sys.stderr)
        for violation in violations:
            print(f"- {violation}", file=sys.stderr)
        return 1

    print(
        "PASS: immutable workflow dependency audit checked "
        f"{external_dependencies} external uses entries across "
        f"{len(dependency_files)} YAML files; id-token: write remains limited to 1 workflow."
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
