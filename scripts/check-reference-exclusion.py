#!/usr/bin/env python3
"""Reference-source exclusion check.

`_reference/**` and the legacy `SpringBlade-boot/**` trees are read-only
research material. Production code, build files, and dependency manifests
must never reference, import, or execute them, and they must not be committed.
"""
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
FORBIDDEN = ("_reference", "SpringBlade-boot")
SCANNED_SUFFIXES = (".go", ".mod", ".sum", ".yaml", ".yml", ".json", ".toml", ".mk", ".ts", ".tsx")
SCANNED_NAMES = ("Makefile", "Dockerfile", ".gitignore")


def fail(message: str) -> None:
    print(f"reference-exclusion: {message}")
    sys.exit(1)


def main() -> None:
    committed = [name for name in FORBIDDEN if (ROOT / name).exists()]
    if committed:
        fail(f"reference tree present in repository: {', '.join(committed)}")

    scan_targets = []
    for path in sorted(ROOT.rglob("*")):
        if not path.is_file():
            continue
        relative = path.relative_to(ROOT)
        parts = relative.parts
        if parts and parts[0] in FORBIDDEN:
            continue
        if any(part in FORBIDDEN for part in parts):
            continue
        if path.name in SCANNED_NAMES or path.suffix in SCANNED_SUFFIXES:
            if any(part in (".git", "node_modules", ".cache", "dist") for part in parts):
                continue
            scan_targets.append(path)

    for path in scan_targets:
        try:
            content = path.read_text(encoding="utf-8")
        except UnicodeDecodeError:
            continue
        for marker in FORBIDDEN:
            if marker in content:
                fail(f"{path.relative_to(ROOT)} references excluded reference source {marker!r}")
    print(f"reference-exclusion: {len(scan_targets)} files verified clean")


if __name__ == "__main__":
    main()
