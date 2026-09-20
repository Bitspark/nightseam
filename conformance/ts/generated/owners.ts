/** Generated native values with explicit caller and handler lifetimes. */
import * as owners from './api/ts/owners-client/src/index.ts';
import * as worker from './api/ts/worker-client/src/index.ts';
import { DuplexPeer, DuplexError } from '@nightseam/runtime';
import { liveOver, type LiveScope, type LiveOwner } from '@nightseam/live';

type Args = Record<string, unknown>;
export class OwnerFailure extends Error {
  readonly code: string;
  constructor(code: string, message: string) { super(message); this.code = code; }
}
interface Dialled { client: owners.Client; scope: LiveScope; owner?: LiveOwner; job?: worker.Job; reports: number }
const handles = new Map<string, Dialled>();
let next = 0;
const within = (args: Args) => Number(args.within_ms ?? 5000);
const lookup = (args: Args): Dialled => {
  const d = handles.get(String(args.on));
  if (!d) throw new OwnerFailure('unknown_handle', String(args.on));
  return d;
};
export function resetOwners(): void { for (const d of handles.values()) d.client.close(); handles.clear(); }
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
  'gen.owners_serve': () => { throw new OwnerFailure('unsupported', 'TypeScript binding supplied by the server-binding lane'); },
  'gen.owners_counts': () => { throw new OwnerFailure('unsupported', 'TypeScript binding supplied by the server-binding lane'); },
  'gen.owners_dial': async args => {
    const peer = new DuplexPeer();
    const scope = liveOver(peer, { maxExports: 4, maxImports: Number(args.max_imports ?? 4) });
    const client = new owners.Client(peer, undefined, {});
    await peer.connect(String(args.url));
    const handle = 'ownerscl' + String(++next);
    handles.set(handle, { client, scope, reports: 0 });
    return { handle };
  },
  'client.owners_create': async args => {
    const d = lookup(args);
    d.owner = d.scope.owner().child();
    const page = await d.client.create({ ticket: { id: 'owned', label: 'owned' }, progress: { report: async () => { d.reports++; } } }, { owner: d.owner });
    if (page.items.length !== 1) throw new OwnerFailure('invalid', 'page lost its job');
    d.job = page.items[0]!;
    await d.job.cancel();
    const renamed = await d.job.rename!({ id: 'owned', label: 'renamed' });
    return { label: renamed.label, reports: d.reports, counts: d.scope.counts() };
  },
  'client.owners_pack': async args => {
    const d = lookup(args);
    d.owner = d.scope.owner().child();
    const bundle = await d.client.pack({ item: async n => n + 3 }, { owner: d.owner });
    return { value: await bundle.run(5), seed: bundle.metadata.seed, counts: d.scope.counts() };
  },
  'client.owners_release': async args => {
    const d = lookup(args);
    if (!d.owner) throw new OwnerFailure('invalid', 'no owner');
    d.owner.release();
    d.owner.release();
    await d.client.drop();
    await zero(d.scope, within(args));
    return d.scope.counts();
  },
  'client.owners_revoke': async args => {
    const d = lookup(args);
    await d.client.drop();
    await zero(d.scope, within(args));
    try { await d.job!.cancel(); }
    catch (error) { return { error: { code: code(error) }, counts: d.scope.counts() }; }
    throw new OwnerFailure('invalid', 'handler release left returned function callable');
  },
  'client.owners_imports': async args => {
    const d = lookup(args);
    const result: Record<string, unknown> = {};
    for (const kind of ['fresh', 'borrowed']) {
      const refs = await d.client.references() as unknown[];
      const held = d.scope.owner().child();
      const retained = worker.importReport(held, refs[0]);
      const batch = d.scope.owner().child();
      try {
        if (kind === 'fresh') owners.importReports(batch, { first: refs[1], last: refs[2] });
        else owners.importRepeated(batch, refs);
        throw new OwnerFailure('invalid', 'partial import unexpectedly succeeded');
      } catch (error) {
        if (code(error) !== 'too_many_imports') throw error;
        result[kind] = code(error);
      }
      if (batch.counts().imports !== 0 || d.scope.counts().imports !== 1) throw new OwnerFailure('invalid', 'failed batch retained attachments');
      await retained(1);
      const aliases = owners.importRepeated(batch, [refs[0], refs[0]]);
      if (batch.counts().imports !== 0 || d.scope.counts().imports !== 1) throw new OwnerFailure('invalid', 'repeated alias created an attachment');
      batch.release();
      for (const alias of aliases) await alias(2);
      held.release();
      try { await aliases[0]!(3); throw new OwnerFailure('invalid', 'releasing original owner left borrowed alias callable'); }
      catch (error) { if (code(error) !== 'reference_released') throw error; }
      await d.client.drop();
      await zero(d.scope, within(args));
    }
    return { ...result, repeated: true, counts: d.scope.counts() };
  },
};
