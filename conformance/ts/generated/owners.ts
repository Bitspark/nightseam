/** Generated native values with explicit caller and handler lifetimes. */
import * as owners from './api/ts/owners-client/src/index.ts';
import * as worker from './api/ts/worker-client/src/index.ts';
import * as binding from './api/ts/owners-binding/src/index.ts';
import { Served, Session, adapterContext } from './server.ts';
import { DuplexError } from '@nightseam/runtime';
import { liveOver, LiveOwner, type LiveScope } from '@nightseam/live';

type Args = Record<string, unknown>;
export class OwnerFailure extends Error {
  readonly code: string;
  constructor(code: string, message: string) { super(message); this.code = code; }
}
interface Dialled { connection: Session<owners.Server>; scope: LiveScope; owner?: LiveOwner; job?: worker.Job; reports: number }
const handles = new Map<string, Dialled>();
type ServerMethods = owners.Server['methods'];
class OwnersServer implements ServerMethods {
  scope!: LiveScope;
  readonly held: LiveOwner[] = [];
  create: ServerMethods['create'] = async (params, context) => {
    const owner = context?.valueContext;
    if (!(owner instanceof LiveOwner) || owner.scope !== this.scope || owner === this.scope.owner()) throw new OwnerFailure('invalid', 'handler received no per-invocation child owner');
    this.held.push(owner);
    await params.progress.report(50, { signal: context?.signal, owner });
    return { items: [{ ticket: params.ticket.id, cancel: async () => {}, rename: async ticket => ticket }] };
  };
  pack: ServerMethods['pack'] = (params, context) => {
    const owner = context?.valueContext;
    if (!(owner instanceof LiveOwner) || owner.scope !== this.scope || owner === this.scope.owner()) throw new OwnerFailure('invalid', 'handler received no per-invocation child owner');
    this.held.push(owner);
    return { metadata: { seed: 7 }, run: async n => params.item(await params.item(n)) };
  };
  drop(): boolean { for (const owner of this.held.splice(0)) owner.release(); return true; }
  references(): unknown {
    const owner = this.scope.owner().child();
    this.held.push(owner);
    return Array.from({ length: 3 }, () => worker.exportReport(owner, async () => {}));
  }
}
const servers = new Map<string, { served: Served<Session<owners.Client>>; server: OwnersServer }>();
let next = 0;
const within = (args: Args) => Number(args.within_ms ?? 5000);
const lookup = (args: Args): Dialled => {
  const d = handles.get(String(args.on));
  if (!d) throw new OwnerFailure('unknown_handle', String(args.on));
  return d;
};
export function resetOwners(): void { for (const d of handles.values()) d.connection.close(); for (const s of servers.values()) s.served.shutdown(); handles.clear(); servers.clear(); }
async function zero(scope: LiveScope, timeout: number): Promise<void> {
  const deadline = Date.now() + timeout;
  while (scope.counts().exports !== 0 || scope.counts().imports !== 0) {
    if (Date.now() >= deadline) throw new OwnerFailure('timeout', 'counts did not return to baseline: ' + JSON.stringify(scope.counts()));
    await new Promise(resolve => setTimeout(resolve, 1));
  }
}
function code(error: unknown): string {
  if (error instanceof DuplexError) return error.code;
  throw error;
}

