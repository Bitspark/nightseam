"""Bounded local Wire endpoints sharing the runtime's return capabilities."""

import asyncio
import inspect
from collections import deque
from dataclasses import dataclass, field, replace

from nightseam.duplex import Message, ReturnAddress, WireError, encode_path

from .peer import ABSENT, Options, PublicError
from .publication import without_unpublished_proof
from .wire import (
    WireDispatchContext,
    WireEventContext,
    WireRequestContext,
    clone_context,
    compose_cancellation,
    observe,
    profile_frame,
    public_error,
    response,
    set_wire_context,
    trace_of,
    wire_context,
    wire_event_context,
    with_wire_event_context,
)


def _disconnected():
    return PublicError("disconnected", "Connection ended; outcome may be unknown.")


@dataclass(eq=False)
class _Registration:
    path: list
    receiver: object


@dataclass(eq=False)
class _Call:
    original: Message
    path: list
    source: object
    returning: object = None
    registration: object = None
    cancelled: asyncio.Event = field(default_factory=asyncio.Event)
    cleanup: object = lambda: None
    timer: object = None
    completed: bool = False
    responded: bool = False
    active: bool = False
    invoking: bool = False
    awaiting_handler: bool = False
    cancel_queued: bool = False
    cancel_delivered: bool = False

    @property
    def key(self):
        # A capability is its identity, even if another address wraps the same Wire.
        return id(self.original.return_address), self.original.frame["id"]


@dataclass
class _Delivery:
    path: list
    message: Message
    call: _Call | None = None
    refusal: Exception | None = None


class _Pair:
    def __init__(self, options):
        self.options = replace(options, families=dict(options.families))
        self.loop = asyncio.get_running_loop()
        self.closed = False
        self.tasks = set()
        self.ends = [_Endpoint(self), _Endpoint(self)]
        self.ends[0].other, self.ends[1].other = self.ends[1], self.ends[0]

    def observe(self, event_type, **members):
        observe(self.options.observer, event_type, **members)

    def pressure(self, queued):
        self.observe("backpressure", queued=queued, stalled=True, deadline=self.options.write_timeout_ms)

    def spawn(self, coroutine):
        task = self.loop.create_task(coroutine)
        self.tasks.add(task)

        def done(task):
            self.tasks.discard(task)
            if not task.cancelled():
                task.exception()

        task.add_done_callback(done)

    def fail(self, error):
        self.end(4011, error.message)

    def end(self, code=1000, reason=""):
        if self.closed:
            return
        self.closed = True
        receivers, requests = [], []
        for endpoint in self.ends:
            if endpoint.event_timer:
                endpoint.event_timer.cancel()
            receivers.extend(registration.receiver for registration in endpoint.registrations.values())
            for call in endpoint.calls.values():
                if call.timer:
                    call.timer.cancel()
                call.cancelled.set()
                call.cleanup()
                if not call.responded:
                    requests.append(call.original)
                call.responded = call.completed = True
            endpoint.registrations.clear()
            endpoint.calls.clear()
            endpoint.queue.clear()
            endpoint.queued = 0
        self.observe("connection.closed", code=code, reason=reason, local=True)

        def notify():
            for receiver in receivers:
                if receiver.closed:
                    try:
                        receiver.closed(code, reason)
                    except Exception:
                        pass  # Every receiver and return capability gets closure.
            for request in requests:
                response(request, error=_disconnected())

        self.loop.call_soon(notify)


class _Return:
    def __init__(self, endpoint, call):
        self.endpoint, self.call = endpoint, call

    def send(self, path, message):
        call = self.call
        if path or message.frame.get("kind") != "response" or message.frame.get("id") != call.original.frame["id"]:
            raise PublicError("invalid_message", "Invalid wire response.")
        # Refused encoding does not consume the return; response() may retry its
        # bounded internal-error fallback before any response is admitted.
        checked = profile_frame(message.frame, "", self.endpoint.pair.options.max_frame_bytes)
        try:
            if call.responded or call.completed:
                raise _disconnected()
            call.responded = True
            try:
                call.original.return_address.wire.send([], Message(checked))
            except Exception as error:
                raise without_unpublished_proof(error) from None
        finally:
            self.endpoint.complete(call)

    def receive(self, path, receiver):
        raise WireError("receiver_exists")

    def close(self, code=1000, reason=""):
        self.endpoint.complete(self.call)


