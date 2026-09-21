"""Local proof that one send was refused before admission."""

import asyncio

from .peer import ABSENT, PublicError


class UnpublishedError(PublicError):
    """This attempt was not queued; the cause retains its ordinary refusal."""

    def __init__(self, cause):
        self.cause = cause
        code = getattr(cause, "code", "cancelled" if isinstance(cause, asyncio.CancelledError) else "send_failed")
        message = str(cause) or "Operation was refused before admission"
        super().__init__(code, message, getattr(cause, "data", ABSENT))


class _UnpublishedCancellation(UnpublishedError, asyncio.CancelledError):
    """Retain asyncio's cancellation protocol beside local admission proof."""


def unpublished(error):
    """Only call at an admission boundary that can prove this send was refused."""
    if isinstance(error, asyncio.CancelledError):
        return _UnpublishedCancellation(error)
    return None if error is None else UnpublishedError(error)


def without_unpublished_proof(error):
    """A delivered operation cannot borrow proof from a nested send attempt."""
    if isinstance(error, UnpublishedError):
        return PublicError(error.code, str(error), error.data)
    return error
