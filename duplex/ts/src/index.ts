/**
 * A frames duplex connection: ordered, message-framed, bidirectional, with an
 * explicit close carrying a code and a reason. Nothing about JSON, requests,
 * correlation or events belongs here; those stay in the peer.
 */
export type Frame = { kind: 'text'; data: string } | { kind: 'binary'; data: ArrayBuffer | Uint8Array };
export type ConnectionState = 'connecting' | 'open' | 'closing' | 'closed';
export interface ConnectionHandlers {
  open?: () => void;
  frame?: (frame: Frame) => void;
  close?: (code: number, reason: string) => void;
  error?: () => void;
}

/**
 * Close codes are the WebSocket registry's numbers (1000 normal, 1008 policy,
 * 1009 too big, 1011 internal, 4000–4999 application) on every transport, so
 * that close semantics travel with the peer.
 */
export interface FrameConnection {
  readonly state: ConnectionState;
  /** Bytes accepted by send and not yet handed to the transport; the peer paces on it. */
  readonly buffered: number;
  /** Throws when the connection is not open. */
  send(frame: Frame): void;
  close(code?: number, reason?: string): void;
  /** Registers handlers; returns a function that detaches all of them. */
  listen(handlers: ConnectionHandlers): () => void;
}

/** The browser WebSocket surface, also implemented by Node's native WebSocket. */
export interface WebSocketLike {
  readonly readyState: number;
  readonly bufferedAmount: number;
  send(data: string): void;
  close(code?: number, reason?: string): void;
  addEventListener(type: string, listener: globalThis.EventListener): void;
  removeEventListener(type: string, listener: globalThis.EventListener): void;
}

const STATES: readonly ConnectionState[] = ['connecting', 'open', 'closing', 'closed'];
/** The registry's "no status code present", used when a close event carries none. */
const NO_STATUS = 1005;

/**
 * Adapts a WebSocket to a frames duplex connection: readyState maps to state,
 * bufferedAmount to buffered, a string message to a text frame, an ArrayBuffer,
 * Uint8Array or Blob to a binary frame, and the close event's code and reason
 * to the close handler. Text frames are delivered synchronously, in the turn
 * the socket delivers them.
 */
export function webSocketConnection(socket: WebSocketLike): FrameConnection {
  const listeners = new Set<ConnectionHandlers>();
  const each = <K extends keyof ConnectionHandlers>(name: K, ...args: Parameters<NonNullable<ConnectionHandlers[K]>>) => {
    for (const handlers of [...listeners]) {
      (handlers[name] as ((...args: unknown[]) => void) | undefined)?.(...args);
    }
  };
  // A Blob is read asynchronously; frames after it wait behind it so order holds.
  let tail: Promise<void> | undefined;
  const after = (task: () => void | Promise<void>) => {
    const next: Promise<void> = (tail ?? Promise.resolve()).then(task).catch(() => {}).then(() => {
      if (tail === next) tail = undefined;
    });
    tail = next;
  };
  const deliver = (frame: Frame) => {
    if (tail) after(() => each('frame', frame));
    else each('frame', frame);
  };
  const open = () => each('open');
  const message = (event: globalThis.Event) => {
    const data: unknown = (event as MessageEvent).data;
    if (typeof data === 'string') deliver({ kind: 'text', data });
    else if (data instanceof ArrayBuffer || data instanceof Uint8Array) deliver({ kind: 'binary', data });
    else if (isBlob(data)) {
      after(() => data.arrayBuffer().then(buffer => each('frame', { kind: 'binary', data: buffer }), () => each('error')));
    } else each('error');
  };
  const close = (event: globalThis.Event) => {
    const { code, reason } = event as Partial<CloseEvent>;
    detach();
    each('close', typeof code === 'number' ? code : NO_STATUS, typeof reason === 'string' ? reason : '');
  };
  const error = () => each('error');
  socket.addEventListener('open', open);
  socket.addEventListener('message', message);
  socket.addEventListener('close', close);
  socket.addEventListener('error', error);
  const detach = () => {
    socket.removeEventListener('open', open);
    socket.removeEventListener('message', message);
    socket.removeEventListener('close', close);
    socket.removeEventListener('error', error);
  };
  // Browsers deliver binary as Blob unless told otherwise; ArrayBuffer keeps delivery synchronous.
  if ('binaryType' in socket) {
    try { (socket as { binaryType: string }).binaryType = 'arraybuffer'; } catch { /* Not settable here. */ }
  }
  return {
    get state() { return STATES[socket.readyState] ?? 'closed'; },
    get buffered() { return socket.bufferedAmount; },
    send(frame) {
      if (socket.readyState !== 1) throw new Error('Connection is not open.');
      // WebSocketLike declares the text surface the peer needs; real sockets also accept binary.
      (socket.send as (data: string | ArrayBuffer | Uint8Array) => void)(frame.data);
    },
    close(code = 1000, reason = '') { socket.close(code, reason); },
    listen(handlers) {
      listeners.add(handlers);
      return () => { listeners.delete(handlers); };
    },
  };
}

/**
 * Two connected ends in memory: what one sends, the other receives, in order,
 * in a later microtask; a close on one end is the close on the other, with its
 * code and reason. It carries the profile in tests without a socket, as the
 * Go pipe does.
 */
export function pipe(): [FrameConnection, FrameConnection] {
  class End implements FrameConnection {
    state: ConnectionState = 'open';
    readonly buffered = 0;
    partner!: End;
    private readonly listeners = new Set<ConnectionHandlers>();
    send(frame: Frame): void {
      if (this.state !== 'open') throw new Error('Connection is not open.');
      const partner = this.partner;
      queueMicrotask(() => {
        if (partner.state === 'open') for (const handlers of [...partner.listeners]) handlers.frame?.(frame);
      });
    }
    close(code = 1000, reason = ''): void {
      if (this.state === 'closed') return;
      this.state = 'closed';
      for (const handlers of [...this.listeners]) handlers.close?.(code, reason);
      this.listeners.clear();
      this.partner.close(code, reason);
    }
    listen(handlers: ConnectionHandlers): () => void {
      this.listeners.add(handlers);
      return () => { this.listeners.delete(handlers); };
    }
  }
  const left = new End();
  const right = new End();
  left.partner = right;
  right.partner = left;
  return [left, right];
}

function isBlob(value: unknown): value is Blob {
  return typeof Blob !== 'undefined' && value instanceof Blob;
}
