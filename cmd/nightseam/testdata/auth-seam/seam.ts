// The TypeScript leg of AUTH-SEAM: a client over a real socket against the
// Go server. It runs AUTH-SEAM-001, -002 and -005 from the other language,
// and guards what it supplies — a sink, a reverse method — with its own
// synthetic context, installed the way a consumer installs one here: on
// the context every handler of the connection runs with.
import assert from 'node:assert/strict';
import * as binding from '@example/worker-binding';
import type { Client, Job, Progress, ProgressSink } from '@example/worker-client/types';
import { DuplexError, DuplexPeer, callWire, defaultPropagator, type Propagator, type WireModelContext } from '@nightseam/runtime';
import { liveOver, valueEnvironment } from '@nightseam/live';

const url = process.argv[2]!;
const options = () => ({ signal: AbortSignal.timeout(8000) });

// ---- the client's synthetic context -----------------------------------------------

// The client trusts the server it dialed: "server" reads projects. A
// propagator is where a TypeScript consumer places the connection's context
// on every handler's context; nothing in the runtime interprets it.
const gate: unique symbol = Symbol('connection context');
type Context = WireModelContext & { readonly [gate]?: { subject: string } };
const authority: Record<string, { actions: string[]; scope: string }[]> = {
  server: [{ actions: ['read'], scope: 'projects/7' }],
};
function fixed(subject: string): Propagator {
  return {
    extract(context, trace) { defaultPropagator.extract(context, trace); Object.defineProperty(context, gate, { value: { subject } }); },
    inject: defaultPropagator.inject,
  };
}
function call(context: Context | undefined, action: string, scope: string): void {
  const g = context?.[gate];
  if (!g) throw new DuplexError('auth.unauthenticated', 'no context on this connection');
  for (const grant of authority[g.subject] ?? []) {
    if (grant.actions.includes(action) && (grant.scope === scope || scope.startsWith(grant.scope + '/'))) return;
  }
  throw new DuplexError('auth.denied', action + ' on ' + scope);
}

async function refused(what: string, run: () => Promise<unknown>, code: string): Promise<void> {
  let error: unknown;
  try { await run(); } catch (e) { error = e; }
  assert(error instanceof DuplexError, what + ': no refusal');
  assert.equal(error.code, code, what + ': ' + error.code + ' (' + error.message + ')');
}

// ---- the connection ----------------------------------------------------------------------

const received: Progress[] = [];
let notified = 0;
const clientSide: Client = {
  methods: {
    // A reverse call: the server, an explicit caller, meets this side's
    // guard under this side's context.
    notify(params, context) { call(context, 'read', 'projects/' + params.projectId); notified++; return true; },
  },
  events: { progress(data) { received.push(data); } },
};

let scope!: ReturnType<typeof liveOver>;
const peer = new DuplexPeer({ propagator: fixed('server'), prepare: p => { scope = liveOver(p, { maxImports: 32, maxExports: 64 }); } });
await peer.connect(url);
const wire = peer.wire();
const prove = (subject: string) => callWire(wire, ['auth', 'prove'], { subject }, options());
const model = await binding.fromWire(wire, { valueEnvironment: valueEnvironment(scope) });
const access = model(clientSide);

// The step a failure is reported against, for the Go driver's output.
let step = 'connect';
try {
  // AUTH-SEAM-001: the first requests meet the guard before any exchange.
  step = 'AUTH-SEAM-001 before establishment';
  await refused('protected method before establishment', () => Promise.resolve(access.methods.list({ projectId: '7' }, options())), 'auth.unauthenticated');
  await refused('protected live method before establishment', () => Promise.resolve(access.methods.start({ projectId: '7' }, { ...options(), valueContext: scope.owner() })), 'auth.unauthenticated');
  assert.equal(await access.methods.whoami({}, options()), '', 'a public member sees no subject');
  step = 'AUTH-SEAM-001 establishment';
  await prove('bob');
  await refused('a context is immutable', () => prove('alice'), 'auth.established');
  assert.equal(await access.methods.whoami({}, options()), 'bob');

  step = 'AUTH-SEAM-002 the same policy over the socket';
  assert.deepEqual(await access.methods.list({ projectId: '7' }, options()), { count: 1 });
  await refused('sibling project', () => Promise.resolve(access.methods.list({ projectId: '8' }, options())), 'auth.denied');
  await refused('a prefix is not a subtree', () => Promise.resolve(access.methods.list({ projectId: '70' }, options())), 'auth.denied');
  await refused('a separator in a selector', () => Promise.resolve(access.methods.list({ projectId: '7/ledger' }, options())), 'auth.selector_invalid');

  // AUTH-SEAM-003: the returned callables carry their guard; a supplied
  // sink is this side's exposure, decided here under this side's context.
  step = 'AUTH-SEAM-003 returned callables';
  const owner = scope.owner().child();
  const job: Job = await access.methods.start({ projectId: '7' }, { ...options(), valueContext: owner });
  assert.equal(await job.status('j1', options()), 'running:j1');
  assert.equal(await job.cancel('j1', options()), true);
  step = 'AUTH-SEAM-003 a supplied sink decides on the delivered context';
  let reported = 0;
  const sink: ProgressSink = async (p, context) => { call(context as Context, 'read', 'projects/' + p.projectId); reported++; return true; };
  assert.equal(await access.methods.subscribe({ projectId: '7', sink }, { ...options(), valueContext: owner }), true);
  assert.equal(reported, 1, 'the supplied sink ran under the server\'s call');

  // AUTH-SEAM-004: an emission is decided per recipient before it leaves.
  step = 'AUTH-SEAM-004 emissions';
  assert.deepEqual(await peer.call('test.emit', { projectId: '7', percent: 50 }, options()), { delivered: 1, dropped: 0 });
  const deadline = Date.now() + 5000;
  while (received.length < 1) { assert(Date.now() < deadline, 'emission not received'); await new Promise(r => setTimeout(r, 2)); }
  assert.deepEqual(received, [{ projectId: '7', percent: 50 }]);
  assert.deepEqual(await peer.call('test.emit', { projectId: '8', percent: 50 }, options()), { delivered: 0, dropped: 1 });

  // AUTH-SEAM-005: expiry at use, with the socket open and the retained
  // reference still resolvable; release and close are untouched.
  step = 'AUTH-SEAM-005 expiry at use';
  await peer.call('test.clock', 2000, options());
  await refused('expired at use', () => Promise.resolve(access.methods.list({ projectId: '7' }, options())), 'auth.denied');
  await refused('a retained callable after expiry', () => job.status('j1', options()), 'auth.denied');
  assert.deepEqual(await peer.call('test.emit', { projectId: '7', percent: 60 }, options()), { delivered: 0, dropped: 1 });
  await peer.call('test.clock', 1000, options());
  assert.equal(await job.status('j2', options()), 'running:j2', 'authority returned with the clock');
  owner.release();
  const drained = Date.now() + 5000;
  while (scope.counts().exports || scope.counts().imports) { assert(Date.now() < drained, 'scope did not drain: ' + JSON.stringify(scope.counts())); await new Promise(r => setTimeout(r, 2)); }
  assert.equal(notified, 0);
  console.log('AUTH-SEAM typescript: established, decided, emitted once, expired at use, drained');
} catch (error) {
  console.error('AUTH-SEAM typescript failed at: ' + step);
  throw error;
} finally {
  peer.close();
}
