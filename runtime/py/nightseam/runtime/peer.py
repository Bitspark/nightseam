"""The bounded nightseam.duplex/1 peer over a frames connection."""

import asyncio
import inspect
import secrets
import time
from contextvars import ContextVar
from dataclasses import dataclass, field

from nightseam.duplex import CloseError, Frame

from .envelope import decode_envelope, require_name
from .json import RawJSON, encode_object, raw_members, scalar_value

PROFILE = "nightseam.duplex/1"


class _Absent:
    def __repr__(self):
        return "ABSENT"


ABSENT = _Absent()


class PublicError(Exception):
    """The only handler exception whose code, message and data cross the wire."""

    def __init__(self, code, message, data=ABSENT):
        require_name(code)
        require_name(message)
        self.code, self.message, self.data = code, message, data
        super().__init__(message)

    def envelope(self):
        result = {"code": self.code, "message": self.message}
        if self.data is not ABSENT:
            result["data"] = self.data
        return result


@dataclass
class RequestContext:
    peer: "Peer"
    request_id: str = ""
    cancelled: asyncio.Event = field(default_factory=asyncio.Event)
    trace: dict | None = None
    meta: object = ABSENT
    raw: RawJSON | None = None


_context = ContextVar("nightseam_request", default=None)


class DefaultPropagator:
    def extract(self, context, trace):
        if trace.get("traceparent"):
            context.trace = trace

    def inject(self, context):
        trace = context.trace if context else None
        if trace and trace.get("traceparent"):
            version, trace_id, _, flags = trace["traceparent"].split("-")
            result = {"traceparent": f"{version}-{trace_id}-{secrets.token_hex(8)}-{flags}"}
            if trace.get("tracestate"):
                result["tracestate"] = trace["tracestate"]
            return result
        return {"traceparent": f"00-{secrets.token_hex(16)}-{secrets.token_hex(8)}-01"}


@dataclass
class Options:
    max_concurrent_handlers: int = 64
    max_pending_requests: int = 128
    queue_capacity: int = 128
    max_frame_bytes: int = 1 << 20
    request_timeout_ms: int = 30_000
    write_timeout_ms: int = 10_000
    propagator: object = field(default_factory=DefaultPropagator)
    observer: object = None
    families: dict = field(default_factory=dict)
    prepare: object = None

    def __post_init__(self):
        if self.prepare is not None and not callable(self.prepare):
            raise PublicError("invalid_options", "prepare must be callable")
        for name in (
            "max_concurrent_handlers",
            "max_pending_requests",
            "queue_capacity",
            "max_frame_bytes",
            "request_timeout_ms",
            "write_timeout_ms",
        ):
            value = getattr(self, name)
            if isinstance(value, bool) or not isinstance(value, int) or not 0 < value <= 9007199254740991:
                raise PublicError("invalid_options", name + " must be a positive safe integer")


@dataclass
class _Pending:
    future: asyncio.Future
    method: str
    trace: dict
    started: float = field(default_factory=time.monotonic)


@dataclass
class _Incoming:
    method: str
    context: RequestContext
    trace: dict
    started: float = field(default_factory=time.monotonic)
    responded: bool = False
    timer: object = None


