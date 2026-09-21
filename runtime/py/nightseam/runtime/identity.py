"""Declaration agreement as an ordinary, transport-independent request."""

import re

from .json import scalar_value
from .peer import PublicError

IDENTITY_METHOD = "identity.check"


def _read_identity(raw):
    def invalid(message):
        raise PublicError("contract_invalid", message)

    if not isinstance(raw, dict):
        invalid("a declaration identity is an object with path and optional digest")
    for key in raw:
        if key not in ("path", "digest"):
            invalid("identity: unknown field " + str(key))
    path = raw.get("path")
    if not isinstance(path, str) or not path:
        invalid("an identity names a nonempty Unicode declaration path")
    try:
        scalar_value(path)
    except ValueError:
        invalid("an identity names a nonempty Unicode declaration path")
    result = {"path": path}
    if "digest" in raw:
        digest = raw["digest"]
        if not isinstance(digest, str) or not re.fullmatch("[0-9a-f]{64}", digest):
            invalid("a declaration digest is lowercase SHA-256 hex")
        result["digest"] = digest
    return result


def _compare(local, remote):
    if local["path"] != remote["path"] or (
        "digest" in local and "digest" in remote and local["digest"] != remote["digest"]
    ):
        raise PublicError("contract_mismatch", "the declaration identity for " + local["path"] + " differs")


def identity_handler(expected):
    """Snapshot an identity and answer requests without running model code."""
    local = _read_identity(expected)

    def handle(value, context=None):
        _compare(local, _read_identity(value))
        return dict(local)

    return handle


async def check_identity(call, expected, *, timeout_ms=30_000, context=None):
    """Check before exposing a model; only method_not_found means absence.

    The supplied call must honor its timeout and cancellation. Refusal leaves
    the carrier open, and unspecified digests do not compare revisions.
    """
    local = _read_identity(expected)
    try:
        remote = await call(IDENTITY_METHOD, local, timeout_ms=timeout_ms, context=context)
    except PublicError as error:
        if error.code == "method_not_found":
            return
        raise
    _compare(local, _read_identity(remote))
