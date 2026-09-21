"""Private driver-1 adapter. All protocol behavior is in the public packages."""

# ruff: noqa: E402 -- The driver resolves the two source components explicitly.

import asyncio
import base64
import sys
import traceback
from collections import deque
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
sys.path[:0] = [str(ROOT / "duplex/py"), str(ROOT / "runtime/py")]

from nightseam.duplex import CloseError, Frame, pipe
from nightseam.duplex.websocket import dial, listen
from nightseam.runtime import ABSENT, IDENTITY_METHOD, Options, Peer, PublicError, check_identity, identity_handler
from nightseam.runtime.json import dumps, loads, raw_members


class DriverError(Exception):
    def __init__(self, code, message, **members):
        self.result = {"code": code, "message": message, **members}


class Inbox:
    def __init__(self):
        self.items = deque()
        self.changed = asyncio.Event()

    def put(self, value):
        self.items.append(value)
        self.changed.set()

    async def take(self, predicate=lambda value: True):
        while True:
            for index, value in enumerate(self.items):
                if predicate(value):
                    del self.items[index]
                    return value
            self.changed.clear()
            await self.changed.wait()


class Conn:
    def __init__(self, connection, lazy=False):
        self.connection = connection
        self.frames = Inbox()
        self.ended = None
        self.closed = asyncio.Event()
        self.task = None if lazy else asyncio.create_task(self.read())

    async def read(self):
        try:
            while True:
                self.frames.put(await self.connection.receive())
        except CloseError as error:
            self.ended = error
            self.closed.set()
            self.frames.put(error)
        except asyncio.CancelledError:
            pass

    async def receive(self):
        if self.task is None:
            try:
                return await self.connection.receive()
            except CloseError as error:
                self.ended = error
                self.closed.set()
                raise
        if self.ended is not None and not self.frames.items:
            raise self.ended
        value = await self.frames.take()
        if isinstance(value, CloseError):
            raise value
        return value

    async def release(self):
        if self.task:
            self.task.cancel()
            await self.task
            self.task = None
        owner = self

        class Released:
            subprotocol = getattr(owner.connection, "subprotocol", "")

            async def send(self, frame):
                await owner.connection.send(frame)

            async def receive(self):
                if owner.frames.items:
                    value = owner.frames.items.popleft()
                    if isinstance(value, CloseError):
                        raise value
                    return value
                return await owner.connection.receive()

            async def close(self, code=1000, reason=""):
                await owner.connection.close(code, reason)

            def abort(self):
                owner.connection.abort()

        return Released()

    async def shutdown(self):
        self.connection.abort()
        if self.task:
            self.task.cancel()
            await self.task


class ControlledPeer:
    def __init__(self, connection, role, options):
        self.events, self.requests = Inbox(), Inbox()
        self.observations = []
        self.observable = options.get("observe", False)
        values = {name: value for name, value in options.items() if name not in ("observe", "propagate")}
        self.peer = Peer(connection, role, Options(**values, observer=self.observations.append))

        def received(name, data, context):
            item = {"name": name, "data": data}
            if context.meta is not ABSENT:
                item["meta"] = context.meta
            self.events.put(item)

        self.peer.on_event(None, received)

    def report(self, trace, drain):
        result = []
        for event in self.observations:
            item = {}
            for key, value in event.items():
                if key == "at" or value == "" or value is None:
                    continue
                if key == "trace":
                    if trace and value.get("traceparent"):
                        _, trace_id, span_id, flags = value["traceparent"].split("-")
                        item[key] = {"trace_id": trace_id, "span_id": span_id, "flags": flags}
                        if value.get("tracestate"):
                            item[key]["state"] = value["tracestate"]
                elif key in ("bytes", "duration", "deadline"):
                    item[key] = value > 0 if key == "bytes" else value >= 0
                else:
                    item[key] = value
            result.append(item)
        if drain:
            self.observations.clear()
        return result

    async def shutdown(self):
        await self.peer.close()

    def canned(self, method, behavior):
        async def handler(params, context):
            item = {"id": context.request_id, "method": method, "phase": "started"}
            if context.meta is not ABSENT:
                item["meta"] = context.meta
            self.requests.put(item)

            def ended(outcome):
                self.requests.put({"id": context.request_id, "method": method, "phase": "ended", "outcome": outcome})

            kind = behavior.get("kind")
            try:
                if kind == "echo":
                    result = context.raw
                elif kind == "return":
                    result = behavior.get("value")
                elif kind == "fail":
                    raise PublicError(
                        behavior.get("code", "internal"),
                        behavior.get("message", "failure"),
                        behavior.get("data", ABSENT),
                    )
                elif kind == "wait":
                    await context.cancelled.wait()
                    ended("cancelled")
                    return None
                elif kind == "hold":
                    release = asyncio.Event()
                    off = self.peer.on_event(behavior.get("until", ""), lambda value, context: release.set())
                    try:
                        await release.wait()
                    finally:
                        off()
                    result = behavior.get("value")
                elif kind == "panic":
                    raise RuntimeError(str(behavior.get("value", "the handler gave up")))
                elif kind == "reverse":
                    result = await self.peer.call(
                        behavior.get("method", ""), behavior.get("params", context.raw), context=context
                    )
                elif kind == "emit":
                    await self.peer.emit(behavior.get("event", ""), behavior.get("data"), context=context)
                    result = behavior.get("then")
                else:
                    raise PublicError("internal", "unknown behavior " + str(kind))
                ended("ok")
                return result
            except PublicError:
                ended("cancelled" if context.cancelled.is_set() else "error")
                raise
            except asyncio.CancelledError:
                ended("cancelled")
                raise
            except Exception:
                ended("panic")
                raise

        return handler


