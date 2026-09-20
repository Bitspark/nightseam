/** The fixture owns HTTP upgrade and its open test policy; generated bindings
 * receive only accepted connections and own the protocol dispatch above them. */
import { createServer, type Server } from 'node:http';
import { WebSocketServer } from 'ws';
import type { WebSocketLike } from '@nightseam/runtime';

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
