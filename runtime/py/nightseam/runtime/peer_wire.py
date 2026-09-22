"""One bounded relative-path root over an existing physical peer."""

import asyncio
import inspect
from collections import deque
from dataclasses import dataclass

from bitwire import Message, ReturnAddress
from nightseam.duplex import WireError, decode_path, encode_path

from .peer import ABSENT, PublicError
from .wire import (
    WireCompletion,
    WireDispatchContext,
    outgoing_trace,
    profile_frame,
    public_error,
    response,
    set_wire_context,
    with_wire_event_context,
)


@dataclass
class _Registration:
    path: tuple
    receiver: object


@dataclass
class _Call:
    original: Message
    task: object = None
    completed: bool = False
    responded: bool = False
    cancel_queued: bool = False
    cancelled: bool = False


class PeerWire:
    def __init__(self, peer):
        self.peer = peer
        self.options = peer.options
        self._queue = deque()
        self._data_queued = 0
        self._calls = {}
        self._attachment = None
        self._scheduled = False
        self._ending = False
        self._closed = False

    @staticmethod
    def _disconnected():
        return PublicError("disconnected", "connection ended; outcome may be unknown")

    @staticmethod
    def _key(message):
        return id(message.return_address), message.frame["id"]

    def _open(self):
        if self._ending or self._closed or self.peer.status != "connected":
            raise self._disconnected()

    def _schedule(self):
        if not self._scheduled and not self._ending:
            self._scheduled = True
            # Even an eager task factory must not dispatch on a sender's stack.
            asyncio.get_running_loop().call_soon(self._start)

    def _start(self):
        if self._ending:
            self._scheduled = False
            return
        self.peer._spawn(self._drain())

    def send(self, path, message):
        self._open()
        name = encode_path(path)
        frame = profile_frame(message.frame, name, self.options.max_frame_bytes)
        kind = frame["kind"]
        if kind not in ("request", "event", "cancel"):
            raise PublicError("invalid_message", "send a response to its request's return address")
        if kind in ("request", "event") and not name:
            raise PublicError("invalid_message", "a physical Wire operation requires a nonempty path")
        if kind != "event" and (
            message.return_address is None or not callable(getattr(message.return_address.wire, "send", None))
        ):
            raise PublicError("invalid_message", "a request or cancellation requires a return address")
        checked = Message(frame, message.return_address)
        call = None
        refusal = None
        if kind == "cancel":
            call = self._calls.get(self._key(checked))
            if call is None or call.completed or call.cancel_queued or call.cancelled:
                return
            call.cancel_queued = True
        else:
            if self._data_queued >= self.options.queue_capacity:
                error = PublicError("busy", "output consumer is stalled; Wire queue limit reached")
                self.peer._pressure(self._data_queued, True)
                self._fail(error)
                raise error
            self._data_queued += 1
            if kind == "request":
                key = self._key(checked)
                if key in self._calls:
                    refusal = PublicError("invalid_message", "duplicate active request identifier")
                elif len(self._calls) >= self.options.max_pending_requests:
                    refusal = PublicError("busy", "outstanding Wire call limit reached")
                else:
                    call = _Call(checked)
                    self._calls[key] = call
        self._queue.append((tuple(path), checked, call, refusal))
        self._schedule()

    def _retire(self, call):
        if call.completed and not call.cancel_queued:
            key = self._key(call.original)
            if self._calls.get(key) is call:
                del self._calls[key]

    def _finished_call(self, call, task):
        call.completed = True
        self._retire(call)
        if task.cancelled():
            error, value = PublicError("cancelled", "request was cancelled"), ABSENT
        else:
            error = task.exception()
            value = ABSENT if error else task.result()
        if not call.responded:
            call.responded = True
            response(call.original, value, public_error(error) if error else None)

    async def _drain(self):
        try:
            while self._queue and not self._ending:
                path, message, call, refusal = self._queue.popleft()
                frame = message.frame
                kind = frame["kind"]
                if kind == "cancel":
                    call.cancel_queued = False
                    call.cancelled = True
                    if not call.completed and call.task is not None:
                        call.task.cancel()
                        # Cancellation admission precedes a later data frame.
                        # This waits only for peer withdrawal, never application work.
                        await asyncio.gather(call.task, return_exceptions=True)
                    self._retire(call)
                    continue
                self._data_queued -= 1
                if refusal is not None:
                    response(message, error=refusal)
                    continue
                name = encode_path(path)
                trace = outgoing_trace(frame)
                if kind == "event":
                    await self.peer._emit(
                        name, frame["data"], meta=frame.get("meta", ABSENT), trace_override=trace, immediate=True
                    )
                    continue
                admitted = asyncio.get_running_loop().create_future()

                def accepted(future=admitted):
                    if not future.done():
                        future.set_result(None)

                call.task = self.peer._spawn(
                    self.peer._call(
                        name,
                        frame["params"],
                        meta=frame.get("meta", ABSENT),
                        trace_override=trace,
                        on_admitted=accepted,
                        immediate=True,
                    )
                )
                call.task.add_done_callback(lambda task, entry=call: self._finished_call(entry, task))
                # A Python coroutine has not admitted anything merely because
                # its Task exists. Fence admission so events/cancels cannot pass it.
                await asyncio.wait((admitted, call.task), return_when=asyncio.FIRST_COMPLETED)
                if not admitted.done():
                    admitted.cancel()
        except asyncio.CancelledError:
            pass
        except Exception as error:
            self._fail(public_error(error))
        finally:
            self._scheduled = False
            if self._queue and not self._ending:
                self._schedule()

    def receive(self, receiver):
        self._open()
        if self._attachment is not None:
            raise WireError("receiver_exists")
        registration = _Registration((), receiver)
        self._attachment = registration

        def detach():
            if self._attachment is registration:
                self._attachment = None

        return detach

    def _lookup(self, name):
        try:
            path = tuple(decode_path(name))
        except WireError:
            return None
        return (path, self._attachment) if self._attachment is not None and self._attachment.receiver.message else None

    def _panic(self, name, error, trace):
        self.peer._observe("handler.panic", method=name, value=str(error), trace=trace, family=self.peer._family(name))

    def _handler(self, name):
        found = self._lookup(name)
        if found is None:
            return None
        path, registration = found

        async def dispatch(params, context):
            result = asyncio.get_running_loop().create_future()
            result.add_done_callback(lambda future: None if future.cancelled() else future.exception())
            completion = WireCompletion()
            answer = None
            bridge = self

            class Returning:
                def send(self, suffix, message):
                    nonlocal answer
                    frame = profile_frame(message.frame, "", bridge.options.max_frame_bytes)
                    if suffix or frame["kind"] != "response" or frame["id"] != "c:1":
                        raise PublicError("invalid_message", "invalid Wire response")
                    if result.done():
                        raise bridge._disconnected()
                    value, error = ABSENT, None
                    if "error" in frame:
                        raw = frame["error"]
                        error = PublicError(raw["code"], raw["message"], raw.get("data", ABSENT))
                        result.set_exception(error)
                    else:
                        value = frame["result"]
                        result.set_result(value)
                    # A response may precede actual application completion.
                    # Publish it now while _run_handler retains the work slot.
                    completion.settle()
                    outcome = (
                        "cancelled"
                        if error is not None
                        and error.code == "cancelled"
                        and context.cancelled.is_set()
                        and completion.cause == "cancelled"
                        else None
                    )
                    answer = bridge._answer(context, value, error, outcome)

                def close(self, code=1000, reason=""):
                    if not result.done():
                        result.set_exception(bridge._disconnected())

            address = ReturnAddress(Returning())
            trace = context._received_trace
            cleanup = set_wire_context(
                address,
                WireDispatchContext(
                    context, lambda error: self._panic(name, error, trace), self.options.max_frame_bytes, completion
                ),
            )
            frame = {"version": 1, "kind": "request", "id": "c:1", "params": params, **trace}
            if context.meta is not ABSENT:
                frame["meta"] = dict(context.meta)
            request = Message(frame, address)

            async def cancellation():
                await context.cancelled.wait()
                control = Message({"version": 1, "kind": "cancel", "id": "c:1", **trace}, address)
                try:
                    pending = registration.receiver.message(path, control)
                    if inspect.isawaitable(pending):
                        await pending
                except Exception:
                    pass
                # Withdrawal signals the body, but its eventual public error
                # still decides the incoming response. Only a receiver deadline
                # may answer before the application has completed.

            watcher = asyncio.create_task(cancellation())
            try:
                pending = registration.receiver.message(path, request)
                if inspect.isawaitable(pending):
                    # Keep the physical handler capacity through the actual
                    # receiver lifetime, including an ignored cancellation.
                    await pending
                return await asyncio.shield(result)
            except Exception as error:
                if not isinstance(error, PublicError):
                    self._panic(name, error, trace)
                raise public_error(error) from error
            finally:
                watcher.cancel()
                try:
                    # The outer handler must not race its own fallback response
                    # ahead of this already-admitted capability completion.
                    if answer is not None:
                        await asyncio.shield(answer)
                finally:
                    await asyncio.gather(watcher, return_exceptions=True)
                    cleanup()

        return dispatch

    def _answer(self, context, result=ABSENT, error=None, outcome=None):
        entry = self.peer._incoming.get(context.request_id)
        if entry is not None and entry.context is context:
            return self.peer._spawn(self.peer._respond(context.request_id, entry, result, error, outcome))

    async def _event(self, name, data, context):
        found = self._lookup(name)
        if found is None:
            return
        path, registration = found
        trace = context._received_trace
        frame = {"version": 1, "kind": "event", "data": data, **trace}
        if context.meta is not ABSENT:
            frame["meta"] = dict(context.meta)
        message = with_wire_event_context(Message(frame), context, lambda error: self._panic(name, error, trace))
        try:
            pending = registration.receiver.message(path, message)
            if inspect.isawaitable(pending):
                await pending
        except Exception as error:
            if not isinstance(error, PublicError):
                self._panic(name, error, trace)
            self._fail(public_error(error))

    def _fail(self, error):
        self.close(4011, error.message)

    def close(self, code=1000, reason=""):
        if self._ending:
            return
        self._ending = True
        asyncio.get_running_loop().call_soon(lambda: self.peer._spawn(self.peer._end(code, reason, True)))

    def _ended(self, code, reason):
        if self._closed:
            return
        self._closed = self._ending = True
        calls = list(self._calls.values())
        queued = list(self._queue)
        self._queue.clear()
        self._calls.clear()
        self._data_queued = 0
        for call in calls:
            call.completed = True
            if call.task is not None:
                call.task.cancel()
            if not call.responded:
                call.responded = True
                response(call.original, error=self._disconnected())
        for _, message, call, refusal in queued:
            if call is None and message.frame["kind"] == "request":
                response(message, error=self._disconnected())
        registrations = [self._attachment] if self._attachment else []
        self._attachment = None

        def notify():
            for registration in registrations:
                if registration.receiver.closed:
                    try:
                        registration.receiver.closed(code, reason)
                    except Exception:
                        pass

        asyncio.get_running_loop().call_soon(notify)
