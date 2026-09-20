/** Ordinary generated callbacks survive uncertain publication and have explicit owners. */
import * as publication from './api/ts/publication-client/src/index.ts';
import * as binding from './api/ts/publication-binding/src/index.ts';
import { DuplexPeer, DuplexError, UnpublishedError, type CallOptions, type EmitOptions, type RequestContext, type EventContext } from '@nightseam/runtime';
import { liveOver, type LiveScope, type LiveOwner } from '@nightseam/live';
import { Served } from './server.ts';

type Args = Record<string, unknown>;
type OwnedCall = CallOptions & { owner?: LiveOwner };
type OwnedEmit = EmitOptions & { owner?: LiveOwner };
export class PublicationFailure extends Error {
  readonly code: string;
  constructor(code: string, message: string) { super(message); this.code = code; }
}
const check = (condition: boolean, message: string): void => { if (!condition) throw new PublicationFailure('invalid', message); };
function code(error: unknown): string {
  if (error === undefined) return 'success';
  if (error instanceof DuplexError) return error.code;
  throw error;
}
const delay = () => new Promise<void>(resolve => setTimeout(resolve, 1));
function aborted(signal: AbortSignal): Promise<void> {
  if (signal.aborted) return Promise.resolve();
  return new Promise(resolve => signal.addEventListener('abort', () => resolve(), { once: true }));
}

class Receiver {
  owner?: LiveOwner;
  callback?: publication.Callback;
  failure?: unknown;
  readonly scope: LiveScope;
  constructor(scope: LiveScope) { this.scope = scope; }
  retain(value: publication.Supply, context: (RequestContext | EventContext) & { owner: LiveOwner }): void {
    check(context.owner.scope === this.scope && context.owner !== this.scope.owner(), 'generated receiver did not supply a child owner');
    this.owner = context.owner;
    this.callback = value.callback;
  }
  async supply(value: publication.Supply, context: RequestContext & { owner: LiveOwner }): Promise<number> {
    this.retain(value, context);
    await value.callback(-1);
    if (['busy', 'cancelled', 'frame_too_large'].includes(value.mode)) throw new DuplexError(value.mode, 'retained before refusal');
    check(['timeout', 'cancel', 'lost_reply'].includes(value.mode), 'unknown supply mode ' + value.mode);
    await aborted(context.signal);
    if (value.mode === 'lost_reply') return 0;
    throw new DuplexError('cancelled', 'caller withdrew');
  }
  async produce(value: publication.Supply, context: RequestContext & { owner: LiveOwner }): Promise<publication.Callback> {
    this.retain(value, context);
    await value.callback(-1);
    await aborted(context.signal);
    // Conversion still runs after cancellation; the generated reply is suppressed.
    return async n => n + 2;
  }
  async event(value: publication.Supply, context: EventContext & { owner: LiveOwner }): Promise<void> {
    try { this.retain(value, context); await value.callback(-1); }
    catch (error) { this.failure = error; }
  }
  inspect(): publication.Counts {
    if (this.failure !== undefined) throw this.failure;
    const c = this.scope.counts();
    const o = this.owner?.counts() ?? { exports: 0, imports: 0 };
    return { ...c, owned_exports: o.exports, owned_imports: o.imports };
  }
  async invoke(): Promise<number> {
    check(this.callback !== undefined, 'no retained callback');
    return this.callback!(40);
  }
  drop(): boolean { this.owner?.release(); this.owner = undefined; return true; }
}

interface Endpoint {
  scope: LiveScope;
  supply(value: publication.Supply, options?: OwnedCall): Promise<number>;
  produce(value: publication.Supply, options?: OwnedCall): Promise<publication.Callback>;
  emit(value: publication.Supply, options?: OwnedEmit): Promise<void>;
  inspect(): Promise<publication.Counts>;
  invoke(): Promise<number>;
  drop(): Promise<boolean>;
  close(): void;
}
interface Handle { endpoint?: Endpoint; served?: Served<binding.Remote>; }
const handles = new Map<string, Handle>();
let next = 0;
const mint = (value: Handle): string => { const handle = 'publication' + String(++next); handles.set(handle, value); return handle; };
export function resetPublication(): void {
  for (const h of handles.values()) { h.endpoint?.close(); h.served?.shutdown(); }
  handles.clear();
}
async function awaitCounts(endpoint: Endpoint, exports: number, imports: number, deadline: number): Promise<void> {
  for (;;) {
    const counts = await endpoint.inspect();
    if (counts.exports === exports && counts.imports === imports && counts.owned_exports === exports && counts.owned_imports === imports) return;
    check(Date.now() < deadline, `counts never reached ${exports}/${imports}: ${JSON.stringify(counts)}`);
    await delay();
  }
}

