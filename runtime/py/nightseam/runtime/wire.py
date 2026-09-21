"""Relative request/event helpers and private local delivery context.

The roots own queues and capacity. These helpers own one call's return
capability or a registration's handler lifetime, never a carrier or owner.
"""

import asyncio
import inspect
import itertools
import time
import weakref
from dataclasses import dataclass, field

from nightseam.duplex import Message, Receiver, ReturnAddress, WireError, encode_path

from .envelope import decode_envelope
from .json import RawJSON, encode_object, loads
from .peer import ABSENT, DefaultPropagator, PublicError
from .publication import UnpublishedError


@dataclass
class WireRequestContext:
    wire: object = None
    request_id: str = ""
    cancelled: asyncio.Event = field(default_factory=asyncio.Event)
    trace: object = None
    meta: object = ABSENT
    raw: RawJSON | None = None

    def __getattr__(self, name):
        source = self.__dict__.get("_source_context")
        if source is not None:
            return getattr(source, name)
        raise AttributeError(name)


@dataclass
class WireEventContext:
    wire: object = None
    trace: object = None
    meta: object = ABSENT
    raw: RawJSON | None = None

    def __getattr__(self, name):
        source = self.__dict__.get("_source_context")
        if source is not None:
            return getattr(source, name)
        raise AttributeError(name)


@dataclass
class WireHandlers:
    request: object = None
    event: object = None
    observer: object = None
    family: str = ""


@dataclass
class WireDispatchContext:
    context: object
    panic: object = None
    max_frame_bytes: int | None = None


@dataclass
class WireEventDispatchContext:
    context: object
    panic: object = None


class _IdentityAssociations:
    """Weak associations keyed by object identity, independent of user equality."""

    def __init__(self):
        self.entries = {}

    def get(self, key):
        held = self.entries.get(id(key))
        return held[1] if held is not None and held[0]() is key else None

    def set(self, key, value):
        identity = id(key)

        def expired(reference):
            held = self.entries.get(identity)
            if held is not None and held[0] is reference:
                self.entries.pop(identity, None)

        reference = weakref.ref(key, expired)
        self.entries[identity] = reference, value

        def cleanup():
            held = self.entries.get(identity)
            if held is not None and held[0] is reference and held[1] is value:
                self.entries.pop(identity, None)

        return cleanup


_dispatch_contexts = _IdentityAssociations()
_event_contexts = _IdentityAssociations()


def wire_context(address):
    return _dispatch_contexts.get(address)


def set_wire_context(address, dispatch):
    return _dispatch_contexts.set(address, dispatch)


def inherit_wire_context(source, target):
    dispatch = wire_context(source)
    return set_wire_context(target, dispatch) if dispatch is not None else lambda: None


def wire_event_context(message):
    return _event_contexts.get(message.return_address) if message.return_address is not None else None


class _EventContextWire:
    def send(self, path, message):
        raise PublicError("invalid_message", "An event context is not a return address")

    def receive(self, path, receiver):
        raise WireError("receiver_exists")

    def close(self, code=1000, reason=""):
        pass


_event_context_carrier = _EventContextWire()


def with_wire_event_context(message, context, panic=None):
    address = ReturnAddress(_event_context_carrier)
    _event_contexts.set(address, WireEventDispatchContext(context, panic))
    return Message(message.frame, address)


def clone_context(source=None, *, context_type=WireRequestContext, **overrides):
    """Preserve received private attributes; explicit fields describe this hop."""
    context = context_type()
    if source is not None:
        context.__dict__.update(getattr(source, "__dict__", {}))
        context._source_context = source
    context.__dict__.update(overrides)
    return context


