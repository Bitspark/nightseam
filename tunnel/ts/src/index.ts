/**
 * Channels multiplexed over one peer of the nightseam.duplex/1 profile: a
 * third transport beneath the seam, after the WebSocket and the pipe. Either
 * side opens a channel; each channel is a FrameConnection, and a peer of any
 * family runs over it unchanged. The outer peer sees four operations of the
 * profile's own — channel.open, a request; channel.frame, channel.credit and
 * channel.close, events — and never what a channel carries.
 *
 * Flow control is per channel, by credit: each side may have at most a window
 * of frames in flight to the other on a channel, the window the other side
 * declared when the channel was opened, and a receiver returns credit as it
 * takes frames. A channel's id is chosen by the side that opens it, odd for
 * the client of the outer connection and even for its server, and is what a
 * handle names: {"channel": 12} in a family's message.
 */
import { DuplexError, type DuplexPeer } from '@nightseam/runtime';
import type { ConnectionHandlers, ConnectionState, Frame, FrameConnection } from '@nightseam/duplex';

export const OPEN_METHOD = 'channel.open';
export const FRAME_EVENT = 'channel.frame';
export const CREDIT_EVENT = 'channel.credit';
export const CLOSE_EVENT = 'channel.close';

export interface TunnelOptions {
  /** Bounds a frame received over a channel; a larger one is refused before delivery, and the channel with it. Default: one mebibyte. */
  maxFrameBytes?: number;
  /** How many frames the other side may have in flight to this one on a channel before its send waits for credit. Default: 32. */
  window?: number;
  /** How many channels the other side may have opened that nobody here accepted or resolved; an open beyond it is refused. Default: 64. */
  acceptCapacity?: number;
}

interface Acceptor { resolve: (channel: Channel) => void; reject: (error: DuplexError) => void }

function positive(value: unknown, name: string): number {
  if (typeof value !== 'number' || !Number.isInteger(value) || value <= 0) throw new DuplexError('invalid_options', `${name} must be a positive integer.`);
  return value;
}

/** The channels of one outer peer. */
export class Tunnel {
  private readonly peer: DuplexPeer;
  private readonly options: Required<TunnelOptions>;
  private next: number;
  private readonly parity: number;
  private readonly table = new Map<number, Channel>();
  private readonly pending: Channel[] = [];
  private readonly acceptors: Acceptor[] = [];
  private closed = false;

  /** Multiplexes channels over a peer, registering the tunnel's operations on it; a peer carries one tunnel. */
  constructor(peer: DuplexPeer, options: TunnelOptions = {}) {
    this.peer = peer;
    this.options = {
      maxFrameBytes: positive(options.maxFrameBytes ?? 1 << 20, 'maxFrameBytes'),
      window: positive(options.window ?? 32, 'window'),
      acceptCapacity: positive(options.acceptCapacity ?? 64, 'acceptCapacity'),
    };
    this.parity = peer.role === 'server' ? 0 : 1;
    this.next = this.parity === 0 ? 2 : 1;
    peer.handle(OPEN_METHOD, params => this.onOpen(params));
    peer.onEvent(FRAME_EVENT, data => this.onFrame(data));
    peer.onEvent(CREDIT_EVENT, data => this.onCredit(data));
    peer.onEvent(CLOSE_EVENT, data => this.onClose(data));
    peer.onClose(() => this.onOuterClose());
  }

  get limits(): Readonly<Required<TunnelOptions>> { return this.options; }

  /** Opens a channel to the other side, saying what family it speaks and the last sequence this side holds; resolves once the other side accepted it. */
  async open(family: string, after = 0): Promise<Channel> {
    if (typeof family !== 'string' || family === '') throw new DuplexError('channel_invalid', 'A channel is opened for a family.');
    if (!Number.isInteger(after) || after < 0) throw new DuplexError('channel_invalid', 'after must be a sequence.');
    const id = this.next;
    this.next += 2;
    const channel = new Channel(this, id, family, after, 0);
    this.table.set(id, channel);
    let result: unknown;
    try {
      result = await this.peer.call(OPEN_METHOD, { channel: id, family, after, window: this.options.window });
    } catch (error) {
      this.table.delete(id);
      channel.endLocal();
      throw error;
    }
    const window = (result as { window?: unknown } | null)?.window;
    if (typeof window !== 'number' || !Number.isInteger(window) || window <= 0) {
      this.table.delete(id);
      channel.endLocal();
      throw new DuplexError('channel_invalid', 'The other side declared no window.');
    }
    channel.grant(window);
    return channel;
  }

