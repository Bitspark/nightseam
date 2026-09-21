/**
 * The TypeScript generated testee: what the generator renders for the
 * probe family — its client and binding — under the runner's control over the generated
 * ops of conformance/DRIVER.md. Laid beside the rendering by the runner,
 * under the checkout, so that @nightseam/runtime resolves through the
 * workspace and the runtime beneath the rendering is the real one.
 */
import { proofOps, resetProof, ProofFailure } from './proof.ts';
import { liveOps, resetLive, LiveFailure } from './live.ts';
import { combinatorOps, resetCombinator, CombinatorFailure } from './combinator.ts';
import { forwardingOps, resetForwarding, ForwardingFailure } from './forwarding.ts';
import { ownersOps, resetOwners, OwnerFailure } from './owners.ts';
import { publicationOps, resetPublication, PublicationFailure } from './publication.ts';
import { wireCellOps, resetWireCells } from './wire-cell.ts';
import { createInterface } from 'node:readline';
import * as probe from './api/ts/probe-client/src/index.ts';
import { DuplexError, errors, validateWire, type Payload, type Seen } from './api/ts/probe-client/src/index.ts';
import * as binding from './api/ts/probe-binding/src/index.ts';
import { Inbox, Served, Session } from './server.ts';

class Failure extends Error {
  readonly code: string;
  constructor(code: string, message: string) {
    super(message);
    this.code = code;
  }
  toJSON(): Record<string, unknown> { return { code: this.code, message: this.message }; }
}
const fail = (code: string, message: string) => new Failure(code, message);

type Args = Record<string, unknown>;

class Dialled {
  connection!: Session<probe.Server>;
  get client(): probe.Server { return this.connection.model; }
  readonly changed: Payload[] = [];
  readonly notifications: Array<{event:string; data:unknown}> = [];
  private readonly notificationWaiters: Array<() => void> = [];
  private readonly waiters: Array<() => void> = [];
  changedEvent(data: Payload): void { this.changed.push(data); for (const w of this.waiters.splice(0)) w(); this.notification('changed',data); }
  private notification(event:string,data:unknown): void { this.notifications.push({event,data}); for(const w of this.notificationWaiters.splice(0)) w(); }
  awaitNotification(withinMs:number): Promise<{event:string; data:unknown}|undefined> {
    if(this.notifications.length) return Promise.resolve(this.notifications.shift());
    return new Promise(resolve=>{
      const wake=()=>{clearTimeout(timer); resolve(this.notifications.shift());};
      const timer=setTimeout(()=>{const i=this.notificationWaiters.indexOf(wake); if(i>=0)this.notificationWaiters.splice(i,1); resolve(undefined);},withinMs);
      this.notificationWaiters.push(wake);
    });
  }
  awaitChanged(withinMs: number): Promise<Payload | undefined> {
    if (this.changed.length) return Promise.resolve(this.changed.shift());
    return new Promise(resolve => {
      const timer = setTimeout(() => resolve(undefined), withinMs);
      this.waiters.push(() => { clearTimeout(timer); resolve(this.changed.shift()); });
    });
  }
  shutdown(): void { this.connection.close(); }
}

const handles = new Map<string, Dialled>();
const servers = new Map<string, { served: Served<Session<probe.Client>>; noticed: Inbox<Seen> }>();
let next = 0;
let bye = false;

const reset = () => {
  resetProof();
  resetLive();
  resetCombinator();
  resetForwarding();
  resetOwners();
  resetPublication();
  resetWireCells();
  for (const d of handles.values()) d.shutdown();
  handles.clear();
  for (const s of servers.values()) s.served.shutdown();
  servers.clear();
};

const withinOf = (args: Args) => typeof args.within_ms === 'number' ? args.within_ms : 5000;

const dialledOf = (args: Args): Dialled => {
  const d = typeof args.on === 'string' ? handles.get(args.on) : undefined;
  if (!d) throw fail('unknown_handle', String(args.on));
  return d;
};

const servedOf = (args: Args) => {
  const s = servers.get(String(args.on));
  if (!s) throw fail('unknown_handle', String(args.on));
  return s;
};

const remoteOf = async (args: Args) => {
  const remote = await servedOf(args).served.remote(withinOf(args));
  if (!remote) throw fail('timeout', 'nobody connected');
  return remote.model;
};

/** How a typed call ended: the public error's code and data. */
const callError = (error: unknown): Record<string, unknown> => {
  if (error instanceof DuplexError) {
    const out: Record<string, unknown> = { code: error.code, message: error.message };
    if (error.data !== undefined) out.data = error.data;
    return out;
  }
  return { code: 'failed', message: String(error) };
};

const typed = async (work: () => Promise<unknown>): Promise<Record<string, unknown>> => {
  try {
    return { result: await work() };
  } catch (error) {
    return { error: callError(error) };
  }
};