class Peer:
    """Own a connection until close; requests run concurrently, events serially.

    Handlers receive (value, context). Cancelling a call signals its remote
    context; it does not forcibly cancel application work. A returned ABSENT
    result is JSON null. Explicit RawJSON values preserve payload spelling.
    """

    def __init__(self, connection, role="client", options=None):
        if role not in ("client", "server"):
            raise PublicError("invalid_options", "role must be client or server")
        self.connection, self.role = connection, role
        self.options = options or Options()
        self.subprotocol = getattr(connection, "subprotocol", "")
        self._prefix = "c:" if role == "client" else "s:"
        self._next = 0
        self._received_serial = 0
        self._request_publication = asyncio.Lock()
        self._call_slots = 0
        self._pending, self._incoming, self._handlers = {}, {}, {}
        self._listeners = []
        self._outgoing = asyncio.Queue(self.options.queue_capacity)
        self._events = asyncio.Queue(self.options.queue_capacity)
        self._closed = asyncio.Event()
        self._finished = asyncio.Event()
        self._close_info = None
        self._tasks = set()
        self._wire = None
        self._observe("connection.opened", role=role)
        if self.options.prepare is not None:
            try:
                prepared = self.options.prepare(self)
                if inspect.isawaitable(prepared):
                    if inspect.iscoroutine(prepared):
                        prepared.close()
                    raise PublicError("invalid_options", "prepare must finish synchronously before reads start")
            except BaseException:
                self.connection.abort()
                self._closed.set()
                self._finished.set()
                if self._wire is not None:
                    self._wire._ended(1006, "preparation failed")
                raise
        self._spawn(self._writer())
        self._spawn(self._reader())
        self._spawn(self._deliver_events())

    @property
    def status(self):
        return "disconnected" if self._closed.is_set() else "connected"

    def wire(self):
        """Return the stable relative-path root over this peer's connection."""
        if self._wire is None:
            from .peer_wire import PeerWire

            self._wire = PeerWire(self)
        return self._wire

    def _spawn(self, coroutine):
        task = asyncio.create_task(coroutine)
        self._tasks.add(task)

        def finished(done):
            self._tasks.discard(done)
            if not done.cancelled():
                done.exception()

        task.add_done_callback(finished)
        return task

    def _observe(self, event_type, **members):
        observer = self.options.observer
        if observer is None:
            return
        event = {"type": event_type, "at": time.monotonic(), **members}
        try:
            (observer.observe if hasattr(observer, "observe") else observer)(event)
        except Exception:
            pass  # Observing cannot interrupt routing or cleanup.

    def _family(self, name):
        return self.options.families.get(name, "")

    def _pressure(self, queued, stalled):
        self._observe("backpressure", queued=queued, stalled=stalled, deadline=self.options.write_timeout_ms)

    def _ended(self, request_id, entry, incoming, outcome, error_code=""):
        self._observe(
            "request.ended",
            id=request_id,
            method=entry.method,
            incoming=incoming,
            duration=(time.monotonic() - entry.started) * 1000,
            outcome=outcome,
            error_code=error_code,
            trace=entry.trace,
            family=self._family(entry.method),
        )

    def handle(self, method, handler):
        require_name(method)
        if not callable(handler):
            raise ValueError("handler must be callable")
        self._handlers[method] = handler

        def remove():
            if self._handlers.get(method) is handler:
                del self._handlers[method]

        return remove

    def on_event(self, name, listener):
        """Register (data, context), or (name, data, context) when name is None."""
        if name is not None:
            require_name(name)
        if not callable(listener):
            raise ValueError("listener must be callable")
        entry = (name, listener)
        self._listeners.append(entry)

        def remove():
            if entry in self._listeners:
                self._listeners.remove(entry)

        return remove

    def _carriage(self, envelope, meta):
        if meta is not ABSENT:
            if not isinstance(meta, dict) or any(
                not isinstance(key, str) or not isinstance(value, str) for key, value in meta.items()
            ):
                raise PublicError("invalid_message", "meta must be an object of strings")
            scalar_value(meta)
            envelope["meta"] = {key: value for key, value in meta.items() if not key.startswith("nightseam.")}
        return envelope

    async def call(self, method, params=ABSENT, *, timeout_ms=None, context=None, meta=ABSENT):
        return await self._call(method, params, timeout_ms=timeout_ms, context=context, meta=meta)

    async def _call(
        self,
        method,
        params=ABSENT,
        *,
        timeout_ms=None,
        context=None,
        meta=ABSENT,
        trace_override=None,
        on_admitted=None,
        immediate=False,
    ):
        from .publication import unpublished

        accepted = False

        def admitted():
            nonlocal accepted
            accepted = True
            if on_admitted is not None:
                on_admitted()

        try:
            return await self._call_admitting(
                method,
                params,
                timeout_ms=timeout_ms,
                context=context,
                meta=meta,
                trace_override=trace_override,
                on_admitted=admitted,
                immediate=immediate,
            )
        except asyncio.CancelledError as error:
            if accepted:
                raise
            # asyncio.timeout on Python 3.11/3.12 requires the exact native
            # cancellation type. Preserve send-local proof as its cause.
            raise asyncio.CancelledError(*error.args) from unpublished(error)
        except Exception as error:
            if accepted:
                raise
            raise unpublished(error) from error

    async def _call_admitting(
        self,
        method,
        params=ABSENT,
        *,
        timeout_ms=None,
        context=None,
        meta=ABSENT,
        trace_override=None,
        on_admitted=None,
        immediate=False,
    ):
        require_name(method)
        timeout_ms = self.options.request_timeout_ms if timeout_ms is None else timeout_ms
        if isinstance(timeout_ms, bool) or not isinstance(timeout_ms, int) or timeout_ms <= 0:
            raise PublicError("invalid_options", "timeout_ms must be positive")
        if self._closed.is_set():
            raise PublicError("disconnected", "peer is disconnected")
        if self._call_slots >= self.options.max_pending_requests:
            raise PublicError("busy", "outstanding call limit reached")
        self._call_slots += 1
        accepted = False
        request_id = None

        def admitted():
            nonlocal accepted
            accepted = True
            if on_admitted is not None:
                on_admitted()

        try:
            async with asyncio.timeout(timeout_ms / 1000):
                if immediate and self._request_publication.locked():
                    await self._end(4011, "consumer stalled", True)
                    raise PublicError("busy", "request publication is occupied")
                # Reservation and queue admission are one ordered operation.
                # asyncio.Lock also prevents a new arrival overtaking waiters.
                async with self._request_publication:
                    if self._closed.is_set():
                        raise PublicError("disconnected", "peer is disconnected")
                    if self._next == 9007199254740991:
                        await self._end(4011, "request identifier exhausted", True)
                        raise PublicError("identifier_exhausted", "create a new peer before further calls")
                    self._next += 1
                    request_id = self._prefix + str(self._next)
                    trace = (
                        trace_override
                        if trace_override is not None
                        else self.options.propagator.inject(context or _context.get())
                    )
                    future = asyncio.get_running_loop().create_future()
                    # A shutdown may settle a caller which is still unwinding a send.
                    future.add_done_callback(lambda done: None if done.cancelled() else done.exception())
                    pending = _Pending(future, method, trace)
                    self._pending[request_id] = pending
                    self._observe(
                        "request.started",
                        id=request_id,
                        method=method,
                        incoming=False,
                        trace=trace,
                        family=self._family(method),
                    )
                    envelope = self._carriage(
                        {
                            "version": 1,
                            "kind": "request",
                            "id": request_id,
                            "method": method,
                            "params": {} if params is ABSENT else params,
                            **trace,
                        },
                        meta,
                    )
                    await self._send(envelope, method, accepted=admitted, immediate=immediate)
                return await asyncio.shield(future)
        except (TimeoutError, asyncio.CancelledError) as error:
            if self._pending.pop(request_id, None) is not None:
                timed_out = isinstance(error, TimeoutError)
                code = "request_timeout" if timed_out else "cancelled"
                self._ended(request_id, pending, False, "timeout" if timed_out else "cancelled", code)
                future.cancel()
                if accepted:
                    self._cancel_request(request_id, trace, method)
            if isinstance(error, TimeoutError):
                raise PublicError("request_timeout", "call deadline exceeded; its outcome may be unknown") from error
            raise
        except Exception as error:
            if self._pending.pop(request_id, None) is not None:
                self._ended(request_id, pending, False, "error", getattr(error, "code", "invalid_message"))
                future.cancel()
            raise
        finally:
            self._call_slots -= 1

    async def emit(self, event, data=ABSENT, *, context=None, meta=ABSENT):
        await self._emit(event, data, context=context, meta=meta)

    async def _emit(self, event, data=ABSENT, *, context=None, meta=ABSENT, trace_override=None, immediate=False):
        from .publication import unpublished

        accepted = False

        def admitted():
            nonlocal accepted
            accepted = True

        try:
            await self._emit_admitting(
                event,
                data,
                context=context,
                meta=meta,
                trace_override=trace_override,
                on_admitted=admitted,
                immediate=immediate,
            )
        except asyncio.CancelledError as error:
            if accepted:
                raise
            raise asyncio.CancelledError(*error.args) from unpublished(error)
        except Exception as error:
            if accepted:
                raise
            raise unpublished(error) from error

    async def _emit_admitting(
        self,
        event,
        data=ABSENT,
        *,
        context=None,
        meta=ABSENT,
        trace_override=None,
        on_admitted=None,
        immediate=False,
    ):
        require_name(event)
        trace = (
            trace_override if trace_override is not None else self.options.propagator.inject(context or _context.get())
        )
        await self._send(
            self._carriage(
                {"version": 1, "kind": "event", "event": event, "data": None if data is ABSENT else data, **trace}, meta
            ),
            event,
            accepted=on_admitted,
            immediate=immediate,
        )

    def _cancel_request(self, request_id, trace, method):
        # Cancellation cannot extend an expired deadline, consume an extra
        # producer, or terminate a healthy carrier merely because it is full.
        if self._closed.is_set() or self._outgoing.full():
            return
        envelope = {"version": 1, "kind": "cancel", "id": request_id, **trace}
        text = encode_object(envelope)
        size = len(text.encode("utf8"))
        if size <= self.options.max_frame_bytes:
            self._outgoing.put_nowait((envelope, text, size, method, time.monotonic()))

    async def _put(self, queue, item, accepted=None, immediate=False):
        if self._closed.is_set():
            raise PublicError("disconnected", "peer is disconnected")
        if not queue.full():
            queue.put_nowait(item)
            if accepted:
                accepted()
            return
        if immediate:
            self._pressure(queue.qsize(), True)
            await self._end(4011, "consumer stalled", True)
            raise PublicError("busy", "consumer did not accept the frame")
        self._pressure(queue.qsize(), False)
        put = asyncio.create_task(queue.put(item))
        closed = asyncio.create_task(self._closed.wait())
        try:
            async with asyncio.timeout(self.options.write_timeout_ms / 1000):
                await asyncio.wait((put, closed), return_when=asyncio.FIRST_COMPLETED)
                if closed.done():
                    raise PublicError("disconnected", "peer is disconnected")
        except TimeoutError:
            self._pressure(queue.qsize(), True)
            await self._end(4011, "consumer stalled", True)
            raise PublicError("busy", "consumer did not drain before the write deadline")
        finally:
            for task in (put, closed):
                if not task.done():
                    task.cancel()
            await asyncio.gather(put, closed, return_exceptions=True)
            # A put can win the same turn as cancellation. Preserve that
            # admission fact before the caller decides whether to cancel it.
            if accepted and not put.cancelled() and put.exception() is None:
                accepted()

    async def _send(self, envelope, name="", *, accepted=None, immediate=False):
        try:
            text = encode_object(envelope)
            size = len(text.encode("utf8"))
        except (ValueError, TypeError, OverflowError) as error:
            raise PublicError("invalid_message", "frame must contain serializable JSON values") from error
        if size > self.options.max_frame_bytes:
            raise PublicError("frame_too_large", "outgoing frame exceeds the size limit")
        await self._put(self._outgoing, (envelope, text, size, name, time.monotonic()), accepted, immediate)
        if envelope["kind"] == "event":
            self._observe(
                "event.emitted", name=name, bytes=size, trace=self._trace(envelope), family=self._family(name)
            )

    @staticmethod
    def _trace(envelope):
        return {key: envelope[key] for key in ("traceparent", "tracestate") if envelope.get(key)}

    async def _writer(self):
        try:
            while True:
                envelope, text, size, name, queued = await self._outgoing.get()
                deadline = queued + self.options.write_timeout_ms / 1000
                self._observe(
                    "frame.sent",
                    kind=envelope["kind"],
                    name=name,
                    bytes=size,
                    id=envelope.get("id", ""),
                    trace=self._trace(envelope),
                    family=self._family(name),
                )
                try:
                    async with asyncio.timeout_at(deadline):
                        await self.connection.send(Frame("text", text))
                except TimeoutError:
                    self._pressure(self._outgoing.qsize(), True)
                    await self._end(4011, "output consumer stalled", True)
                    return
        except asyncio.CancelledError:
            pass
        except CloseError as error:
            await self._end(error.code, error.reason, False)
        except Exception:
            await self._end(4011, "send failed", True)

    async def _reader(self):
        try:
            while True:
                incoming = await self.connection.receive()
                if incoming.kind != "text" or incoming.size > self.options.max_frame_bytes:
                    await self._end(4011, "invalid profile frame", True)
                    return
                try:
                    envelope = decode_envelope(incoming.data, self.role)
                    raw = dict(raw_members(incoming.data))
                except ValueError:
                    await self._end(4011, "invalid profile frame", True)
                    return
                kind = envelope["kind"]
                name = envelope.get("method", envelope.get("event", ""))
                trace = self._trace(envelope)
                self._observe(
                    "frame.received",
                    kind=kind,
                    name=name,
                    bytes=incoming.size,
                    id=envelope.get("id", ""),
                    trace=trace,
                    family=self._family(name),
                )
                if kind == "response":
                    pending = self._pending.pop(envelope["id"], None)
                    if pending:
                        if "error" in envelope:
                            public = envelope["error"]
                            error = PublicError(public["code"], public["message"], public.get("data", ABSENT))
                            self._ended(envelope["id"], pending, False, "error", error.code)
                            pending.future.set_exception(error)
                        else:
                            self._ended(envelope["id"], pending, False, "ok")
                            pending.future.set_result(envelope["result"])
                elif kind == "cancel":
                    entry = self._incoming.get(envelope["id"])
                    if entry:
                        entry.context.cancelled.set()
                elif kind == "request":
                    await self._request(envelope, raw["params"])
                else:
                    await self._put(self._events, (envelope, raw["data"], incoming.size))
        except asyncio.CancelledError:
            pass
        except CloseError as error:
            await self._end(error.code, error.reason, False)
        except Exception:
            await self._end(4011, "receive failed", True)

    async def _request(self, envelope, raw):
        request_id, method = envelope["id"], envelope["method"]
        trace = self._trace(envelope)
        serial = int(request_id[2:])
        if serial <= self._received_serial:
            await self._end(4011, "incoming request serial did not increase", True)
            return
        self._received_serial = serial
        if len(self._incoming) >= self.options.max_concurrent_handlers:
            await self._send(
                {
                    "version": 1,
                    "kind": "response",
                    "id": request_id,
                    "error": PublicError("busy", "incoming request limit reached").envelope(),
                    **trace,
                },
                method,
                immediate=True,
            )
            return
        context = RequestContext(self, request_id, meta=envelope.get("meta", ABSENT), raw=raw)
        context._received_trace = dict(trace)
        self.options.propagator.extract(context, dict(trace))
        entry = _Incoming(method, context, trace)
        self._incoming[request_id] = entry
        self._observe(
            "request.started", id=request_id, method=method, incoming=True, trace=trace, family=self._family(method)
        )

        def expire():
            context.cancelled.set()
            self._spawn(
                self._respond(
                    request_id, entry, error=PublicError("cancelled", "request deadline exceeded"), outcome="timeout"
                )
            )

        entry.timer = asyncio.get_running_loop().call_later(self.options.request_timeout_ms / 1000, expire)
        self._spawn(self._run_handler(request_id, entry, envelope["params"]))

    async def _run_handler(self, request_id, entry, params):
        token = _context.set(entry.context)
        try:
            if entry.context.cancelled.is_set():
                await self._respond(request_id, entry, outcome="cancelled")
                return
            handler = self._handlers.get(entry.method)
            if handler is None and self._wire is not None:
                handler = self._wire._handler(entry.method)
            if handler is None:
                raise PublicError("method_not_found", "unknown method " + entry.method)
            result = handler(params, entry.context)
            if inspect.isawaitable(result):
                result = await result
            await self._respond(request_id, entry, result)
        except asyncio.CancelledError:
            if not self._closed.is_set():
                await self._respond(
                    request_id, entry, error=PublicError("cancelled", "handler cancelled"), outcome="cancelled"
                )
        except PublicError as error:
            await self._respond(request_id, entry, error=error)
        except Exception as error:
            self._observe(
                "handler.panic",
                method=entry.method,
                value=str(error),
                trace=entry.trace,
                family=self._family(entry.method),
            )
            await self._respond(request_id, entry, error=PublicError("internal", "request handler failed"))
        finally:
            entry.timer.cancel()
            if self._incoming.get(request_id) is entry:
                del self._incoming[request_id]
            _context.reset(token)

    async def _respond(self, request_id, entry, result=ABSENT, error=None, outcome=None):
        if entry.responded or self._incoming.get(request_id) is not entry or self._closed.is_set():
            return
        if error is None and entry.context.cancelled.is_set():
            error = PublicError("cancelled", "request was cancelled")
            outcome = outcome or "cancelled"
        outcome = outcome or ("error" if error else "ok")
        entry.responded = True
        entry.timer.cancel()
        envelope = {"version": 1, "kind": "response", "id": request_id, **entry.trace}
        if error:
            envelope["error"] = error.envelope()
        else:
            envelope["result"] = None if result is ABSENT else result
        self._ended(
            request_id, entry, True, outcome, "request_timeout" if outcome == "timeout" else error.code if error else ""
        )
        try:
            await self._send(envelope)
        except PublicError as failure:
            if failure.code in ("invalid_message", "frame_too_large"):
                try:
                    await self._send(
                        {
                            "version": 1,
                            "kind": "response",
                            "id": request_id,
                            "error": PublicError("internal", "response could not be encoded").envelope(),
                            **entry.trace,
                        }
                    )
                    return
                except PublicError:
                    pass
            await self._end(4011, "response could not be sent", True)

    async def _deliver_events(self):
        try:
            while True:
                envelope, raw, size = await self._events.get()
                name, trace = envelope["event"], self._trace(envelope)
                context = RequestContext(self, meta=envelope.get("meta", ABSENT), raw=raw)
                context._received_trace = dict(trace)
                self.options.propagator.extract(context, dict(trace))
                self._observe("event.delivered", name=name, bytes=size, trace=trace, family=self._family(name))
                token = _context.set(context)
                try:
                    async with asyncio.timeout(self.options.write_timeout_ms / 1000):
                        for wanted, listener in list(self._listeners):
                            if wanted is not None and wanted != name:
                                continue
                            try:
                                result = (
                                    listener(name, envelope["data"], context)
                                    if wanted is None
                                    else listener(envelope["data"], context)
                                )
                                if inspect.isawaitable(result):
                                    await result
                            except Exception:
                                pass  # A listener's failure cannot interrupt later events.
                        if self._wire is not None:
                            await self._wire._event(name, envelope["data"], context)
                finally:
                    _context.reset(token)
        except TimeoutError:
            self._pressure(self._events.qsize(), True)
            await self._end(4011, "event handler stalled", True)
        except asyncio.CancelledError:
            pass

    async def _end(self, code, reason, local):
        if self._closed.is_set():
            return
        self._close_info = {"code": code, "reason": reason, "clean": code == 1000}
        self._closed.set()
        if self._wire is not None:
            self._wire._ended(code, reason)
        for request_id, entry in list(self._pending.items()):
            self._ended(request_id, entry, False, "error", "disconnected")
            entry.future.set_exception(PublicError("disconnected", "connection ended"))
        self._pending.clear()
        for request_id, entry in self._incoming.items():
            entry.context.cancelled.set()
            entry.timer.cancel()
            if not entry.responded:
                self._ended(request_id, entry, True, "error", "disconnected")
        self._incoming.clear()
        self._observe("connection.closed", code=code, reason=reason, local=local)
        current = asyncio.current_task()
        tasks = [task for task in self._tasks if task is not current]
        for task in tasks:
            task.cancel()
        try:
            if local:
                try:
                    async with asyncio.timeout(self.options.write_timeout_ms / 1000):
                        await self.connection.close(code, reason)
                except (Exception, asyncio.CancelledError):
                    self.connection.abort()
            # Application handlers can suppress task cancellation. Their work
            # must not hold the transport open or make close unbounded.
            if tasks:
                await asyncio.wait(tasks, timeout=self.options.write_timeout_ms / 1000)
        finally:
            self._finished.set()

    async def close(self):
        await self._end(1000, "", True)
        await self._finished.wait()

    async def wait_closed(self):
        await self._finished.wait()
        return dict(self._close_info)