def compose_cancellation(*events):
    """Join request-local Events; cleanup removes the retained watchers."""
    events = tuple({id(event): event for event in events if event is not None}.values())
    if len(events) == 1:
        return events[0], lambda: None

    class JoinedEvent(asyncio.Event):
        def is_set(self):
            return super().is_set() or any(event.is_set() for event in events)

        async def wait(self):
            if self.is_set():
                return True
            return await super().wait()

    combined = JoinedEvent()
    tasks = []

    def cleanup():
        if any(event.is_set() for event in events):
            combined.set()
        for task in tasks:
            task.cancel()
        tasks.clear()

    if any(event.is_set() for event in events):
        combined.set()
    else:

        async def wait(event):
            await event.wait()
            combined.set()

        tasks.extend(asyncio.create_task(wait(event)) for event in events)
    return combined, cleanup


def trace_of(frame):
    return {key: frame[key] for key in ("traceparent", "tracestate") if key in frame}


class _ProfileSnapshot(dict):
    pass


def with_outgoing_trace(frame, trace):
    saved = _ProfileSnapshot(frame)
    saved._outgoing_trace = trace
    return saved


def outgoing_trace(frame):
    return getattr(frame, "_outgoing_trace", trace_of(frame))


def _snapshot(value):
    try:
        # encode_object also understands explicit RawJSON payloads.
        return (
            loads(encode_object(value)) if isinstance(value, dict) else loads(encode_object({"value": value}))["value"]
        )
    except (ValueError, TypeError, OverflowError, RecursionError) as error:
        raise PublicError("invalid_message", "Frame must contain serializable JSON values") from error


def profile_frame(value, name, max_frame_bytes=None):
    """Validate a structured snapshot using the physical envelope grammar."""
    saved = _snapshot(value)
    if not isinstance(saved, dict) or "method" in saved or "event" in saved:
        raise PublicError("invalid_message", "Invalid structured profile frame")
    envelope = dict(saved)
    if saved.get("kind") == "request":
        envelope["method"] = name
    elif saved.get("kind") == "event":
        envelope["event"] = name
    text = encode_object(envelope)
    if max_frame_bytes is not None and len(text.encode("utf8")) > max_frame_bytes:
        raise PublicError("frame_too_large", "Outgoing frame exceeds the size limit")
    # Logical ids belong to local return capabilities, not physical roles.
    local = "server" if str(saved.get("id", "")).startswith("s:") else "client"
    role = local if saved.get("kind") == "response" else "client" if local == "server" else "server"
    try:
        decode_envelope(text, role)
    except (ValueError, TypeError) as error:
        raise PublicError("invalid_message", "Invalid structured profile frame") from error
    return with_outgoing_trace(saved, outgoing_trace(value)) if hasattr(value, "_outgoing_trace") else saved


def public_error(error):
    """Normalize public fields and discard proof belonging to a nested send."""
    if (
        isinstance(error, PublicError)
        and isinstance(error.code, str)
        and error.code
        and isinstance(str(error), str)
        and str(error)
    ):
        return PublicError(error.code, str(error), error.data)
    return PublicError("internal", "Request handler failed")


def response(request, result=ABSENT, error=None):
    """Send a validated response or bounded fallback; return its chosen outcome."""
    if request.frame.get("kind") != "request" or request.return_address is None:
        return PublicError("disconnected", "The request has no return address")
    outcome = None
    try:
        if error is not None:
            refusal = public_error(error)
            outcome = (
                error
                if isinstance(error, PublicError) and (error.code == refusal.code and str(error) == str(refusal))
                else refusal
            )
            payload = _snapshot({"error": refusal.envelope()})
        else:
            payload = {"result": _snapshot(None if result is ABSENT else result)}
    except (ValueError, TypeError, PublicError):
        outcome = PublicError("internal", "Response could not be encoded")
        payload = {"error": outcome.envelope()}
    frame = {"version": 1, "kind": "response", "id": request.frame["id"], **payload, **trace_of(request.frame)}
    try:
        request.return_address.wire.send([], Message(frame))
    except Exception as failure:
        if isinstance(failure, PublicError) and failure.code in ("invalid_message", "frame_too_large"):
            outcome = PublicError("internal", "Response could not be encoded")
            try:
                request.return_address.wire.send(
                    [],
                    Message(
                        {
                            "version": 1,
                            "kind": "response",
                            "id": request.frame["id"],
                            "error": outcome.envelope(),
                            **trace_of(request.frame),
                        }
                    ),
                )
            except Exception:
                pass
        elif outcome is None:
            outcome = public_error(failure)
    return outcome