class _Endpoint:
    def __init__(self, pair):
        self.pair = pair
        self.other = None
        self.queue = deque()
        self.queued = 0
        self.active = 0
        self.calls = {}
        self.registrations = {}
        self.event_timer = None
        self.draining = False

    def send(self, path, message):
        if self.pair.closed:
            raise _disconnected()
        path = list(path)
        frame = profile_frame(message.frame, encode_path(path), self.pair.options.max_frame_bytes)
        kind = frame["kind"]
        if kind not in ("request", "event", "cancel"):
            raise PublicError("invalid_message", "A response is sent to its request's return address.")
        if kind != "event" and (message.return_address is None or message.return_address.wire is None):
            raise PublicError("invalid_message", "A wire request or cancellation requires a return address.")
        snapshot = Message(frame, message.return_address)
        event_context = wire_event_context(message)
        if kind == "event" and event_context is not None:
            snapshot = with_wire_event_context(snapshot, event_context.context, event_context.panic)
        self.other.admit(path, snapshot)

    def admit(self, path, message):
        frame = message.frame
        kind = frame["kind"]
        call = refusal = None
        if kind == "cancel":
            call = self.calls.get((id(message.return_address), frame["id"]))
            if call is None or call.completed or call.cancel_queued or call.cancel_delivered:
                return
            call.cancel_queued = True
            # Cancellation belongs to the admitted route, even after detach or
            # a different path on a subsequent control from the same origin.
            path = call.path
        else:
            if self.queued >= self.pair.options.queue_capacity:
                error = PublicError("busy", "Local wire queue limit reached.")
                self.pair.pressure(self.queued)
                self.pair.fail(error)
                raise error
            self.queued += 1
            if kind == "request":
                key = id(message.return_address), frame["id"]
                if key in self.calls:
                    refusal = PublicError("invalid_message", "Duplicate active request identifier.")
                elif len(self.calls) >= self.pair.options.max_pending_requests:
                    refusal = PublicError("busy", "Outstanding call limit reached.")
                else:
                    call = _Call(message, path, wire_context(message.return_address))
                    call.returning = ReturnAddress(_Return(self, call))
                    self.calls[key] = call
        if call is not None:
            message = Message(frame, call.returning)
        self.queue.append(_Delivery(path, message, call, refusal))
        self.schedule()

    def receive(self, path, receiver):
        if self.pair.closed:
            raise _disconnected()
        path = list(path)
        key = encode_path(path), receiver.namespace
        if not callable(receiver.message):
            raise PublicError("invalid_message", "A wire receiver requires a callback.")
        if key in self.registrations:
            raise WireError("receiver_exists")
        registration = _Registration(path, receiver)
        self.registrations[key] = registration

        def detach():
            if self.registrations.get(key) is registration:
                del self.registrations[key]

        return detach

    def close(self, code=1000, reason=""):
        self.pair.end(code, reason)

    def match(self, path):
        exact = self.registrations.get((encode_path(path), False))
        if exact:
            return exact
        best = None
        for (_, namespace), registration in self.registrations.items():
            if namespace and path[: len(registration.path)] == registration.path:
                if best is None or len(best.path) < len(registration.path):
                    best = registration
        return best

    def retire(self, call):
        if call.completed and not call.cancel_queued and self.calls.get(call.key) is call:
            del self.calls[call.key]
            call.cleanup()

    def release_active(self, call):
        if call.active:
            call.active = False
            self.active -= 1

    def complete(self, call):
        if not call.completed:
            call.completed = True
            if call.timer:
                call.timer.cancel()
            call.cancelled.set()
            if not call.invoking and not call.awaiting_handler:
                self.release_active(call)
        self.retire(call)

    def timeout(self, call):
        if self.pair.closed or call.completed:
            return
        call.cancelled.set()
        if not call.cancel_queued and not call.cancel_delivered:
            call.cancel_queued = True
            self.queue.append(
                _Delivery(
                    call.path,
                    Message({"version": 1, "kind": "cancel", "id": call.original.frame["id"]}, call.returning),
                    call,
                )
            )
            self.schedule()
        # Answer once; neither a deadline nor an early response releases work
        # which is still running in an asynchronous application handler.
        if not call.responded:
            call.responded = True
            response(call.original, error=PublicError("cancelled", "Request deadline exceeded."))

    def schedule(self):
        if not self.draining and not self.pair.closed:
            self.draining = True
            # create_task may execute immediately under an eager factory.
            # Admission must never run a receiver on the sender's stack.
            self.pair.loop.call_soon(self.start)

    def start(self):
        if self.pair.closed:
            self.draining = False
            return
        self.pair.spawn(self.drain())

    def panic(self, path, frame, error):
        self.pair.observe(
            "handler.panic",
            method=encode_path(path),
            value=str(error),
            trace=trace_of(frame),
            family=self.pair.options.families.get(encode_path(path), ""),
        )

    def request_context(self, call, message):
        source = call.source.context if call.source else None
        cancelled, cancel_cleanup = compose_cancellation(call.cancelled, *([source.cancelled] if source else []))
        context = clone_context(
            source,
            context_type=WireRequestContext,
            wire=self,
            cancelled=cancelled,
            request_id=source.request_id if source else message.frame["id"],
            meta=dict(message.frame["meta"]) if "meta" in message.frame else ABSENT,
        )
        if source is None:
            self.pair.options.propagator.extract(context, trace_of(message.frame))
        panic = (
            call.source.panic
            if call.source and call.source.panic
            else lambda error: self.panic(call.path, message.frame, error)
        )
        context_cleanup = set_wire_context(
            call.returning, WireDispatchContext(context, panic, self.pair.options.max_frame_bytes)
        )

        def cleanup():
            context_cleanup()
            cancel_cleanup()

        call.cleanup = cleanup

    async def finish_handler(self, pending, call, message):
        try:
            await pending
        except asyncio.CancelledError:
            if not self.pair.closed:
                response(message, error=PublicError("cancelled", "Request cancelled."))
        except Exception as error:
            response(message, error=public_error(error))
        finally:
            call.awaiting_handler = False
            self.release_active(call)

    async def finish_cancel(self, pending):
        try:
            await pending
        except asyncio.CancelledError:
            pass
        except Exception as error:
            self.pair.fail(public_error(error))

    def deliver_cancel(self, delivery):
        call = delivery.call
        call.cancel_queued = False
        call.cancel_delivered = True
        call.cancelled.set()
        self.retire(call)
        if not call.completed and call.registration:
            try:
                pending = call.registration.receiver.message(call.path, delivery.message)
                if inspect.isawaitable(pending):
                    self.pair.spawn(self.finish_cancel(pending))
            except Exception as error:
                self.pair.fail(public_error(error))

    async def deliver_event(self, registration, path, message):
        if wire_event_context(message) is None:
            context = WireEventContext(wire=self)
            self.pair.options.propagator.extract(context, trace_of(message.frame))
            message = with_wire_event_context(message, context, lambda error: self.panic(path, message.frame, error))

        def stalled():
            if not self.pair.closed:
                self.pair.pressure(self.queued)
                self.pair.fail(PublicError("stalled_consumer", "Local wire event handler deadline exceeded."))

        self.event_timer = self.pair.loop.call_later(self.pair.options.write_timeout_ms / 1000, stalled)
        try:
            pending = registration.receiver.message(path, message)
            if inspect.isawaitable(pending):
                await pending
        finally:
            self.event_timer.cancel()
            self.event_timer = None

    def deliver_request(self, registration, delivery):
        call, message = delivery.call, delivery.message
        if registration is None or self.active >= self.pair.options.max_concurrent_handlers:
            response(
                message,
                error=PublicError("busy", "Incoming request limit reached.")
                if registration
                else PublicError("method_not_found", "Unknown method."),
            )
            return
        call.registration = registration
        self.active += 1
        call.active = True
        self.request_context(call, message)
        call.timer = self.pair.loop.call_later(self.pair.options.request_timeout_ms / 1000, self.timeout, call)
        call.invoking = True
        try:
            pending = registration.receiver.message(delivery.path, message)
            if inspect.isawaitable(pending):
                call.awaiting_handler = True
                self.pair.spawn(self.finish_handler(pending, call, message))
        except Exception as error:
            response(message, error=public_error(error))
        finally:
            call.invoking = False
            if call.completed and not call.awaiting_handler:
                self.release_active(call)

    async def drain(self):
        try:
            while self.queue and not self.pair.closed:
                delivery = self.queue.popleft()
                kind = delivery.message.frame["kind"]
                if kind == "cancel":
                    self.deliver_cancel(delivery)
                    continue
                self.queued -= 1
                if delivery.refusal:
                    response(delivery.message, error=delivery.refusal)
                    continue
                registration = self.match(delivery.path)
                if kind == "event":
                    if registration:
                        await self.deliver_event(registration, delivery.path, delivery.message)
                else:
                    self.deliver_request(registration, delivery)
                    if delivery.call.awaiting_handler:
                        # Starting a Python coroutine is deferred until its
                        # task runs. Preserve request/event admission order,
                        # while letting the handler await a reverse call.
                        await asyncio.sleep(0)
        except Exception as error:
            self.pair.fail(public_error(error))
        finally:
            self.draining = False
            if self.queue and not self.pair.closed:
                self.schedule()


def wire_pair(options=None):
    """Return two local origins with synchronous admission and deferred dispatch.

    The current asyncio loop owns both endpoints; no Peer or transport is
    constructed. Closing either endpoint ends this pair alone.
    """
    options = options or Options()
    if getattr(options, "prepare", None) is not None:
        raise PublicError("invalid_options", "Local wires install receivers through receive.")
    pair = _Pair(options)
    return tuple(pair.ends)
