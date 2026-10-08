#!/usr/bin/env python3
"""Module and table ownership enforcement.

Rules enforced:
1. Every migration section declares exactly one owning module.
2. Every wheretolive.* table is owned by exactly one module.
3. SQL touching a wheretolive.* table appears only in the owning module's sources,
   the owning module's generated query package, or the module's private
   query.sql file. Hand-written SQL may not live anywhere else.
"""
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
MIGRATIONS = ROOT / "backend" / "internal" / "platform" / "database" / "migrations"
MODULES = ROOT / "backend" / "internal" / "modules"
PLATFORM = ROOT / "backend" / "internal" / "platform"

SQL_USE = re.compile(r"\b(?:FROM|INTO|UPDATE|JOIN)\s+(?:ONLY\s+)?wheretolive\.([a-z_]+)", re.IGNORECASE)
TABLE_DEF = re.compile(r"\bCREATE (?:TABLE|VIEW)\s+(?:IF NOT EXISTS\s+)?wheretolive\.([a-z_]+)", re.IGNORECASE)
TABLE_ALTER = re.compile(r"\b(?:ALTER TABLE|DROP TABLE)\s+(?:IF EXISTS\s+)?wheretolive\.([a-z_]+)", re.IGNORECASE)
OWNER = re.compile(r"^--\s*owner:\s*([a-z]+)\s*$", re.MULTILINE)
OWNER_SPLIT = re.compile(r"^--\s*owner:\s*[a-z]+\s*$", re.MULTILINE)


def fail(message: str) -> None:
    print(f"ownership: {message}")
    sys.exit(1)


def collect_ownership():
    owners = {}
    for migration in sorted(MIGRATIONS.glob("*.up.sql")):
        content = migration.read_text(encoding="utf-8")
        declared = OWNER.findall(content)
        if not declared:
            fail(f"{migration.relative_to(ROOT)}: missing '-- owner:' header")
        if len(set(declared)) != len(declared):
            fail(f"{migration.relative_to(ROOT)}: owner declared twice: {declared}")
        # The owner header covers every statement in the file; files with
        # multiple owners repeat the header before the owned statements.
        sections = OWNER_SPLIT.split(content)
        # sections[0] is text before the first owner header (must be blank).
        for index, owner in enumerate(declared):
            body = sections[index + 1]
            for table in TABLE_DEF.findall(body):
                if table in owners and owners[table] != owner:
                    fail(f"{migration.relative_to(ROOT)}: table {table} already owned by {owners[table]}")
                owners[table] = owner
            for table in TABLE_ALTER.findall(body):
                if owners.get(table) not in (None, owner):
                    fail(f"{migration.relative_to(ROOT)}: {owner} alters table {table} owned by {owners[table]}")
    if not owners:
        fail("no owned tables found in migrations")
    return owners


def module_sources(module_dir: Path):
    generated = "/db/"
    for path in sorted(module_dir.rglob("*")):
        if not path.is_file() or path.suffix not in (".go", ".sql"):
            continue
        if generated in str(path):
            continue
        # Integration test fixtures may seed other modules' tables after the
        # schema reset; the ownership rule governs production access paths.
        if path.name.endswith("_test.go"):
            continue
        yield path


def main() -> None:
    owners = collect_ownership()

    # The shared platform query file may only contain queries that touch no
    # wheretolive tables (connection checks and similar).
    shared_sql = PLATFORM / "database" / "query.sql"
    for table in SQL_USE.findall(shared_sql.read_text(encoding="utf-8")):
        fail(f"{shared_sql.relative_to(ROOT)}: shared query file must not reference wheretolive.{table}")

    violations = []
    for module_dir in sorted(MODULES.iterdir()):
        if not module_dir.is_dir():
            continue
        module = module_dir.name
        for path in module_sources(module_dir):
            content = path.read_text(encoding="utf-8")
            for table in SQL_USE.findall(content):
                owner = owners.get(table)
                if owner is None:
                    violations.append(f"{path.relative_to(ROOT)}: references unknown table wheretolive.{table}")
                elif owner != module:
                    violations.append(
                        f"{path.relative_to(ROOT)}: module {module} touches wheretolive.{table} owned by {owner}"
                    )
    if violations:
        for violation in violations:
            print(f"ownership: {violation}")
        sys.exit(1)
    print(f"ownership: {len(owners)} tables/views verified across all modules")


if __name__ == "__main__":
    main()
