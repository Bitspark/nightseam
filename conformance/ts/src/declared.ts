// Production adapter for independent Bitwire ADR0006 cases at fdc2ae99.
// The scenario harness is upstream; construction, parts and routing use Nightseam.
import { readFileSync } from 'node:fs';
import { once } from 'node:events';
import { isDeepStrictEqual } from 'node:util';
import { WebSocket, WebSocketServer } from 'ws';
import type { Endpoint, Message, Path, ReturnAddress, Wire } from '@bitspark/bitwire';
import { at, mount, Declared, webSocketConnection, type WebSocketLike } from '@nightseam/duplex';
import { wirePair, forwardWire, DuplexPeer } from '@nightseam/runtime';

/** A node's own value: behavior at its empty relative path. Refusal is a value. */
interface Origin extends Wire {
  readonly name: string;
  readonly instance: number;
}
const refuse: Origin = {
  name: '',
  instance: 0,
  send() {
    throw new Error('No destination at the origin');
  },
};
type Entry = readonly [string, Wire | undefined];

/** Constructs declared composites; parts are the construction owner's retained description. */
interface Realization {
  compose(own: Origin, entries: readonly Entry[]): Wire;
  parts(composite: Wire): [Origin, Entry[]] | undefined;
  expose(composite: Wire): Wire;
  teardown(): void;
}
// Descriptions belong to the assembler; access is never cast back to parts.
class Production implements Realization {
  private readonly retained = new Map<Wire, Declared>();
  compose(own: Origin, entries: readonly Entry[]): Wire {
    // The fixture deliberately supplies undefined to probe constructor refusal.
    const d = Declared.compose(own, entries as readonly (readonly [string, Wire])[]);
    const access = d.bind();
    this.retained.set(access, d);
    return access;
  }
  parts(w: Wire): [Origin, Entry[]] | undefined {
    const d = this.retained.get(w);
    if (!d) return undefined;
    const { origin, children } = d.decompose();
    return [origin as Origin, children];
  }
  expose(w: Wire): Wire {
    return w;
  }
  // Declared construction acquires no resource requiring teardown.
  teardown(): void {}
}

// ---- Instrumented child access and interception used by the fixtures ----
interface Forwarded {
  path: string[];
  count: number;
}
class Env {
  expected?: Message;
  readonly contexts = new Map<ReturnAddress, object>();
  readonly marker = {};
  unchanged = true;
  last?: Forwarded;
  readonly trace: unknown[] = [];
  private readonly instances = new Map<string, number>();
  readonly sender: Wire;
  constructor(sender: Wire) {
    this.sender = sender;
  }
  verify(message: Message): void {
    this.unchanged &&=
      !!this.expected &&
      isDeepStrictEqual(message.frame, this.expected.frame) &&
      message.return === this.expected.return &&
      !!message.return &&
      this.contexts.get(message.return) === this.marker;
  }
  next(name: string): number {
    const value = (this.instances.get(name) ?? 0) + 1;
    this.instances.set(name, value);
    return value;
  }
  newOrigin(name: string): Origin {
    let count = 0;
    return {
      name,
      instance: this.next(name),
      send: (path, message) => {
        if (path.length) throw new Error('Origin received a nonempty path');
        this.verify(message);
        this.last = { path: [name], count: ++count };
        at(this.sender, [name]).send([], message);
      },
    };
  }
  newAccess(name: string): Access {
    return new Access(name, this.next(name), this);
  }
}
/** Complete, stateful child access with its own instance counter. */
class Access implements Wire {
  count = 0;
  readonly name: string;
  readonly instance: number;
  private readonly env: Env;
  constructor(name: string, instance: number, env: Env) {
    this.name = name;
    this.instance = instance;
    this.env = env;
  }
  send(path: Path, message: Message): void {
    this.env.verify(message);
    this.env.last = { path: [this.name, ...path], count: ++this.count };
    at(this.env.sender, [this.name]).send(path, message);
  }
}
interface Policy {
  id: string;
  instance: number;
  limit: number;
  remaining: number;
}
/** Interception composed around access; it is not a node value. */
class Guard implements Wire {
  readonly policy: Policy;
  readonly inner: Wire;
  private readonly env: Env;
  constructor(policy: Policy, inner: Wire, env: Env) {
    this.policy = policy;
    this.inner = inner;
    this.env = env;
  }
  send(path: Path, message: Message): void {
    this.env.trace.push(['check', this.policy.id, [...path]]);
    if (this.policy.remaining === 0) throw new Error('Guard refused');
    if (this.policy.remaining > 0) this.policy.remaining--;
    this.inner.send(path, message);
  }
}