def observe(observer, event_type, **members):
    if observer is None:
        return
    try:
        (observer.observe if hasattr(observer, "observe") else observer)(
            {"type": event_type, "at": time.monotonic(), **members}
        )
    except Exception:
        pass


_observations = itertools.count(1)


def _observe_request(observer, family, name, incoming, trace):
    if observer is None:
        return lambda error=None, outcome=None: None
    request_id, started = "wire:" + str(next(_observations)), time.monotonic()
    ended = False
    observe(observer, "request.started", id=request_id, method=name, incoming=incoming, trace=trace, family=family)

    def finish(error=None, outcome=None):
        nonlocal ended
        if ended:
            return
        ended = True
        observe(
            observer,
            "request.ended",
            id=request_id,
            method=name,
            incoming=incoming,
            duration=(time.monotonic() - started) * 1000,
            trace=trace,
            family=family,
            outcome=outcome or ("error" if error is not None else "ok"),
            error_code=getattr(error, "code", "internal" if error is not None else ""),
        )

    return finish


def _carriage(frame, meta):
    if meta is ABSENT:
        return frame
    if not isinstance(meta, dict) or any(not isinstance(k, str) or not isinstance(v, str) for k, v in meta.items()):
        raise PublicError("invalid_message", "meta must be an object of strings")
    frame["meta"] = {key: value for key, value in meta.items() if not key.startswith("nightseam.")}
    return frame


class _Returning:
    def __init__(self, future, dispatch=None):
        self.future, self.dispatch = future, dispatch

    def send(self, path, message):
        limit = self.dispatch.max_frame_bytes if self.dispatch else None
        frame = profile_frame(message.frame, "", limit)
        if path or frame["kind"] != "response" or frame["id"] != "c:1":
            raise PublicError("invalid_message", "Invalid wire response")
        if self.future.done():
            raise WireError("closed")
        if "error" in frame:
            error = frame["error"]
            self.future.set_exception(PublicError(error["code"], error["message"], error.get("data", ABSENT)))
        else:
            self.future.set_result(frame["result"])

    def receive(self, path, receiver):
        raise WireError("receiver_exists")

    def close(self, code=1000, reason=""):
        if not self.future.done():
            self.future.set_exception(PublicError("disconnected", "Connection ended; outcome may be unknown"))


async def call_wire(
    wire,
    path,
    params=ABSENT,
    *,
    timeout_ms=None,
    context=None,
    meta=ABSENT,
    propagator=None,
    observer=None,
    family="",
):
    """Await one response; its logical id is scoped to this call's capability."""
    return await _call_wire(
        wire,
        path,
        params,
        timeout_ms=timeout_ms,
        context=context,
        meta=meta,
        propagator=propagator,
        observer=observer,
        family=family,
    )