const ops: Record<string, (args: Args) => Promise<unknown> | unknown> = {
  ...proofOps,
  ...liveOps,
  ...combinatorOps,
  ...forwardingOps,
  ...ownersOps,
  ...publicationOps,
  ...wireCellOps,
  hello: () => ({ driver: 1, language: 'typescript', layers: ['generated'], features: ['listen'] }),
  reset: () => { reset(); return {}; },
  bye: () => { bye = true; reset(); return {}; },
  'gen.serve': async () => {
    const noticed = new Inbox<Seen>();
    const served = await new Served(socket => {
      const connection = new Session<probe.Client>({ role: 'server' });
      connection.expose(binding.toWire(remote => {
        connection.model = remote;
        return { methods: {
          echo: params => ({ ...params, text: [...params.text].reverse().join('') }),
          noArgs: () => 'none',
          seen: () => [],
        }, events: { noticed: data => noticed.put(data) } };
      }, {}));
      return connection.attach(socket);
    }).listen();
    const handle = `srv${++next}`;
    servers.set(handle, { served, noticed });
    return { handle, url: served.url };
  },
  'gen.dial': async args => {
    const dialled = new Dialled();
    const connection = new Session<probe.Server>();
    connection.expose(probe.toWire(remote => {
      connection.model = remote;
      return { methods: { reverse: (params: Payload) => ({ ...params, text: 'typescript:' + params.text }) },
        events: { changed: data => dialled.changedEvent(data) } };
    }, {}));
    await connection.connect(String(args.url)).catch(error => { throw fail('failed', String(error)); });
    const handle = `cl${++next}`;
    dialled.connection = connection;
    handles.set(handle, dialled);
    return { handle };
  },
  'client.echo': args => typed(async () => dialledOf(args).client.methods.echo(args.params as Payload)),
  'client.seen': args => typed(async () => dialledOf(args).client.methods.seen(args.params as Seen)),
  'client.no_args': args => typed(async () => dialledOf(args).client.methods.noArgs({})),
  'client.emit_noticed': async args => {
    await Promise.resolve(dialledOf(args).client.events.noticed(args.data as Seen)).catch(error => { throw fail('disconnected', String(error)); });
    return {};
  },
  'client.await_changed': async args => {
    const data = await dialledOf(args).awaitChanged(withinOf(args));
    if (data === undefined) throw fail('timeout', 'no changed event');
    return { data };
  },
  'client.close': args => { dialledOf(args).connection.close(); return {}; },
  'client.await_notification': async args => {
    const notification=await dialledOf(args).awaitNotification(withinOf(args));
    if(notification===undefined) throw fail('timeout','no typed notification');
    return notification;
  },
  'server.reverse': async args => {
    const signal = AbortSignal.timeout(withinOf(args));
    const remote = await remoteOf(args);
    return typed(async () => remote.methods.reverse(args.params as Payload, { signal }));
  },
  'server.emit_changed': async args => {
    const remote = await remoteOf(args);
    await Promise.resolve(remote.events.changed(args.data as Payload)).catch(error => { throw fail('disconnected', String(error)); });
    return {};
  },
  'server.await_noticed': async args => {
    const deadline = Date.now() + withinOf(args);
    await remoteOf(args);
    const data = await servedOf(args).noticed.take(Math.max(0, deadline - Date.now()));
    if (data === undefined) throw fail('timeout', 'no noticed event');
    return { data };
  },
  'gen.validate': args => {
    try {
      validateWire(JSON.parse(String(args.type)), args.value);
      return { valid: true };
    } catch (error) {
      return { valid: false, message: String(error) };
    }
  },
  'gen.errors': () => Object.values(errors).sort(),
  'gen.is_error': args => ({ value: Object.values(errors).includes(String(args.code) as never) }),
};

const serve = async (line: string): Promise<string> => {
  let request: Args;
  try {
    request = JSON.parse(line) as Args;
  } catch (error) {
    return JSON.stringify({ id: 0, error: fail('invalid', String(error)) });
  }
  const { id, op, ...args } = request;
  const handler = typeof op === 'string' ? ops[op] : undefined;
  if (!handler) return JSON.stringify({ id, error: fail('unsupported', `no such op: ${String(op)}`) });
  try {
    return JSON.stringify({ id, ok: (await handler(args)) ?? {} });
  } catch (error) {
    if (error instanceof Failure || error instanceof ProofFailure || error instanceof LiveFailure || error instanceof CombinatorFailure || error instanceof ForwardingFailure || error instanceof OwnerFailure || error instanceof PublicationFailure) return JSON.stringify({ id, error: {code:error.code,message:error.message} });
    return JSON.stringify({ id, error: fail('internal', error instanceof Error ? error.message : String(error)) });
  }
};

for await (const line of createInterface({ input: process.stdin, crlfDelay: Infinity })) {
  if (line.trim() === '') continue;
  const answer = await serve(line);
  await new Promise<void>(resolve => process.stdout.write(answer + '\n', () => resolve()));
  if (bye) break;
}
reset();
process.exit(0);