// ---- Fixture interpretation ----
interface Declaration {
  id: string;
  origin?: string | null;
  children?: [string, string][];
  access?: string;
}
interface Step {
  op: string;
  path?: string[];
  keep?: string[][];
  selections?: string[][];
  via?: string;
  key?: string;
  to?: string;
  node?: string;
  mode?: string;
  id?: string;
  origin?: string | null;
  limit?: number;
}
interface Case {
  id: string;
  kind?: string;
  root: string;
  fault?: string;
  steps?: Step[];
  relay?: boolean;
  mount?: boolean;
}
interface Fixture {
  declarations: Declaration[];
  cases: Case[];
}

const utf8 = new TextEncoder();
function byteOrder(a: string, b: string): number {
  const x = utf8.encode(a),
    y = utf8.encode(b);
  for (let i = 0; i < Math.min(x.length, y.length); i++) if (x[i] !== y[i]) return x[i]! - y[i]!;
  return x.length - y.length;
}
function sameEntries(got: readonly Entry[], want: readonly Entry[]): boolean {
  if (got.length !== want.length) return false;
  const index = new Map(want);
  for (const [key, child] of got) {
    if (!index.has(key) || index.get(key) !== child) return false;
    index.delete(key);
  }
  return index.size === 0;
}

class Harness {
  readonly env: Env;
  private readonly declarations = new Map<string, Declaration>();
  private readonly built = new Map<string, Wire>();
  private readonly origins = new Map<string, Origin>();
  root!: Wire;
  view?: Wire;
  partsExact = true;
  readonly R: Realization;
  constructor(R: Realization, fixture: Fixture, sender: Wire) {
    this.R = R;
    this.env = new Env(sender);
    for (const d of fixture.declarations) {
      if (this.declarations.has(d.id)) throw new Error('Duplicate declaration');
      this.declarations.set(d.id, d);
    }
  }
  /** Records R1 and mutates the caller's input afterward: the composite keeps its own copy. */
  construct(own: Origin, entries: readonly Entry[]): Wire {
    const input: Entry[] = [...entries];
    const w = this.R.compose(own, input);
    input.fill(['mutated', undefined]);
    let found = this.R.parts(w);
    let exact = !!found && found[0] === own && sameEntries(found[1], entries);
    if (exact && found![1].length) {
      found![1][0] = ['mutated', undefined]; // Returned parts are a copy of the retained description.
      found = this.R.parts(w);
      exact = !!found && found[0] === own && sameEntries(found[1], entries);
    }
    this.partsExact &&= exact;
    return w;
  }
  private origin(name: string): Origin {
    let found = this.origins.get(name);
    if (!found) this.origins.set(name, (found = this.env.newOrigin(name)));
    return found;
  }
  build(id: string, fault = '', rootID = '', visiting = new Set<string>()): Wire {
    const found = this.built.get(id);
    if (found) return found;
    const d = this.declarations.get(id);
    if (!d) throw new Error('Missing declaration');
    if (d.access) {
      const w = this.env.newAccess(d.access);
      this.built.set(id, w);
      return w;
    }
    if (visiting.has(id)) throw new Error('Cyclic declaration');
    visiting.add(id);
    const children = [...(d.children ?? [])];
    if (id === rootID && fault === 'cycle') children.push(['loop', rootID]);
    const entries: Entry[] = children.map(([key, child]) => [key, this.build(child, fault, rootID, visiting)]);
    if (id === rootID && fault === 'duplicate') entries.push(['a', this.build('leaf')]);
    if (id === rootID && fault === 'invalidKey') entries.push(['\ud800', this.build('leaf')]);
    if (id === rootID && fault === 'missingChild') entries.push(['hole', undefined]);
    const w = this.construct(d.origin == null ? refuse : this.origin(d.origin), entries);
    visiting.delete(id);
    this.built.set(id, w);
    return w;
  }
  caller(): Wire {
    return this.root instanceof Guard ? this.root : this.R.expose(this.root);
  }
  render(w: Wire): unknown {
    const found = this.R.parts(w);
    if (found) {
      const [own, entries] = found;
      return {
        origin: own === refuse ? null : [own.name, own.instance],
        children: entries.sort(([a], [b]) => byteOrder(a, b)).map(([key, child]) => [key, this.render(child!)]),
      };
    }
    if (w instanceof Access) return { access: [w.name, w.instance] };
    if (w instanceof Guard) return { guard: [w.policy.id, w.policy.instance], inner: this.render(w.inner) };
    return 'opaque';
  }
  structure(w: Wire, path: readonly string[]): unknown {
    if (!path.length) return this.render(w);
    const found = this.R.parts(w);
    if (!found) throw new Error('Structure path leaves the declared composites');
    const child = found[1].find(([key]) => key === path[0]);
    return child ? this.structure(child[1]!, path.slice(1)) : 'missing';
  }
  /** Rebuilds declared ancestors from retained parts; a guard keeps its policy instance. */
  replaceAt(w: Wire, path: readonly string[], f: (w: Wire) => Wire): Wire {
    if (w instanceof Guard && path.length) return new Guard(w.policy, this.replaceAt(w.inner, path, f), this.env);
    if (!path.length) return f(w);
    const found = this.R.parts(w);
    if (!found) throw new Error('Edit path leaves the declared composites');
    return this.construct(
      found[0],
      found[1].map(([key, child]) => [key, key === path[0] ? this.replaceAt(child!, path.slice(1), f) : child]),
    );
  }
  /** One complete cut: kept subtrees are reused whole; other declared composites are rebuilt from parts. */
  rebuild(w: Wire, where: readonly string[], keep: readonly (readonly string[])[]): Wire {
    if (keep.some((path) => isDeepStrictEqual([...path], [...where]))) return w;
    if (w instanceof Guard) return new Guard(w.policy, this.rebuild(w.inner, where, keep), this.env);
    const found = this.R.parts(w);
    if (!found) return w; // Opaque child access is retained whole.
    return this.construct(
      found[0],
      found[1].map(([key, child]) => [key, this.rebuild(child!, [...where, key], keep)]),
    );
  }
  copy(w: Wire): Wire {
    if (w instanceof Access) return this.env.newAccess(w.name);
    const found = this.R.parts(w);
    if (!found) throw new Error('Cannot copy opaque access');
    const own = found[0] === refuse ? refuse : this.env.newOrigin(found[0].name);
    return this.construct(
      own,
      found[1].map(([key, child]) => [key, this.copy(child!)]),
    );
  }
}

