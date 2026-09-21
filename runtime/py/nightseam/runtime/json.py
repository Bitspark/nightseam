"""JSON boundaries preserve scalar strings before decoding or serialization."""

import json as _json
from dataclasses import dataclass

UNICODE_ERROR = "invalid Unicode: expected Unicode scalar strings"


def scalar_value(value, seen=None):
    if isinstance(value, str):
        try:
            value.encode("utf8", "strict")
        except UnicodeError as error:
            raise ValueError(UNICODE_ERROR) from error
    elif isinstance(value, (dict, list, tuple)):
        seen = set() if seen is None else seen
        if id(value) in seen:
            return
        seen.add(id(value))
        if isinstance(value, dict):
            for key, member in value.items():
                scalar_value(key, seen)
                scalar_value(member, seen)
        else:
            for member in value:
                scalar_value(member, seen)
        seen.remove(id(value))


def _pairs(pairs):
    # A decoder would otherwise discard a malformed overwritten member.
    for key, value in pairs:
        scalar_value(key)
        scalar_value(value)
    return dict(pairs)


def _constant(value):
    raise ValueError("invalid JSON constant: " + value)


_decoder = _json.JSONDecoder(object_pairs_hook=_pairs, parse_constant=_constant)


def loads(source):
    if isinstance(source, bytes):
        try:
            source = source.decode("utf8", "strict")
        except UnicodeError as error:
            raise ValueError(UNICODE_ERROR) from error
    scalar_value(source)
    value = _decoder.decode(source)
    scalar_value(value)
    return value


def dumps(value):
    scalar_value(value)
    return _json.dumps(value, ensure_ascii=False, allow_nan=False, separators=(",", ":"))


@dataclass(frozen=True)
class RawJSON:
    """An explicit JSON source value, for forwarding without changing its bytes."""

    text: str

    def __post_init__(self):
        loads(self.text)

    def decode(self):
        return loads(self.text)


def raw_members(source):
    """Top-level object values, preserving spelling and duplicate keys."""
    value = loads(source)
    if not isinstance(value, dict):
        raise ValueError("expected JSON object")
    source = source.decode("utf8") if isinstance(source, bytes) else source
    at = source.index("{") + 1
    result = []

    def space(position):
        while position < len(source) and source[position] in " \t\r\n":
            position += 1
        return position

    at = space(at)
    while source[at] != "}":
        key, at = _decoder.raw_decode(source, at)
        at = space(at) + 1  # The full parse above has checked the colon.
        start = space(at)
        _, at = _decoder.raw_decode(source, start)
        result.append((key, RawJSON(source[start:at])))
        at = space(at)
        if source[at] == ",":
            at = space(at + 1)
    return result


def encode_object(members):
    """Serialize an envelope, leaving explicitly raw payloads as written."""
    return (
        "{"
        + ",".join(
            dumps(key) + ":" + (value.text if isinstance(value, RawJSON) else dumps(value))
            for key, value in members.items()
        )
        + "}"
    )