async def _call_wire(
    wire,
    path,
    params=ABSENT,
    *,
    timeout_ms=None,
    context=None,
    meta=ABSENT,
    propagator=None,
    observer=None,
    family="",
    dispatch=None,
):
    cancelled = getattr(context, "cancelled", None)
    try:
        name = encode_path(path)
        timeout_ms = 30_000 if timeout_ms is None else timeout_ms
        if isinstance(timeout_ms, bool) or not isinstance(timeout_ms, int) or not 0 < timeout_ms <= 9007199254740991:
            raise PublicError("invalid_options", "timeout_ms must be a positive safe integer")
        if cancelled is not None and cancelled.is_set():
            raise PublicError("cancelled", "Call was cancelled before sending")
        trace = (propagator or DefaultPropagator()).inject(context)
        frame = _snapshot(
            _carriage(
                {
                    "version": 1,
                    "kind": "request",
                    "id": "c:1",
                    "params": {} if params is ABSENT else params,
                    **trace_of(trace or {}),
                },
                meta,
            )
        )
        frame = with_outgoing_trace(frame, trace)
    except Exception as error:
        raise UnpublishedError(error) from None
    future = asyncio.get_running_loop().create_future()
    future.add_done_callback(lambda done: None if done.cancelled() else done.exception())
    address = ReturnAddress(_Returning(future, dispatch))
    cleanup = set_wire_context(address, dispatch) if dispatch is not None else lambda: None
    finish = _observe_request(observer, family, name, False, trace)
    accepted = False
    watcher = None

    def withdraw(error, outcome):
        finish(error, outcome)
        if accepted:
            try:
                wire.send(path, Message({"version": 1, "kind": "cancel", "id": "c:1", **trace_of(frame)}, address))
            except Exception:
                pass

    try:
        try:
            wire.send(path, Message(frame, address))
            accepted = True
        except Exception as error:
            raise UnpublishedError(error) from None
        if cancelled is not None:

            async def watch():
                await cancelled.wait()
                if not future.done():
                    failure = PublicError("cancelled", "Call was cancelled; its outcome may be unknown")
                    future.set_exception(failure)
                    withdraw(failure, "cancelled")

            watcher = asyncio.create_task(watch())
        try:
            async with asyncio.timeout(timeout_ms / 1000):
                return await asyncio.shield(future)
        except TimeoutError:
            error = PublicError("request_timeout", "Call deadline exceeded; its outcome may be unknown")
            withdraw(error, "timeout")
            raise error from None
        except asyncio.CancelledError:
            withdraw(PublicError("cancelled", "Call was cancelled; its outcome may be unknown"), "cancelled")
            raise
    except Exception as error:
        finish(error)
        raise
    finally:
        if watcher is not None:
            watcher.cancel()
        cleanup()
        if not future.done():
            future.cancel()
        # finish is idempotent, including cancellation and a synchronous reply.
        if future.done() and not future.cancelled() and future.exception() is None:
            finish()


def emit_wire(wire, path, data=ABSENT, *, context=None, meta=ABSENT, propagator=None, observer=None, family=""):
    """Synchronous admission; application delivery belongs to the root."""
    try:
        name = encode_path(path)
        trace = (propagator or DefaultPropagator()).inject(context)
        frame = with_outgoing_trace(
            _snapshot(
                _carriage(
                    {
                        "version": 1,
                        "kind": "event",
                        "data": None if data is ABSENT else data,
                        **trace_of(trace or {}),
                    },
                    meta,
                )
            ),
            trace,
        )
        observe(
            observer,
            "event.emitted",
            name=name,
            bytes=len(encode_object({"v": frame["data"]}).encode("utf8")) - 6,
            trace=trace,
            family=family,
        )
        wire.send(path, Message(frame))
    except Exception as error:
        raise UnpublishedError(error) from None