function mailbox<T>() {
  const values: T[] = [];
  let pending: ((value: T) => void) | undefined;
  return {
    put(value: T) {
      if (pending) {
        const resolve = pending;
        pending = undefined;
        resolve(value);
      } else values.push(value);
    },
    async take(): Promise<T> {
      if (values.length) return values.shift()!;
      if (pending) throw new Error('Concurrent mailbox reads');
      return new Promise<T>((resolve, reject) => {
        const timer = setTimeout(() => {
          pending = undefined;
          reject(new Error('Delivery deadline exceeded'));
        }, 5000);
        pending = (value) => {
          clearTimeout(timer);
          resolve(value);
        };
      });
    },
  };
}
type Deliveries = ReturnType<typeof mailbox<{ path: Path; message: Message }>>;
class Scope {
  cleanups: (() => void)[] = [];
  close(): void {
    for (const cleanup of this.cleanups.reverse()) cleanup();
  }
  async pair(): Promise<[Endpoint, Endpoint]> {
    if (process.env.NIGHTSEAM_BITWIRE_CARRIER === 'local') {
      const pair = wirePair();
      this.cleanups.push(() => {
        pair[0].close();
        pair[1].close();
      });
      return pair;
    }
    const listener = new WebSocketServer({ port: 0, host: '127.0.0.1' });
    this.cleanups.push(() => {
      listener.close();
    });
    await once(listener, 'listening', { signal: AbortSignal.timeout(5000) });
    const address = listener.address();
    if (!address || typeof address === 'string') throw new Error('No WebSocket address');
    const accepted = once(listener, 'connection', { signal: AbortSignal.timeout(5000) });
    accepted.catch(() => {});
    listener.on('connection', (socket) => this.cleanups.push(() => socket.terminate()));
    const client = new WebSocket(`ws://127.0.0.1:${address.port}`);
    this.cleanups.push(() => client.terminate());
    await once(client, 'open', { signal: AbortSignal.timeout(5000) });
    const [remote] = (await accepted) as [WebSocket];
    const a = new DuplexPeer({ role: 'client' }),
      b = new DuplexPeer({ role: 'server' });
    this.cleanups.push(() => {
      a.close();
      b.close();
    });
    const asLike = (socket: WebSocket): WebSocketLike => {
      socket.binaryType = 'arraybuffer';
      return socket as unknown as WebSocketLike;
    };
    await Promise.all([a.attach(webSocketConnection(asLike(client))), b.attach(webSocketConnection(asLike(remote)))]);
    return process.env.NIGHTSEAM_BITWIRE_REVERSE === '1' ? [b.wire(), a.wire()] : [a.wire(), b.wire()];
  }
  async carriers(test: Case): Promise<[Endpoint, Wire, Endpoint]> {
    const [source, first] = await this.pair();
    let sender: Wire = source,
      receiver = first;
    if (test.mount) {
      const mounted = mount(new Map([['mounted', source]]));
      sender = at(mounted, ['mounted']);
      this.cleanups.push(() => mounted.close());
    }
    if (test.relay) {
      const [outgoing, target] = await this.pair();
      this.cleanups.push(forwardWire(receiver, outgoing));
      receiver = target;
    }
    return [source, sender, receiver];
  }
}
const event = (value: string): Message => ({ frame: { version: 1, kind: 'event', data: value } });
const refuseWire: Wire = {
  send() {
    throw new Error('No destination');
  },
};