  /** The next channel the other side opened that nobody here has taken yet, by accept or by channel. */
  accept(): Promise<Channel> {
    const next = this.pending.shift();
    if (next) return Promise.resolve(next);
    if (this.closed) return Promise.reject(new DuplexError('disconnected', 'The connection carrying the channels closed.'));
    return new Promise((resolve, reject) => { this.acceptors.push({ resolve, reject }); });
  }

  /** Resolves an id to the channel it names, whichever side opened it; a channel the other side opened counts as taken. */
  channel(id: number): Channel | undefined {
    const channel = this.table.get(id);
    if (channel) {
      const at = this.pending.indexOf(channel);
      if (at >= 0) this.pending.splice(at, 1);
    }
    return channel;
  }

  /** @internal */
  remove(id: number): void {
    const channel = this.table.get(id);
    if (!channel) return;
    this.table.delete(id);
    const at = this.pending.indexOf(channel);
    if (at >= 0) this.pending.splice(at, 1);
  }

  /** @internal */
  emit(event: string, data: unknown): Promise<void> { return this.peer.emit(event, data); }

  private onOpen(params: unknown): { window: number } {
    const p = params as Partial<Record<'channel' | 'family' | 'after' | 'window', unknown>> | null;
    if (!p || typeof p !== 'object' || typeof p.channel !== 'number' || !Number.isInteger(p.channel) || p.channel <= 0 || typeof p.family !== 'string' || p.family === '' || typeof p.after !== 'number' || !Number.isInteger(p.after) || p.after < 0 || typeof p.window !== 'number' || !Number.isInteger(p.window) || p.window <= 0) {
      throw new DuplexError('channel_invalid', 'channel.open needs a positive channel id of the opener\'s parity, a family, a sequence and a window.');
    }
    if (p.channel % 2 === this.parity) throw new DuplexError('channel_invalid', 'The channel id is of this side\'s parity.');
    if (this.table.has(p.channel)) throw new DuplexError('channel_exists', `Channel ${p.channel} is open.`);
    if (this.acceptors.length === 0 && this.pending.length >= this.options.acceptCapacity) {
      throw new DuplexError('channel_refused', 'No room for a channel nobody has accepted.');
    }
    const channel = new Channel(this, p.channel, p.family, p.after, p.window);
    this.table.set(p.channel, channel);
    const acceptor = this.acceptors.shift();
    if (acceptor) acceptor.resolve(channel);
    else this.pending.push(channel);
    return { window: this.options.window };
  }

  private onFrame(data: unknown): void {
    const payload = data as Partial<Record<'channel' | 'text' | 'binary', unknown>> | null;
    if (!payload || typeof payload !== 'object' || typeof payload.channel !== 'number') return;
    const channel = this.table.get(payload.channel);
    if (!channel) return;
    let frame: Frame;
    let size: number;
    if (typeof payload.text === 'string' && payload.binary === undefined) {
      frame = { kind: 'text', data: payload.text };
      size = new TextEncoder().encode(payload.text).byteLength;
    } else if (typeof payload.binary === 'string' && payload.text === undefined) {
      let bytes: Uint8Array;
      try { bytes = decodeBase64(payload.binary); } catch {
        channel.fail(1002, 'a binary frame that is not base64');
        return;
      }
      frame = { kind: 'binary', data: bytes };
      size = bytes.byteLength;
    } else {
      channel.fail(1002, 'a frame that is neither text nor binary');
      return;
    }
    if (size > this.options.maxFrameBytes) {
      channel.fail(1009, `a frame of ${size} bytes exceeds the limit of ${this.options.maxFrameBytes}`);
      return;
    }
    channel.deliver(frame);
  }

  private onCredit(data: unknown): void {
    const payload = data as Partial<Record<'channel' | 'frames', unknown>> | null;
    if (!payload || typeof payload !== 'object' || typeof payload.channel !== 'number' || typeof payload.frames !== 'number' || !Number.isInteger(payload.frames) || payload.frames <= 0) return;
    this.table.get(payload.channel)?.grant(payload.frames);
  }

  private onClose(data: unknown): void {
    const payload = data as Partial<Record<'channel' | 'code' | 'reason', unknown>> | null;
    if (!payload || typeof payload !== 'object' || typeof payload.channel !== 'number') return;
    const channel = this.table.get(payload.channel);
    if (!channel) return;
    this.remove(payload.channel);
    channel.endRemote(typeof payload.code === 'number' ? payload.code : 1005, typeof payload.reason === 'string' ? payload.reason : '');
  }