def register_wire(wire, path, handlers):
    """Register facets together; roots await returned handler lifetimes."""
    if not isinstance(handlers, WireHandlers) or (handlers.request is None and handlers.event is None):
        raise ValueError("wire registration requires at least one handler")
    if any(handler is not None and not callable(handler) for handler in (handlers.request, handlers.event)):
        raise ValueError("wire handler must be callable")
    name = encode_path(path)
    incoming = {}

    def stop(*args):
        for _, controller in list(incoming.values()):
            controller.set()

    def receive(suffix, message):
        frame = message.frame
        if frame["kind"] == "event":
            if handlers.event is None:
                return None

            async def event():
                trace = trace_of(frame)
                dispatch = wire_event_context(message)
                context = clone_context(
                    dispatch.context if dispatch else None,
                    context_type=WireEventContext,
                    wire=wire,
                    meta=dict(frame["meta"]) if "meta" in frame else ABSENT,
                    raw=RawJSON(encode_object({"v": frame["data"]})[5:-1]),
                )
                if dispatch is None:
                    DefaultPropagator().extract(context, trace)
                observe(
                    handlers.observer,
                    "event.delivered",
                    name=name,
                    bytes=len(context.raw.text.encode("utf8")),
                    trace=trace,
                    family=handlers.family,
                )
                try:
                    result = handlers.event(frame["data"], context)
                    if inspect.isawaitable(result):
                        await result
                except Exception as error:
                    if not isinstance(error, PublicError) and dispatch and dispatch.panic:
                        try:
                            dispatch.panic(error)
                        except Exception:
                            pass
                    wire.close(1002, "wire event rejected")

            return event()
        address = message.return_address
        if frame["kind"] not in ("request", "cancel") or address is None:
            return None
        key = id(address), frame["id"]
        if frame["kind"] == "cancel":
            active = incoming.get(key)
            if active is not None and active[0] is address:
                active[1].set()
            return None
        trace = trace_of(frame)
        finish = _observe_request(handlers.observer, handlers.family, name, True, trace)
        if handlers.request is None or key in incoming:
            error = (
                PublicError("method_not_found", "An event has no request handler")
                if handlers.request is None
                else (PublicError("invalid_message", "Duplicate active request identifier"))
            )
            finish(response(message, error=error))
            return None
        controller = asyncio.Event()
        incoming[key] = address, controller
        dispatch = wire_context(address)
        source = dispatch.context if dispatch is not None else None
        cancelled, cleanup = compose_cancellation(controller, getattr(source, "cancelled", None))
        context = clone_context(
            source,
            wire=wire,
            cancelled=cancelled,
            request_id=getattr(source, "request_id", None) or frame["id"],
            meta=dict(frame["meta"]) if "meta" in frame else ABSENT,
            raw=RawJSON(encode_object({"v": frame["params"]})[5:-1]),
        )
        if dispatch is None:
            DefaultPropagator().extract(context, trace)

        async def run():
            cancelled_before = False
            try:
                if cancelled.is_set():
                    cancelled_before = True
                    raise PublicError("cancelled", "Request was cancelled")
                result = handlers.request(frame["params"], context)
                if inspect.isawaitable(result):
                    result = await result
                error = PublicError("cancelled", "Request was cancelled") if cancelled.is_set() else None
                outcome = response(message, result, error)
                finish(outcome, "cancelled" if error is not None and outcome is error else None)
            except asyncio.CancelledError:
                error = PublicError("cancelled", "Handler was cancelled")
                outcome = response(message, error=error)
                finish(outcome, "cancelled" if outcome is error else None)
                raise
            except Exception as error:
                if not isinstance(error, PublicError) and dispatch and dispatch.panic:
                    try:
                        dispatch.panic(error)
                    except Exception:
                        pass
                outcome = response(message, error=error)
                finish(outcome, "cancelled" if cancelled_before and outcome is error else "error")
            finally:
                cleanup()
                incoming.pop(key, None)

        return run()

    detach = wire.receive(path, Receiver(message=receive, closed=stop))
    detached = False

    def remove():
        nonlocal detached
        if not detached:
            detached = True
            detach()
            stop()

    return remove


def handle_wire(wire, path, handler):
    return register_wire(wire, path, WireHandlers(request=handler))


def on_wire_event(wire, path, listener):
    return register_wire(wire, path, WireHandlers(event=listener))


def forward_wire(left, right):
    """Forward raw capabilities unchanged; detach owns only registrations."""
    removals, detached = [], False

    def stop(*args):
        nonlocal detached
        if not detached:
            detached = True
            for remove in removals:
                remove()

    def receiver(destination):
        def deliver(path, message):
            try:
                destination.send(path, message)
            except Exception as error:
                stop()
                response(message, error=public_error(error))

        return Receiver(namespace=True, message=deliver, closed=stop)

    try:
        for source, destination in ((left, right), (right, left)):
            remove = source.receive([], receiver(destination))
            if detached:
                remove()
            else:
                removals.append(remove)
    except Exception:
        stop()
        raise
    return stop
