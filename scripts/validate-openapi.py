#!/usr/bin/env python3
"""Structural validation of the authoritative OpenAPI contract.

The generator accepts syntactically valid YAML that is still incomplete
(missing operation ids, unauthenticated tenant operations, empty responses).
This check enforces the project-specific contract rules.
"""
import sys

import yaml

CONTRACT = "api/openapi.yaml"
HTTP_METHODS = {"get", "put", "post", "delete", "patch"}
# Session lifecycle and self-permission discovery do not protect a resource.
# Liveness and readiness probes are unauthenticated by design.
SYSTEM_PATHS = {"/livez", "/readyz"}
# Exact allowlist: a /public prefix must never make future writes anonymous.
PUBLIC_READS = {("/public/places", "get"), ("/public/places/{slug}", "get")}
PUBLIC_IDENTITY = {("/public/auth/status", "get"), ("/public/auth/me", "get"), ("/public/auth/register", "post"), ("/public/auth/resend-verification", "post"), ("/public/auth/recover", "post"), ("/public/auth/verify-email", "post"), ("/public/auth/reset-password", "post"), ("/public/auth/login", "post"), ("/public/auth/refresh", "post"), ("/public/auth/logout", "post")}
PERMISSION_FREE_PATHS = {"/auth/logout", "/auth/logout-all", "/auth/password", "/users/me", "/authorization/permissions"}


def fail(message: str) -> None:
    print(f"openapi: {message}")
    sys.exit(1)


def main() -> None:
    try:
        with open(CONTRACT, encoding="utf-8") as handle:
            document = yaml.safe_load(handle)
    except (OSError, yaml.YAMLError) as error:
        fail(f"cannot parse {CONTRACT}: {error}")

    for key in ("openapi", "info", "paths", "components"):
        if key not in document:
            fail(f"missing required top-level key {key!r}")
    if not document["openapi"].startswith("3."):
        fail(f"unsupported OpenAPI version {document['openapi']!r}")

    schemas = document.get("components", {}).get("schemas", {})
    responses = document.get("components", {}).get("responses", {})
    parameters = document.get("components", {}).get("parameters", {})
    paths = document["paths"]
    if not paths:
        fail("contract declares no paths")

    seen_operation_ids = set()
    tenant_operations = 0
    platform_operations = 0
    for path, raw_path in sorted(paths.items()):
        if not isinstance(raw_path, dict):
            fail(f"{path}: path item must be a mapping")
        for method, operation in raw_path.items():
            if method == "parameters":
                continue
            if method not in HTTP_METHODS:
                fail(f"{path}: unexpected key {method!r}")
            operation_id = operation.get("operationId")
            if not operation_id:
                fail(f"{path} {method}: missing operationId")
            if operation_id in seen_operation_ids:
                fail(f"{path} {method}: duplicate operationId {operation_id!r}")
            seen_operation_ids.add(operation_id)
            if not operation.get("responses"):
                fail(f"{path} {method}: missing responses")
            for status in operation["responses"]:
                if not str(status).isdigit() and not str(status).startswith("default"):
                    fail(f"{path} {method}: invalid response status {status!r}")
            if (path, method) in PUBLIC_READS and operation.get("security") != []:
                fail(f"{path} {method}: public read must explicitly declare security: []")
            security = operation.get("security", [])
            bearer = any("bearerAuth" in entry for entry in security)
            consumer_bearer = any("consumerBearerAuth" in entry for entry in security)
            if consumer_bearer and (path, method) not in PUBLIC_IDENTITY:
                fail(f"{path} {method}: consumer capability not allowlisted")
            platform_bearer = any("platformBearerAuth" in entry for entry in security)
            if bearer:
                tenant_operations += 1
                if path not in PERMISSION_FREE_PATHS and "x-wheretolive-permission" not in operation:
                    fail(f"{path} {method}: tenant bearer operation lacks x-wheretolive-permission")
            elif platform_bearer:
                platform_operations += 1
            elif consumer_bearer:
                pass
            elif not security and not (path.startswith("/auth/") or path.startswith("/platform/auth/")) and path not in SYSTEM_PATHS and (path, method) not in PUBLIC_READS and (path, method) not in PUBLIC_IDENTITY:
                fail(f"{path} {method}: operation declares no security")
            if sum((bearer, platform_bearer, consumer_bearer)) > 1:
                fail(f"{path} {method}: operation mixes tenant and platform security")
        for reference in _collect_refs(raw_path):
            _check_ref(reference, paths, schemas, responses, parameters)

    if tenant_operations == 0 or platform_operations == 0:
        fail("contract must expose tenant and platform operations")


def _collect_refs(node):
    if isinstance(node, dict):
        for key, value in node.items():
            if key == "$ref" and isinstance(value, str):
                yield value
            else:
                yield from _collect_refs(value)
    elif isinstance(node, list):
        for item in node:
            yield from _collect_refs(item)


def _check_ref(reference: str, paths, schemas, responses, parameters) -> None:
    target = reference.split("#", 1)[-1].lstrip("/")
    section = {"components/schemas": schemas, "components/responses": responses, "components/parameters": parameters}
    resolved = None
    for prefix, table in section.items():
        if target.startswith(prefix):
            resolved = table
            remainder = target[len(prefix):].strip("/")
            break
    if resolved is None:
        fail(f"unsupported $ref target {reference!r}")
    name = remainder.split("/")[0]
    if name not in resolved:
        fail(f"$ref {reference!r} points at unknown component {name!r}")


if __name__ == "__main__":
    main()