class PeerListener:
    def __init__(self, listener, options):
        self.listener, self.options = listener, options
        self.accepted = asyncio.Queue()
        self.peers = []
        self.task = asyncio.create_task(self.run())

    async def run(self):
        try:
            while True:
                connection = await self.listener.accept()
                peer = ControlledPeer(connection, "server", self.options)
                self.peers.append(peer)
                self.accepted.put_nowait(peer)
        except (asyncio.CancelledError, CloseError):
            pass

    async def shutdown(self):
        self.task.cancel()
        await self.task
        await asyncio.gather(*(peer.shutdown() for peer in self.peers))
        await self.listener.close()


class Testee:
    def __init__(self):
        self.handles = {}
        self.next = 0

    def mint(self, value):
        self.next += 1
        handle = "h" + str(self.next)
        self.handles[handle] = value
        return handle

    def get(self, args, expected):
        handle = args.get("on")
        if handle not in self.handles:
            raise DriverError("unknown_handle", "unknown handle " + str(handle))
        value = self.handles[handle]
        if not isinstance(value, expected):
            raise DriverError("invalid", "handle has the wrong kind")
        return value

    async def reset(self):
        values = list(self.handles.values())
        self.handles.clear()
        for value in values:
            if isinstance(value, asyncio.Task):
                value.cancel()
        tasks = []
        for value in values:
            if hasattr(value, "shutdown"):
                tasks.append(value.shutdown())
            elif hasattr(value, "close"):
                tasks.append(value.close())
        await asyncio.gather(*tasks, return_exceptions=True)
        await asyncio.gather(*(value for value in values if isinstance(value, asyncio.Task)), return_exceptions=True)

    async def op(self, args, raw):
        op = args["op"]
        if op == "hello":
            return {
                "driver": 1,
                "language": "python",
                "layers": ["seam", "peer"],
                "features": ["listen", "pipe", "lazy", "observer", "propagator"],
            }
        if op in ("reset", "bye"):
            await self.reset()
            return {}
        if op in ("conn.listen", "peer.listen"):
            options = args.get("options", {})
            listener = await listen(
                limit=args.get("limit", options.get("max_frame_bytes", 1 << 20)), subprotocols=args.get("subprotocols")
            )
            value = PeerListener(listener, options) if op.startswith("peer") else listener
            return {"handle": self.mint(value), "url": listener.url}
        if op in ("conn.dial", "peer.dial"):
            options = args.get("options", {})
            connection = await dial(
                args["url"],
                limit=args.get("limit", options.get("max_frame_bytes", 1 << 20)),
                subprotocols=args.get("subprotocols"),
            )
            if op.startswith("peer"):
                peer = ControlledPeer(connection, "client", options)
                return {"handle": self.mint(peer), "subprotocol": peer.peer.subprotocol}
            return {"handle": self.mint(Conn(connection, args.get("consume") == "lazy"))}
        if op == "conn.accept":
            from nightseam.duplex.websocket import Listener

            listener = self.get(args, Listener)
            return {"handle": self.mint(Conn(await listener.accept(), args.get("consume") == "lazy"))}
        if op == "peer.accept":
            listener = self.get(args, PeerListener)
            peer = await listener.accepted.get()
            return {"handle": self.mint(peer), "subprotocol": peer.peer.subprotocol}
        if op == "conn.pipe":
            a, b = pipe(args.get("limit", 1 << 20))
            return {
                "a": self.mint(Conn(a, args.get("consume") == "lazy")),
                "b": self.mint(Conn(b, args.get("consume") == "lazy")),
            }
        if op == "peer.over":
            connection = await self.get(args, Conn).release()
            return {"handle": self.mint(ControlledPeer(connection, args["role"], args.get("options", {})))}
        if op.startswith("conn."):
            connection = self.get(args, Conn)
            if op == "conn.send":
                kind = args["kind"]
                data = args.get("text", "") if kind == "text" else base64.b64decode(args.get("base64", ""))
                await connection.connection.send(Frame(kind, data))
            elif op == "conn.receive":
                frame = await connection.receive()
                return (
                    {"kind": frame.kind, "text": frame.data}
                    if frame.kind == "text"
                    else {"kind": frame.kind, "base64": base64.b64encode(frame.data).decode("ascii")}
                )
            elif op == "conn.close":
                await connection.connection.close(args.get("code", 1000), args.get("reason", ""))
            elif op == "conn.abort":
                connection.connection.abort()
            elif op == "conn.await_close":
                if connection.task is None:
                    connection.task = asyncio.create_task(connection.read())
                await connection.closed.wait()
                return {"code": connection.ended.code, "reason": connection.ended.reason}
            else:
                raise DriverError("unsupported", op)
            return {}
        if op.startswith("peer."):
            controlled = self.get(args, ControlledPeer)
            peer = controlled.peer
            if op in ("peer.identity", "peer.check_identity"):
                identity = {"path": args["path"]}
                if args.get("digest"):
                    identity["digest"] = args["digest"]
                if op == "peer.identity":
                    peer.handle(IDENTITY_METHOD, identity_handler(identity))
                else:
                    await check_identity(peer.call, identity, timeout_ms=args.get("within_ms", 5000))
            elif op == "peer.handle":
                peer.handle(args["method"], controlled.canned(args["method"], args.get("behavior", {})))
            elif op == "peer.on_event":
                behavior = args.get("behavior", "record")
                if isinstance(behavior, dict):
                    behavior = behavior.get("kind", "record")
                if behavior == "block":

                    async def block(value, context):
                        await asyncio.Event().wait()

                    peer.on_event(args["name"], block)
                elif behavior == "panic":

                    def panic(value, context):
                        raise RuntimeError("event handler panic")

                    peer.on_event(args["name"], panic)
            elif op == "peer.call":
                task = asyncio.create_task(
                    peer.call(
                        args["method"],
                        raw.get("params", ABSENT),
                        timeout_ms=args.get("timeout_ms"),
                        meta=args.get("meta", ABSENT),
                    )
                )
                task.add_done_callback(lambda done: None if done.cancelled() else done.exception())
                await asyncio.sleep(0)  # The call starts before its handle is returned.
                return {"handle": self.mint(task)}
            elif op == "peer.emit":
                await peer.emit(args["event"], raw.get("data", ABSENT), meta=args.get("meta", ABSENT))
            elif op == "peer.await_event":
                item = await controlled.events.take(lambda item: item["name"] == args["name"])
                return {key: value for key, value in item.items() if key != "name"}
            elif op == "peer.await_request":
                return await controlled.requests.take(
                    lambda item: item["method"] == args["method"] and item["phase"] == args["phase"]
                )
            elif op == "peer.observed":
                if not controlled.observable:
                    raise DriverError("unsupported", "peer was not opened with observe")
                return controlled.report(args.get("trace", False), args.get("drain", True))
            elif op == "peer.close":
                await peer.close()
            elif op == "peer.await_close":
                closed = await peer.wait_closed()
                return {"clean": closed["clean"], "code": closed["code"]}
            else:
                raise DriverError("unsupported", op)
            return {}
        if op == "call.cancel":
            self.get(args, asyncio.Task).cancel()
            await asyncio.sleep(0)
            return {}
        if op == "call.await":
            task = self.get(args, asyncio.Task)
            try:
                return {"result": await asyncio.shield(task)}
            except asyncio.CancelledError:
                if task.cancelled():
                    return {"error": {"code": "cancelled", "message": "call cancelled"}}
                raise
            except PublicError as error:
                return {"error": error.envelope()}
        raise DriverError("unsupported", op)


async def main():
    testee = Testee()
    try:
        while True:
            line = await asyncio.to_thread(sys.stdin.buffer.readline)
            if not line:
                break
            args = loads(line)
            response = {"id": args["id"]}
            try:
                async with asyncio.timeout(args.get("within_ms", 5000) / 1000):
                    response["ok"] = await testee.op(args, dict(raw_members(line)))
            except TimeoutError:
                response["error"] = {"code": "timeout", "message": "operation did not settle before its deadline"}
            except DriverError as error:
                response["error"] = error.result
            except CloseError as error:
                response["error"] = {
                    "code": "closed",
                    "message": str(error),
                    "close_code": error.code,
                    "reason": error.reason,
                }
            except PublicError as error:
                response["error"] = error.envelope()
            except (ValueError, TypeError, KeyError) as error:
                response["error"] = {"code": "invalid", "message": str(error)}
            except Exception as error:
                traceback.print_exc(file=sys.stderr)
                response["error"] = {"code": "failed", "message": str(error)}
            sys.stdout.buffer.write((dumps(response) + "\n").encode("utf8"))
            sys.stdout.buffer.flush()
            if args["op"] == "bye":
                break
    finally:
        await testee.reset()


if __name__ == "__main__":
    asyncio.run(main())
