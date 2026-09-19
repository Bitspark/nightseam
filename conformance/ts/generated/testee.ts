/**
 * The TypeScript generated testee: what the generator renders for the
 * probe family — its client — under the runner's control over the generated
 * ops of conformance/DRIVER.md. Laid beside the rendering by the runner,
 * under the checkout, so that @nightseam/runtime resolves through the
 * workspace and the runtime beneath the rendering is the real one.
 *
 * TypeScript renders a client and no binding, so gen.serve is unsupported
 * here and the runner skips what needs it; a scenario that dials a Go
 * binding, or that asks the rendering what it says, runs.
 */
import { proofOps, resetProof, ProofFailure } from './proof.ts';
import { createInterface } from 'node:readline';
import { Client, DuplexError, errors, validateWire, type Payload, type Seen } from './api/ts/probe-client/src/index.ts';

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
  client!: Client;
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
  shutdown(): void { this.client.close(); }
}

const handles = new Map<string, Dialled>();
let next = 0;
let bye = false;

const reset = () => {
  resetProof();
  for (const d of handles.values()) d.shutdown();
  handles.clear();
};

const withinOf = (args: Args) => typeof args.within_ms === 'number' ? args.within_ms : 5000;

const dialledOf = (args: Args): Dialled => {
  const d = typeof args.on === 'string' ? handles.get(args.on) : undefined;
  if (!d) throw fail('unknown_handle', String(args.on));
  return d;
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
  hello: () => ({ driver: 1, language: 'typescript', layers: ['generated'], features: [] }),
  reset: () => { reset(); return {}; },
  bye: () => { bye = true; reset(); return {}; },
  'gen.serve': () => { throw fail('unsupported', 'TypeScript renders a client and no binding'); },
  'gen.dial': async args => {
    const dialled = new Dialled();
    const client = await Client.dial(String(args.url), {}, {
      reverse: (params: Payload) => ({ ...params, text: 'typescript:' + params.text }),
    }, { changed: data => dialled.changedEvent(data) }).catch(error => { throw fail('failed', String(error)); });
    const handle = `cl${++next}`;
    dialled.client = client;
    handles.set(handle, dialled);
    return { handle };
  },
  'client.echo': args => typed(() => dialledOf(args).client.echo(args.params as Payload)),
  'client.seen': args => typed(() => dialledOf(args).client.seen(args.params as Seen)),
  'client.no_args': args => typed(() => dialledOf(args).client.noArgs()),
  'client.emit_noticed': async args => {
    await dialledOf(args).client.emitNoticed(args.data as Seen).catch(error => { throw fail('disconnected', String(error)); });
    return {};
  },
  'client.await_changed': async args => {
    const data = await dialledOf(args).awaitChanged(withinOf(args));
    if (data === undefined) throw fail('timeout', 'no changed event');
    return { data };
  },
  'client.close': args => { dialledOf(args).client.close(); return {}; },
  'client.await_notification': async args => {
    const notification=await dialledOf(args).awaitNotification(withinOf(args));
    if(notification===undefined) throw fail('timeout','no typed notification');
    return notification;
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
    if (error instanceof Failure || error instanceof ProofFailure) return JSON.stringify({ id, error: {code:error.code,message:error.message} });
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
