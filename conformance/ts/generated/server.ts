/** The fixture owns HTTP upgrade, peers and their wires. Generated bindings
 * receive model factories and explicit conversion contexts. */
import { createServer, type Server } from 'node:http';
import { WebSocketServer } from 'ws';
import {
  DuplexPeer,
  createDispatcher,
  forwardWire,
  type AdapterContext,
  type PeerOptions,
  type WebSocketLike,
  type WireDispatcher,
} from '@nightseam/runtime';
import type { Endpoint } from '@bitspark/bitwire';
import { valueEnvironment, type LiveScope } from '@nightseam/live';

/** One fixture attachment point for adapter options and an explicit live scope. */
export function adapterContext(scope?: LiveScope, options?: PeerOptions): AdapterContext {
  return { options, ...(scope ? { valueEnvironment: valueEnvironment(scope) } : {}) };
}

/** Test transport assembly is separate from every generated model. The model
 * factory captures its opposite side while toWire installs the local model,
 * before attach/connect allows the physical connection to dispatch frames. */
export class Session<M> {
  readonly peer: DuplexPeer;
  model!: M;
  private local?: Endpoint;
  private detach?: () => void;
  private routes?: WireDispatcher;

  constructor(options: PeerOptions = {}) {
    this.peer = new DuplexPeer(options);
    this.peer.onClose(() => this.release());
  }

  /** The peer root has one owning attachment, so a fixture that both forwards
   * a model and registers a handler of its own shares that owner. */
  get dispatcher(): WireDispatcher {
    return (this.routes ??= createDispatcher(this.peer.wire()));
  }

  expose(wire: Endpoint): void {
    this.local = wire;
    // The model takes the root route; an exact registration beside it — a
    // fixture's own operation — wins for its own path.
    this.detach = forwardWire(this.dispatcher.select([]), wire);
  }

  async attach(socket: WebSocketLike): Promise<this> {
    try { await this.peer.attach(socket); return this; }
    catch (error) { this.close(); throw error; }
  }

  async connect(url: string): Promise<this> {
    try { await this.peer.connect(url); return this; }
    catch (error) { this.close(); throw error; }
  }

  private release(): void {
    this.detach?.();
    this.detach = undefined;
    this.routes?.close();
    this.routes = undefined;
    this.local?.close();
    this.local = undefined;
  }

  close(): void { this.release(); this.peer.close(); }
}

interface Remote { close(): void; }

export class Served<R extends Remote> {
  url = '';
  private latest?: R;
  private failure?: unknown;
  private stopped = false;
  private readonly remotes = new Set<R>();
  private readonly waiters = new Set<() => void>();
  private readonly http: Server;
  private readonly sockets: WebSocketServer;

  constructor(accept: (socket: WebSocketLike) => Promise<R>) {
    this.http = createServer();
    this.sockets = new WebSocketServer({ server: this.http, maxPayload: 1 << 20 });
    this.sockets.on('connection', socket => {
      // ws implements the browser event API; its overloads use ws's event types.
      void accept(socket as unknown as WebSocketLike).then(remote => {
        if (this.stopped) { remote.close(); return; }
        this.latest = remote;
        this.remotes.add(remote);
        for (const wake of this.waiters) wake();
      }, error => {
        this.failure = error;
        socket.terminate();
        for (const wake of this.waiters) wake();
      });
    });
  }

  async listen(): Promise<this> {
    await new Promise<void>((resolve, reject) => {
      this.http.once('error', reject);
      this.http.listen(0, '127.0.0.1', () => {
        this.http.removeListener('error', reject);
        const address = this.http.address();
        if (!address || typeof address === 'string') { reject(new Error('no listening address')); return; }
        this.url = `ws://127.0.0.1:${address.port}`;
        resolve();
      });
    }).catch(error => { this.shutdown(); throw error; });
    return this;
  }

  async remote(within: number): Promise<R | undefined> {
    if (this.latest) return this.latest;
    if (this.failure !== undefined) throw this.failure;
    return new Promise((resolve, reject) => {
      const wake = () => {
        clearTimeout(timer);
        this.waiters.delete(wake);
        if (this.failure !== undefined) reject(this.failure);
        else resolve(this.latest);
      };
      const timer = setTimeout(wake, within);
      this.waiters.add(wake);
    });
  }

  shutdown(): void {
    this.stopped = true;
    for (const remote of this.remotes) remote.close();
    for (const socket of this.sockets.clients) socket.terminate();
    this.remotes.clear();
    this.sockets.close();
    this.http.close();
    for (const wake of this.waiters) wake();
  }
}

/** A callback mailbox that removes timed-out waiters before the next event. */
export class Inbox<T> {
  private readonly items: T[] = [];
  private readonly waiters = new Set<() => void>();
  put(value: T): void {
    this.items.push(value);
    this.waiters.values().next().value?.();
  }
  async take(within: number): Promise<T | undefined> {
    if (this.items.length) return this.items.shift();
    return new Promise(resolve => {
      const wake = () => {
        clearTimeout(timer);
        this.waiters.delete(wake);
        resolve(this.items.shift());
      };
      const timer = setTimeout(wake, within);
      this.waiters.add(wake);
    });
  }
}