async function send(h: Harness, w: Wire, path: Path, message: Message, deliveries: Deliveries): Promise<void> {
  h.env.expected = message;
  h.env.last = undefined;
  try {
    w.send(path, message);
  } catch {
    if (h.env.last) throw new Error('A destination accepted a refused send');
    h.env.trace.push(['refused']);
    return;
  }
  const got = await deliveries.take();
  if (!isDeepStrictEqual(got.message.frame, message.frame)) throw new Error('Message changed or unexpected delivery');
  const last = h.env.last as Forwarded | undefined;
  if (!last || !isDeepStrictEqual([...got.path], last.path))
    throw new Error('Delivery does not match its declared destination');
  h.env.trace.push(['delivered', [...got.path], last.count]);
}
function marked(h: Harness, value: string): Message {
  const message: Message = { ...event(value), return: { wire: refuseWire } };
  h.env.contexts.set(message.return!, h.env.marker);
  return message;
}
async function borrowed(sender: Wire, deliveries: Deliveries): Promise<boolean> {
  try {
    sender.send(['borrowed'], event('borrowed'));
  } catch {
    return false;
  }
  const got = await deliveries.take();
  return (
    isDeepStrictEqual(got.path, ['borrowed']) &&
    got.message.frame.kind === 'event' &&
    got.message.frame.data === 'borrowed'
  );
}
function refusal(error: unknown): unknown {
  return { construction: 'refused' };
}