export const ownersOps: Record<string, (args: Args) => unknown | Promise<unknown>> = {
  'gen.owners_serve': async () => {
    const server = new OwnersServer();
    const served = await new Served(async socket => {
      const connection = new Session<owners.Client>({ role: 'server' });
      server.scope = liveOver(connection.peer, { maxExports: 4, maxImports: 4 });
      connection.expose(binding.toWire(remote => {
        connection.model = remote;
        return { methods: server, events: {} };
      }, adapterContext(server.scope)));
      return connection.attach(socket);
    }).listen();
    const handle = 'ownerssrv' + String(++next);
    servers.set(handle, { served, server });
    return { handle, url: served.url };
  },
  'gen.owners_counts': async args => {
    const s = servers.get(String(args.on));
    if (!s) throw new OwnerFailure('unknown_handle', String(args.on));
    if (!await s.served.remote(within(args))) throw new OwnerFailure('timeout', 'no owner client attached');
    await zero(s.server.scope, within(args));
    return s.server.scope.counts();
  },
  'gen.owners_dial': async args => {
    const connection = new Session<owners.Server>();
    const scope = liveOver(connection.peer, { maxExports: 4, maxImports: Number(args.max_imports ?? 4) });
    connection.expose(owners.toWire(remote => {
      connection.model = remote;
      return { methods: {}, events: {} };
    }, adapterContext(scope)));
    await connection.connect(String(args.url));
    const handle = 'ownerscl' + String(++next);
    handles.set(handle, { connection, scope, reports: 0 });
    return { handle };
  },
  'client.owners_create': async args => {
    const d = lookup(args);
    d.owner = d.scope.owner().child();
    const page = await d.connection.model.methods.create({ ticket: { id: 'owned', label: 'owned' }, progress: { report: async () => { d.reports++; } } }, { valueContext: d.owner });
    if (page.items.length !== 1) throw new OwnerFailure('invalid', 'page lost its job');
    d.job = page.items[0]!;
    await d.job.cancel();
    const renamed = await d.job.rename!({ id: 'owned', label: 'renamed' });
    return { label: renamed.label, reports: d.reports, counts: d.scope.counts() };
  },
  'client.owners_pack': async args => {
    const d = lookup(args);
    d.owner = d.scope.owner().child();
    const bundle = await d.connection.model.methods.pack({ item: async n => n + 3 }, { valueContext: d.owner });
    return { value: await bundle.run(5), seed: bundle.metadata.seed, counts: d.scope.counts() };
  },
  'client.owners_release': async args => {
    const d = lookup(args);
    if (!d.owner) throw new OwnerFailure('invalid', 'no owner');
    d.owner.release();
    d.owner.release();
    await d.connection.model.methods.drop({});
    await zero(d.scope, within(args));
    return d.scope.counts();
  },
  'client.owners_revoke': async args => {
    const d = lookup(args);
    await d.connection.model.methods.drop({});
    await zero(d.scope, within(args));
    try { await d.job!.cancel(); }
    catch (error) { return { error: { code: code(error) }, counts: d.scope.counts() }; }
    throw new OwnerFailure('invalid', 'handler release left returned function callable');
  },
  'client.owners_imports': async args => {
    const d = lookup(args);
    const result: Record<string, unknown> = {};
    for (const kind of ['fresh', 'borrowed']) {
      const refs = await d.connection.model.methods.references({}) as unknown[];
      const held = d.scope.owner().child();
      const retained = worker.importReport(held, refs[0]);
      const batch = d.scope.owner().child();
      try {
        if (kind === 'fresh') owners.importReportsUnchecked(batch, { first: refs[1], last: refs[2] });
        else owners.importRepeatedUnchecked(batch, refs);
        throw new OwnerFailure('invalid', 'partial import unexpectedly succeeded');
      } catch (error) {
        if (code(error) !== 'too_many_imports') throw error;
        result[kind] = code(error);
      }
      if (batch.counts().imports !== 0 || d.scope.counts().imports !== 1) throw new OwnerFailure('invalid', 'failed batch retained attachments');
      await retained(1);
      const aliases = owners.importRepeatedUnchecked(batch, [refs[0], refs[0]]);
      if (batch.counts().imports !== 0 || d.scope.counts().imports !== 1) throw new OwnerFailure('invalid', 'repeated alias created an attachment');
      batch.release();
      for (const alias of aliases) await alias(2);
      held.release();
      try { await aliases[0]!(3); throw new OwnerFailure('invalid', 'releasing original owner left borrowed alias callable'); }
      catch (error) { if (code(error) !== 'reference_released') throw error; }
      await d.connection.model.methods.drop({});
      await zero(d.scope, within(args));
    }
    return { ...result, repeated: true, counts: d.scope.counts() };
  },
};
