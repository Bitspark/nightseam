"""The profile's envelope grammar; application payloads stay opaque."""

import re

from .json import loads, raw_members

TRACE = {"traceparent", "tracestate"}
PARENT = re.compile(r"[0-9a-f]{2}-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}")


def require_name(value):
    if not isinstance(value, str) or not value:
        raise ValueError("expected nonempty string")


def decode_envelope(source, role):
    frame = loads(source)
    if not isinstance(frame, dict) or isinstance(frame.get("version"), bool) or frame.get("version") != 1:
        raise ValueError("invalid envelope version")
    if len(raw_members(source)) != len(frame):
        raise ValueError("duplicate envelope member")
    local, remote = ("c:", "s:") if role == "client" else ("s:", "c:")
    kind = frame.get("kind")
    common = {"version", "kind"} | TRACE
    if kind == "request":
        allowed = common | {"id", "method", "params", "meta"}
        require_name(frame.get("method"))
        if "params" not in frame:
            raise ValueError("missing params")
    elif kind == "response":
        allowed = common | {"id", "result", "error"}
        if ("result" in frame) == ("error" in frame):
            raise ValueError("expected result or error")
        if "error" in frame:
            error = frame["error"]
            if not isinstance(error, dict) or error.keys() - {"code", "message", "data"}:
                raise ValueError("invalid public error")
            require_name(error.get("code"))
            require_name(error.get("message"))
    elif kind == "event":
        allowed = common | {"event", "data", "meta"}
        require_name(frame.get("event"))
        if "data" not in frame:
            raise ValueError("missing event data")
    elif kind == "cancel":
        allowed = common | {"id"}
    else:
        raise ValueError("unknown envelope kind")
    if frame.keys() - allowed:
        raise ValueError("unknown envelope member")
    if kind != "event":
        request_id = frame.get("id")
        prefix = local if kind == "response" else remote
        if not isinstance(request_id, str) or not re.fullmatch(re.escape(prefix) + r"[1-9][0-9]{0,19}", request_id):
            raise ValueError("invalid request id")
    if "traceparent" in frame and (
        not isinstance(frame["traceparent"], str) or not PARENT.fullmatch(frame["traceparent"])
    ):
        raise ValueError("invalid traceparent")
    if "tracestate" in frame and not isinstance(frame["tracestate"], str):
        raise ValueError("invalid tracestate")
    if "meta" in frame:
        meta = frame["meta"]
        if not isinstance(meta, dict) or any(
            not isinstance(value, str) or key.startswith("nightseam.") for key, value in meta.items()
        ):
            raise ValueError("invalid meta")
    return frame