async function apply(h: Harness, test: Case, index: number, s: Step, deliveries: Deliveries): Promise<void> {
  const edit =
    (f: (own: Origin, entries: Entry[]) => Wire) =>
    (w: Wire): Wire => {
      const found = h.R.parts(w);
      if (!found) throw new Error('Edit of opaque access');
      return f(found[0], found[1]);
    };
  const policy = (id: string, limit: number): Policy => ({
    id,
    instance: h.env.next(`policy:${id}`),
    limit,
    remaining: limit,
  });
  switch (s.op) {
    case 'send':
    case 'invalidPath': {
      let w = h.caller();
      if (s.via === 'view') {
        if (!h.view) throw new Error('No captured view');
        w = h.view;
      }
      for (const prefix of s.selections ?? []) w = at(w, prefix);
      await send(h, w, s.op === 'invalidPath' ? ['\ud800'] : s.path!, marked(h, `${test.id}:${index}`), deliveries);
      break;
    }
    case 'direct':
      await send(h, h.build(s.node!), s.path!, marked(h, `${test.id}:${index}`), deliveries);
      break;
    case 'structure': {
      const root = h.root instanceof Guard && s.path!.length ? h.root.inner : h.root;
      h.env.trace.push(['structure', h.structure(root, s.path!)]);
      break;
    }
    case 'rebuild':
      h.root = h.rebuild(h.root, [], s.keep!);
      break;
    case 'origin':
      h.root = h.replaceAt(
        h.root,
        s.path!,
        edit((own, entries) => h.construct(s.origin == null ? refuse : h.env.newOrigin(own.name), entries)),
      );
      break;
    case 'substitute':
      h.root = h.replaceAt(h.root, s.path!, (w) => (s.mode === 'copy' ? h.copy(w) : h.rebuild(w, [], [])));
      break;
    case 'omit':
    case 'rename':
    case 'add':
      h.root = h.replaceAt(
        h.root,
        s.path!,
        edit((own, entries) => {
          const next: Entry[] = entries
            .filter(([key]) => !(key === s.key && s.op === 'omit'))
            .map(([key, child]) => [key === s.key && s.op === 'rename' ? s.to! : key, child]);
          if (s.op === 'add') next.push([s.key!, h.build(s.node!)]);
          return h.construct(own, next);
        }),
      );
      break;
    case 'replace':
      h.root = h.replaceAt(h.root, s.path!, () => h.build(s.node!));
      break;
    case 'view':
      h.view = h.caller();
      for (const prefix of s.selections ?? []) h.view = at(h.view, prefix);
      break;
    case 'guard':
      h.root = new Guard(policy(s.id!, s.limit!), h.root, h.env);
      break;
    case 'freshGuard': {
      const g = h.root as Guard;
      h.root = new Guard(policy(g.policy.id, g.policy.limit), g.inner, h.env);
      break;
    }
    case 'guardChild':
      h.root = h.replaceAt(h.root, [...s.path!, s.key!], (w) => new Guard(policy(s.id!, s.limit!), w, h.env));
      break;
    case 'rebuildFromViews': {
      const g = h.root as Guard;
      const found = h.R.parts(g.inner);
      if (!found) throw new Error('Guarded access is not a composite');
      h.root = new Guard(
        g.policy,
        h.construct(
          found[0],
          found[1].map(([key]) => [key, at(g, [key])]),
        ),
        h.env,
      );
      break;
    }
    case 'teardown':
      h.R.teardown();
      break;
    default:
      throw new Error('Unknown step: ' + s.op);
  }
}

async function observe(R: Realization, fixture: Fixture, test: Case): Promise<unknown> {
  const scope = new Scope();
  try {
    const [source, sender, receiver] = await scope.carriers(test);
    const deliveries: Deliveries = mailbox();
    scope.cleanups.push(
      receiver.receive({
        message(path, message) {
          deliveries.put({ path: [...path], message });
        },
      }),
    );
    const h = new Harness(R, fixture, sender);
    try {
      h.root = h.build(test.root, test.fault, test.root);
    } catch (error) {
      return refusal(error);
    }
    for (const [index, step] of (test.steps ?? []).entries()) await apply(h, test, index, step, deliveries);
    const caller = h.caller();
    const sendOnly = !('receive' in caller) && !('close' in caller);
    R.teardown();
    return {
      trace: h.env.trace,
      partsExact: h.partsExact,
      unchanged: h.env.unchanged,
      sendOnly,
      borrowedUsable: await borrowed(source, deliveries),
    };
  } finally {
    scope.close();
  }
}

