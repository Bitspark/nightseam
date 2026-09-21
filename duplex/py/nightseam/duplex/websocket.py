"""WebSocket transport adapter for the frames connection protocol."""

import asyncio

from websockets.asyncio.client import connect
from websockets.asyncio.server import serve
from websockets.exceptions import ConnectionClosed

from . import CloseError, Frame


class WebSocketConnection:
    def __init__(self, socket):
        self.socket = socket
        self.subprotocol = socket.subprotocol or ""

    def _closed(self, error):
        close = error.rcvd or error.sent
        return CloseError(close.code, close.reason) if close else CloseError()

    async def send(self, frame):
        try:
            await self.socket.send(frame.data, text=frame.kind == "text")
        except ConnectionClosed as error:
            raise self._closed(error) from error

    async def receive(self):
        try:
            data = await self.socket.recv()
            return Frame("text" if isinstance(data, str) else "binary", data)
        except ConnectionClosed as error:
            raise self._closed(error) from error

    async def close(self, code=1000, reason=""):
        await self.socket.close(code, reason)

    def abort(self):
        self.socket.transport.abort()


async def dial(url, *, limit=1 << 20, subprotocols=None, open_timeout=30):
    if limit < 0:
        raise ValueError("receive limit must not be negative")
    socket = await connect(
        url,
        max_size=limit or None,
        max_queue=8,
        write_limit=32768,
        compression=None,
        proxy=None,
        subprotocols=subprotocols or None,
        open_timeout=open_timeout,
        ping_interval=None,
    )
    return WebSocketConnection(socket)


class Listener:
    def __init__(self):
        self._accepted = asyncio.Queue(maxsize=8)
        self._server = None
        self._closed = asyncio.Event()

    @property
    def url(self):
        host, port = self._server.sockets[0].getsockname()[:2]
        return f"ws://{host}:{port}"

    async def accept(self):
        take = asyncio.create_task(self._accepted.get())
        ended = asyncio.create_task(self._closed.wait())
        try:
            await asyncio.wait((take, ended), return_when=asyncio.FIRST_COMPLETED)
            if ended.done():
                raise CloseError()
            return take.result()
        finally:
            for task in (take, ended):
                if not task.done():
                    task.cancel()
            await asyncio.gather(take, ended, return_exceptions=True)

    async def close(self):
        self._closed.set()
        self._server.close()
        await self._server.wait_closed()


async def listen(host="127.0.0.1", port=0, *, limit=1 << 20, subprotocols=None):
    if limit < 0:
        raise ValueError("receive limit must not be negative")
    listener = Listener()

    async def connected(socket):
        admission = asyncio.create_task(listener._accepted.put(WebSocketConnection(socket)))
        ended = asyncio.create_task(socket.wait_closed())
        stopping = asyncio.create_task(listener._closed.wait())
        try:
            await asyncio.wait((admission, ended, stopping), return_when=asyncio.FIRST_COMPLETED)
            if not ended.done() and not stopping.done():
                await ended
        finally:
            for task in (admission, ended, stopping):
                if not task.done():
                    task.cancel()
            await asyncio.gather(admission, ended, stopping, return_exceptions=True)

    def select(socket, offered):
        return next((protocol for protocol in subprotocols or [] if protocol in offered), None)

    listener._server = await serve(
        connected,
        host,
        port,
        max_size=limit or None,
        max_queue=8,
        write_limit=32768,
        compression=None,
        ping_interval=None,
        subprotocols=subprotocols or None,
        select_subprotocol=select,
    )
    return listener