async function exercise(endpoint: Endpoint, timeout: number): Promise<unknown> {
  const deadline = Date.now() + timeout;
  const completed: string[] = [];
  const modes = ['busy', 'busy', 'busy', 'busy', 'busy', 'busy', 'busy', 'cancelled', 'frame_too_large', 'timeout', 'cancel', 'lost_reply', 'event', 'returned_reply_lost', 'unpublished', 'event_unpublished'];
  for (const mode of modes) {
    const owner = endpoint.scope.owner().child();
    const controller = new AbortController();
    let entered = false;
    let markEntered!: () => void;
    const started = new Promise<void>(resolve => { markEntered = resolve; });
    const value: publication.Supply = { mode, callback: async n => { if (n === -1) { entered = true; markEntered(); } return n + 1; } };
    const options: OwnedCall = { owner, signal: controller.signal, timeoutMs: 1000 };
    let outcome: unknown;
    if (mode === 'unpublished') {
      controller.abort();
      try { await endpoint.supply(value, options); } catch (error) { outcome = error; }
    } else if (mode === 'event_unpublished') {
      value.mode = 'x'.repeat(2048);
      try { await endpoint.emit(value, { owner }); } catch (error) { outcome = error; }
    } else if (mode === 'event') {
      await endpoint.emit(value, { owner });
      await Promise.race([started, new Promise<void>((_, reject) => { const timer = setTimeout(() => reject(new PublicationFailure('timeout', 'event was not received')), 1000); void started.then(() => clearTimeout(timer)); })]);
    } else {
      const finished = (mode === 'returned_reply_lost' ? endpoint.produce(value, options) : endpoint.supply(value, options)).then(() => undefined, error => error as unknown);
      await Promise.race([started, finished]);
      check(entered, mode + ' settled before delivery barrier');
      if (mode === 'cancel' || mode === 'returned_reply_lost') controller.abort();
      outcome = await finished;
    }
    const want = mode === 'cancel' || mode === 'unpublished' || mode === 'returned_reply_lost' ? 'cancelled' : mode === 'timeout' || mode === 'lost_reply' ? 'request_timeout' : mode === 'event' ? 'success' : mode === 'event_unpublished' ? 'frame_too_large' : mode;
    const unpublished = mode === 'unpublished' || mode === 'event_unpublished';
    check((outcome instanceof UnpublishedError) === unpublished, mode + ': non-publication proof did not belong to this send attempt');
    if (mode !== 'event_unpublished') check(code(outcome) === want, `${mode}: got ${code(outcome)}, want ${want}`);
    if (mode === 'unpublished' || mode === 'event_unpublished') {
      const counts = owner.counts();
      check(counts.exports === 0 && counts.imports === 0, mode + ' retained unsent export');
      check(!entered, mode + ' reached receiver');
      await awaitCounts(endpoint, 0, 0, deadline);
    } else {
      const counts = owner.counts();
      check(counts.exports === 1 && counts.imports === 0, mode + ' lost caller-owned export');
      const exports = mode === 'returned_reply_lost' ? 1 : 0;
      await awaitCounts(endpoint, exports, 1, deadline);
      check(await endpoint.invoke() === 41, mode + ' retained callback refused after failed supply');
      owner.release();
      await awaitCounts(endpoint, exports, 0, deadline);
      let refusal: unknown;
      try { await endpoint.invoke(); } catch (error) { refusal = error; }
      check(code(refusal) === 'reference_released', mode + ' owner release left remote alias usable');
    }
    owner.release();
    check(await endpoint.drop(), mode + ' drop failed');
    await awaitCounts(endpoint, 0, 0, deadline);
    const counts = endpoint.scope.counts();
    check(counts.exports === 0 && counts.imports === 0, mode + ' leaked caller bindings');
    completed.push(mode);
  }
  return { completed, cycles: completed.length, counts: endpoint.scope.counts() };
}

export const publicationOps: Record<string, (args: Args) => unknown | Promise<unknown>> = {
  'gen.publication_serve': async () => {
    const handle: Handle = {};
    const served = await new Served(async socket => {
      const peer = new DuplexPeer({ role: 'server', maxFrameBytes: 1024 });
      const scope = liveOver(peer, { maxExports: 4, maxImports: 4 });
      const receiver = new Receiver(scope);
      const remote = binding.install(peer, {
        supply: (value, _remote, context) => receiver.supply(value, context),
        produce: (value, _remote, context) => receiver.produce(value, context),
        inspect: () => receiver.inspect(),
        invoke: () => receiver.invoke(),
        drop: () => receiver.drop(),
      }, { offeredBack: (value, context) => receiver.event(value, context) });
      await peer.attach(socket);
      handle.endpoint = { scope, supply: (v, o) => remote.supplyBack(v, o), produce: (v, o) => remote.produceBack(v, o), emit: (v, o) => remote.emitOffered(v, o), inspect: () => remote.inspectBack(), invoke: () => remote.invokeBack(), drop: () => remote.dropBack(), close: () => peer.close() };
      return remote;
    }).listen();
    handle.served = served;
    return { handle: mint(handle), url: served.url };
  },
  'gen.publication_dial': async args => {
    const peer = new DuplexPeer({ maxFrameBytes: 1024 });
    const scope = liveOver(peer, { maxExports: 4, maxImports: 4 });
    const receiver = new Receiver(scope);
    const client = new publication.Client(peer, {
      supplyBack: (value, context) => receiver.supply(value, context),
      produceBack: (value, context) => receiver.produce(value, context),
      inspectBack: () => receiver.inspect(),
      invokeBack: () => receiver.invoke(),
      dropBack: () => receiver.drop(),
    }, { offered: (value, context) => receiver.event(value, context) });
    await peer.connect(String(args.url));
    return { handle: mint({ endpoint: { scope, supply: (v, o) => client.supply(v, o), produce: (v, o) => client.produce(v, o), emit: (v, o) => client.emitOfferedBack(v, o), inspect: () => client.inspect(), invoke: () => client.invoke(), drop: () => client.drop(), close: () => peer.close() } }) };
  },
  'gen.publication_exercise': async args => {
    const handle = handles.get(String(args.on));
    if (!handle) throw new PublicationFailure('unknown_handle', String(args.on));
    const timeout = Number(args.within_ms ?? 5000);
    if (handle.served) check(await handle.served.remote(timeout) !== undefined, 'no connected publication client');
    check(handle.endpoint !== undefined, 'no publication endpoint');
    return exercise(handle.endpoint!, timeout);
  },
};