  private onOuterClose(): void {
    this.closed = true;
    const channels = [...this.table.values()];
    this.table.clear();
    this.pending.length = 0;
    for (const channel of channels) channel.endRemote(1001, 'the connection carrying the channel closed');
    for (const acceptor of this.acceptors.splice(0)) acceptor.reject(new DuplexError('disconnected', 'The connection carrying the channels closed.'));
  }
}

/**
 * One channel of a tunnel: a FrameConnection, and what the opener said of
 * it — its id on the outer connection, the family it speaks and the last
 * sequence the opener holds, from which the other side resumes. A send
 * beyond the other side's window waits in the channel, and buffered counts
 * what waits, so a peer above paces on it as on any connection.
 */
export class Channel implements FrameConnection {
  readonly id: number;
  readonly family: string;
  readonly after: number;
  private readonly tunnel: Tunnel;
  private credit: number;
  private taken = 0;
  private readonly queued: Frame[] = [];
  private readonly listeners = new Set<ConnectionHandlers>();
  private current: ConnectionState = 'open';

  /** @internal */
  constructor(tunnel: Tunnel, id: number, family: string, after: number, credit: number) {
    this.tunnel = tunnel;
    this.id = id;
    this.family = family;
    this.after = after;
    this.credit = credit;
  }

  get state(): ConnectionState { return this.current; }
  get buffered(): number { return this.queued.length; }

  send(frame: Frame): void {
    if (this.current !== 'open') throw new Error('Channel is not open.');
    if (this.credit > 0) {
      this.credit--;
      this.transmit(frame);
    } else {
      this.queued.push(frame);
    }
  }

  close(code = 1000, reason = ''): void {
    if (this.current === 'closed') return;
    this.current = 'closed';
    this.queued.length = 0;
    this.tunnel.remove(this.id);
    void this.tunnel.emit(CLOSE_EVENT, { channel: this.id, code, reason }).catch(() => { /* The outer peer reports its own failure. */ });
    this.ended(code, reason);
  }

  listen(handlers: ConnectionHandlers): () => void {
    this.listeners.add(handlers);
    return () => { this.listeners.delete(handlers); };
  }

  private transmit(frame: Frame): void {
    const payload = frame.kind === 'text' ? { channel: this.id, text: frame.data } : { channel: this.id, binary: encodeBase64(frame.data) };
    void this.tunnel.emit(FRAME_EVENT, payload).catch(() => { /* The outer peer reports its own failure; the channel ends with it. */ });
  }

  /** @internal */
  grant(frames: number): void {
    this.credit += frames;
    while (this.credit > 0 && this.queued.length > 0) {
      this.credit--;
      this.transmit(this.queued.shift()!);
    }
  }

  /** @internal */
  deliver(frame: Frame): void {
    if (this.current !== 'open') return;
    this.taken++;
    if (this.taken >= Math.max(Math.floor(this.tunnel.limits.window / 2), 1)) {
      const frames = this.taken;
      this.taken = 0;
      void this.tunnel.emit(CREDIT_EVENT, { channel: this.id, frames }).catch(() => { /* The outer peer reports its own failure. */ });
    }
    for (const handlers of [...this.listeners]) handlers.frame?.(frame);
  }

  /** @internal */
  endLocal(): void {
    this.current = 'closed';
    this.queued.length = 0;
    this.listeners.clear();
  }

  /** @internal */
  endRemote(code: number, reason: string): void {
    if (this.current === 'closed') return;
    this.current = 'closed';
    this.queued.length = 0;
    this.ended(code, reason);
  }

  /** @internal */
  fail(code: number, reason: string): void {
    if (this.current === 'closed') return;
    this.current = 'closed';
    this.queued.length = 0;
    this.tunnel.remove(this.id);
    void this.tunnel.emit(CLOSE_EVENT, { channel: this.id, code, reason }).catch(() => { /* The outer peer reports its own failure. */ });
    this.ended(code, reason);
  }

  private ended(code: number, reason: string): void {
    for (const handlers of [...this.listeners]) handlers.close?.(code, reason);
    this.listeners.clear();
  }
}

function encodeBase64(data: ArrayBuffer | Uint8Array): string {
  const bytes = data instanceof Uint8Array ? data : new Uint8Array(data);
  let binary = '';
  for (let at = 0; at < bytes.length; at += 0x8000) binary += String.fromCharCode(...bytes.subarray(at, at + 0x8000));
  return btoa(binary);
}

function decodeBase64(text: string): Uint8Array {
  const binary = atob(text);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return bytes;
}