/** Admits a real request, then rebuilds, rebinds and tears down before its late reply and captured cancel. */
async function pending(R: Realization, fixture: Fixture, test: Case): Promise<unknown> {
  const scope = new Scope();
  try {
    const [source, sender, receiver] = await scope.carriers(test);
    const captured = mailbox<Message>(),
      cancelled = mailbox<string>(),
      replies = mailbox<string>();
    const deliveries: Deliveries = mailbox();
    scope.cleanups.push(
      receiver.receive({
        message(path, message) {
          if (message.frame.kind === 'event') {
            deliveries.put({ path: [...path], message });
            return;
          }
          const lifecycle = message.return!.wire;
          lifecycle.send(['invocation.capture', 'old'], {
            frame: { version: 1, kind: 'event', data: null },
            return: {
              wire: {
                send(path, message) {
                  if (path.length || message.frame.kind !== 'cancel') throw new Error('Invalid captured control');
                  cancelled.put('old');
                },
              },
            },
          });
          lifecycle.send(['invocation.ready', 'old'], event('ready'));
          lifecycle.send(['invocation.begin', 'old'], event('begin'));
          captured.put(message);
        },
      }),
    );
    const h = new Harness(R, fixture, sender);
    try {
      h.root = h.build(test.root, '', test.root);
    } catch (error) {
      return refusal(error);
    }
    const original: ReturnAddress = {
      wire: {
        send(path, message) {
          if (
            path.length ||
            message.frame.kind !== 'response' ||
            message.frame.error ||
            typeof message.frame.result !== 'string'
          )
            throw new Error('Unexpected reply');
          replies.put(message.frame.result);
        },
      },
    };
    const request: Message = { frame: { version: 1, kind: 'request', id: 'c:1', params: null }, return: original };
    h.env.contexts.set(original, h.env.marker);
    h.env.expected = request;
    at(at(h.caller(), ['a']), ['b']).send([], request);
    const old = await captured.take();
    if (old.frame.kind !== 'request') throw new Error('Expected request');
    h.root = h.rebuild(h.root, [], []);
    for (const path of [['a', 'b'], ['alias']])
      await apply(h, test, 0, { op: 'replace', path, node: 'replacement' }, deliveries);
    await send(h, at(h.caller(), ['alias']), [], marked(h, 'new'), deliveries);
    R.teardown();
    old.return!.wire.send([], { frame: { version: 1, kind: 'response', id: old.frame.id, result: 'old' } });
    const lateReply = await replies.take();
    old.return!.wire.send(['invocation.control'], { frame: { version: 1, kind: 'cancel', id: old.frame.id } });
    const controls = [await cancelled.take()];
    old.return!.wire.send(['invocation.release', 'old'], event('release'));
    old.return!.wire.send(['invocation.done', 'old'], event('done'));
    return {
      trace: h.env.trace,
      lateReply,
      cancelled: controls,
      unchanged: h.env.unchanged,
      borrowedUsable: await borrowed(source, deliveries),
    };
  } finally {
    scope.close();
  }
}

if (!['local', 'peer'].includes(process.env.NIGHTSEAM_BITWIRE_CARRIER ?? ''))
  throw new Error('Expected local or peer carrier');
const [inputPath] = process.argv.slice(2);
if (!inputPath || process.argv.length !== 3) throw new Error('Usage: declared.ts inputs.json');
const fixture = JSON.parse(readFileSync(inputPath, 'utf8')) as Fixture;
const output = [];
for (const test of fixture.cases) {
  const R = new Production();
  output.push({
    id: test.id,
    observations: await (test.kind === 'pending' ? pending(R, fixture, test) : observe(R, fixture, test)),
  });
}
process.stdout.write(JSON.stringify(output) + '\n');
