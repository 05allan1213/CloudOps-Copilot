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
TOP_LEVEL_ON_PATTERN = re.compile(r"^on:\s*(?P<value>.*?)\s*$")
MAPPING_KEY_PATTERN = re.compile(r"^(?P<indent>\s+)(?P<key>[A-Za-z0-9_-]+):")


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


def workflow_is_dispatch_only(lines: list[str]) -> bool:
    for index, line in enumerate(lines):
        content = line.split("#", 1)[0].rstrip()
        match = TOP_LEVEL_ON_PATTERN.match(content)
        if match is None:
            continue

        value = match.group("value").strip()
        if value:
            if value == "workflow_dispatch":
                return True
            if value.startswith("[") and value.endswith("]"):
                triggers = {
                    trigger.strip().strip("'\"")
                    for trigger in value[1:-1].split(",")
                    if trigger.strip()
                }
                return triggers == {"workflow_dispatch"}
            return False

        block_lines: list[str] = []
        for candidate in lines[index + 1 :]:
            candidate_content = candidate.split("#", 1)[0].rstrip()
            if not candidate_content:
                continue
            if not candidate_content[0].isspace():
                break
            block_lines.append(candidate_content)

        mapping_entries = [
            MAPPING_KEY_PATTERN.match(candidate) for candidate in block_lines
        ]
        mapping_entries = [entry for entry in mapping_entries if entry is not None]
        if not mapping_entries:
            return False
        direct_indent = min(len(entry.group("indent")) for entry in mapping_entries)
        triggers = {
            entry.group("key")
            for entry in mapping_entries
            if len(entry.group("indent")) == direct_indent
        }
        return triggers == {"workflow_dispatch"}

    return False


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

    oidc_write_files: list[Path] = []
    for path in workflow_files:
        lines = path.read_text(encoding="utf-8").splitlines()
        if any(
            OIDC_WRITE_PATTERN.match(line)
            for line in lines
        ):
            oidc_write_files.append(path)
            if not workflow_is_dispatch_only(lines):
                violations.append(
                    f"{path}: id-token: write is only allowed in a workflow_dispatch-only workflow"
                )

    if len(oidc_write_files) > 1:
        rendered = ", ".join(str(path) for path in oidc_write_files) or "none"
        violations.append(
            "expected at most one workflow file with id-token: write; "
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
        f"{len(dependency_files)} YAML files; id-token: write is limited to "
        f"{len(oidc_write_files)} workflow(s) and requires dispatch-only triggering."
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
