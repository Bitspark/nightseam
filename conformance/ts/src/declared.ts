// Production adapter for independent Bitwire ADR0005 cases at 671b61d.
// The scenario vocabulary is upstream; all routing and structural operations use Declared.
import { readFileSync } from 'node:fs';
import { once } from 'node:events';
import { isDeepStrictEqual } from 'node:util';
import { WebSocket, WebSocketServer } from 'ws';
import type { Endpoint, Message, Path, ReturnAddress, Wire } from '@bitspark/bitwire';
import {
  at,
  mount,
  Declared,
  permitAdmission,
  webSocketConnection,
  type AdmissionPolicy,
  type WebSocketLike,
} from '@nightseam/duplex';
import { wirePair, forwardWire, DuplexPeer } from '@nightseam/runtime';

type Check = [string, string[]];
interface Policy extends AdmissionPolicy {
  id: string;
  remaining: number;
  checks: Check[];
}
function makePolicy(id: string, remaining: number, checks: Check[]): Policy {
  return {
    id,
    remaining,
    checks,
    admit(path) {
      this.checks.push([this.id, [...path]]);
      if (this.remaining === 0) throw new Error('Policy refused');
      if (this.remaining > 0) this.remaining--;
    },
  };
}
type Declaration = Declared;
type Parts = [Wire, Policy | undefined, [string, Declaration][]];
const refuse: Wire = {
  send() {
    throw new Error('No origin');
  },
};
function compose(
  own: Wire,
  policy: Policy | undefined,
  entries: readonly (readonly [string, Declaration])[],
): Declaration {
  return Declared.compose({ own, policy: policy ?? permitAdmission }, entries);
}
function parts(d: Declaration): Parts {
  const { value, children } = d.decompose();
  return [
    value.own,
    value.policy === permitAdmission ? undefined : (value.policy as Policy),
    children.map(([k, v]) => [k, v]),
  ];
}
function ownOf(d: Declaration): Wire {
  return d.decompose().value.own;
}
function policyOf(d: Declaration): Policy | undefined {
  return parts(d)[1];
}
function rebuild(d: Declaration, deep: boolean, memo = new Map<Declaration, Declaration>()): Declaration {
  const found = memo.get(d);
  if (found) return found;
  const [own, policy, children] = parts(d);
  const next = compose(
    own,
    policy,
    deep ? children.map(([k, child]) => [k, rebuild(child, true, memo)] as const) : children,
  );
  memo.set(d, next);
  return next;
}
function retained(d: Declaration): boolean {
  const { value, children } = d.decompose();
  const other = Declared.compose(value, children).decompose();
  const same =
    value.own === other.value.own &&
    value.policy === other.value.policy &&
    children.length === other.children.length &&
    children.every(([k, v], i) => k === other.children[i]![0] && v === other.children[i]![1]);
  children.length = 0;
  return same && d.decompose().children.length === other.children.length;
}
function bind(d: Declaration): Wire {
  return d.bind();
}
interface NodeSpec {
  id: string;
  own: string | null;
  policy?: string;
  children: [string, string][];
}
interface Step {
  op: string;
  path?: string[];
  selections?: string[][];
  raw?: string;
  cut?: string;
  kind?: string;
  useView?: boolean;
}
interface Case {
  id: string;
  kind?: string;
  fault?: string;
  limits: Record<string, number>;
  steps: Step[];
  relay?: boolean;
  mount?: boolean;
}
function create(
  specs: NodeSpec[],
  test: Case,
  own: (name: string) => Wire,
  checks: Check[],
): [Declaration, Map<string, Declaration>] {
  const definitions = new Map<string, NodeSpec>();
  for (const spec of specs) {
    if (definitions.has(spec.id)) throw new Error('Duplicate node');
    definitions.set(spec.id, spec);
  }
  if (test.fault === 'cycle') {
    const root = definitions.get('root')!;
    definitions.set('root', { ...root, children: [...root.children, ['loop', 'root']] });
  }
  const gates = new Map(Object.entries(test.limits).map(([id, remaining]) => [id, makePolicy(id, remaining, checks)]));
  const nodes = new Map<string, Declaration>();
  const visiting = new Set<string>();
  function build(id: string): Declaration {
    if (visiting.has(id)) throw new Error('Cyclic declaration');
    const found = nodes.get(id);
    if (found) return found;
    const spec = definitions.get(id);
    if (!spec) throw new Error('Missing declaration');
    visiting.add(id);
    const entries: [string, Declaration][] = spec.children.map(([k, v]) => [k, build(v)]);
    if (id === 'root' && test.fault === 'duplicate') entries.push(entries[0]!);
    if (id === 'root' && test.fault === 'invalidKey') {
      // Deixis byte keys must be in the exact UTF-8 image. Never decode with replacement.
      const key = new TextDecoder('utf-8', { fatal: true }).decode(new Uint8Array([255]));
      entries.push([key, nodes.get('leaf')!]);
    }
    if (spec.policy && !gates.has(spec.policy)) throw new Error('Missing policy');
    const next = compose(
      spec.own === null ? refuse : own(spec.own),
      spec.policy ? gates.get(spec.policy) : undefined,
      entries,
    );
    visiting.delete(id);
    nodes.set(id, next);
    return next;
  }
  return [build('root'), nodes];
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
}
const event = (value: string): Message => ({ frame: { version: 1, kind: 'event', data: value } });
interface Delivery {
  path: Path;
  message: Message;
  count: number;
}
async function borrowed(sender: Wire, deliveries: ReturnType<typeof mailbox<Delivery>>): Promise<boolean> {
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
async function observe(specs: NodeSpec[], test: Case): Promise<unknown> {
  const scope = new Scope();
  try {
    const [source, firstReceiver] = await scope.pair();
    let sender: Wire = source;
    const mounted = test.mount ? mount(new Map([['mounted', source]])) : undefined;
    if (mounted) {
      sender = at(mounted, ['mounted']);
      scope.cleanups.push(() => mounted.close());
    }
    let receiver = firstReceiver;
    if (test.relay) {
      const [outgoing, target] = await scope.pair();
      scope.cleanups.push(forwardWire(receiver, outgoing));
      receiver = target;
    }
    const deliveries = mailbox<Delivery>(),
      counts = new Map<string, number>();
    scope.cleanups.push(
      receiver.receive({
        message(path, message) {
          if (path.length !== 1) throw new Error('Unexpected destination path');
          const name = path[0]!,
            count = (counts.get(name) ?? 0) + 1;
          counts.set(name, count);
          deliveries.put({ path: [...path], message, count });
        },
      }),
    );
    const checks: Check[] = [];
    let unchanged = true,
      expected: Message;
    const marker = {},
      contexts = new Map<ReturnAddress, object>(),
      origins = new Map<string, Wire>();
    const own = (name: string): Wire => {
      const existing = origins.get(name);
      if (existing) return existing;
      const origin: Wire = {
        send(path, message) {
          unchanged &&=
            path.length === 0 &&
            isDeepStrictEqual(message.frame, expected.frame) &&
            message.return === expected.return &&
            !!message.return &&
            contexts.get(message.return) === marker;
          at(sender, [name]).send(path, message);
        },
      };
      origins.set(name, origin);
      return origin;
    };
    let root: Declaration, nodes: Map<string, Declaration>;
    try {
      [root, nodes] = create(specs, test, own, checks);
    } catch {
      return { construction: 'refused' };
    }
    let partsRetained = retained(root);
    let selected: Wire | undefined;
    const outcomes: string[] = [],
      observed: [string, number][] = [];
    for (const [index, action] of test.steps.entries()) {
      switch (action.op) {
        case 'captureView':
          selected = bind(root);
          for (const prefix of action.selections ?? []) selected = at(selected, prefix);
          break;
        case 'rebind': {
          const [origin, policy, children] = parts(root);
          root = compose(
            origin,
            policy,
            children.map(([k, v]) => [
              k,
              k === 'a' ? compose(ownOf(v), policyOf(v), [['b', compose(own('replacement'), undefined, [])]]) : v,
            ]),
          );
          break;
        }
        case 'rebuild':
          root = rebuild(root, action.cut === 'all');
          partsRetained &&= retained(root);
          break;
        case 'substitute': {
          const [origin, policy, children] = parts(root);
          root = compose(
            origin,
            policy,
            children.map(([k, v]) => [k, k === 'a' ? rebuild(v, true) : v]),
          );
          break;
        }
        case 'boundChild': {
          const selected = at(bind(root), ['a', 'b']);
          root = compose(ownOf(root), policyOf(root), [['a', compose(selected, undefined, [])]]);
          break;
        }
        case 'resetPolicy': {
          const [origin, , children] = parts(root);
          root = compose(origin, makePolicy('outer', test.limits.outer!, checks), children);
          break;
        }
        case 'sharePolicy': {
          const [origin, policy, children] = parts(root);
          root = compose(
            origin,
            policy,
            children.map(([k, v]) => [k, k === 'a' ? compose(ownOf(v), policy, v.decompose().children) : v]),
          );
          break;
        }
        case 'dropOwn':
          root = compose(refuse, policyOf(root), root.decompose().children);
          break;
        case 'omit':
        case 'rename':
        case 'extra': {
          const [origin, policy, children] = parts(root);
          const next: [string, Declaration][] = children
            .filter(([k]) => !(k === 'a' && action.op === 'omit'))
            .map(([k, v]) => [k === 'a' && action.op === 'rename' ? 'renamed' : k, v]);
          if (action.op === 'extra') next.push(['extra', nodes.get('leaf')!]);
          root = compose(origin, policy, next);
          break;
        }
        case 'send':
        case 'invalidPath':
        case 'control': {
          let access = bind(action.raw ? nodes.get(action.raw)! : root);
          if (action.useView) {
            if (!selected) throw new Error('No captured view');
            access = selected;
          }
          for (const prefix of action.selections ?? []) access = at(access, prefix);
          expected = { ...event(`${test.id}:${index}`), return: { wire: refuse } };
          contexts.set(expected.return!, marker);
          if (action.op === 'control')
            expected = {
              frame:
                action.kind === 'cancel'
                  ? { version: 1, kind: 'cancel', id: 'c:1' }
                  : { version: 1, kind: 'response', id: 'c:1', result: null },
              return: expected.return,
            };
          const path = action.op === 'invalidPath' ? [String.fromCharCode(0xd800)] : action.path!;
          let admitted = true;
          try {
            access.send(path, expected);
          } catch {
            admitted = false;
          }
          outcomes.push(admitted ? 'admitted' : 'refused');
          if (admitted) {
            const got = await deliveries.take();
            if (
              got.message.frame.kind !== 'event' ||
              expected.frame.kind !== 'event' ||
              got.message.frame.data !== expected.frame.data
            ) {
              throw new Error('Message changed or unexpected delivery');
            }
            observed.push([got.path[0]!, got.count]);
          }
          break;
        }
        default:
          throw new Error('Unknown step: ' + action.op);
      }
    }
    const sendOnly = Object.keys(bind(root)).join(',') === 'send';
    mounted?.close();
    return {
      outcomes,
      checks,
      deliveries: observed,
      unchanged,
      sendOnly,
      partsRetained,
      borrowedUsable: await borrowed(source, deliveries),
    };
  } finally {
    scope.close();
  }
}

async function pending(specs: NodeSpec[], test: Case): Promise<unknown> {
  const scope = new Scope();
  try {
    const [sender, receiver] = await scope.pair();
    const captured = mailbox<Message>(),
      cancelled = mailbox<string>(),
      deliveries = mailbox<Delivery>(),
      replies = mailbox<string>();
    scope.cleanups.push(
      receiver.receive({
        message(path, message) {
          if (message.frame.kind === 'event') {
            deliveries.put({ path, message, count: 1 });
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
    const checks: Check[] = [];
    let preserved = true;
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
    let [root] = create(
      specs,
      test,
      (name) => ({
        send(path, message) {
          preserved &&= message.return === original && path.length === 0;
          at(sender, [name]).send(path, message);
        },
      }),
      checks,
    );
    at(at(bind(root), ['a']), ['b']).send([], {
      frame: { version: 1, kind: 'request', id: 'c:1', params: null },
      return: original,
    });
    const old = await captured.take();
    if (old.frame.kind !== 'request') throw new Error('Expected request');
    root = rebuild(root, true);
    const replacement = compose(at(sender, ['new']), undefined, []);
    root = compose(ownOf(root), policyOf(root), [
      ['alias', replacement],
      ['a', replacement],
    ]);
    let newCallRefused = false;
    try {
      bind(root).send(['alias'], event('new'));
    } catch {
      newCallRefused = true;
    }
    old.return!.wire.send([], { frame: { version: 1, kind: 'response', id: old.frame.id, result: 'old' } });
    const lateReply = await replies.take();
    old.return!.wire.send(['invocation.control'], { frame: { version: 1, kind: 'cancel', id: old.frame.id } });
    const controls = [await cancelled.take()];
    old.return!.wire.send(['invocation.release', 'old'], event('release'));
    old.return!.wire.send(['invocation.done', 'old'], event('done'));
    return {
      checks,
      newCallRefused,
      lateReply,
      cancelled: controls,
      returnPreserved: preserved,
      borrowedUsable: await borrowed(sender, deliveries),
    };
  } finally {
    scope.close();
  }
}
if (!['local', 'peer'].includes(process.env.NIGHTSEAM_BITWIRE_CARRIER ?? ''))
  throw new Error('Expected local or peer carrier');
const fixture = JSON.parse(readFileSync(process.argv[2]!, 'utf8')) as { nodes: NodeSpec[]; cases: Case[] };
const output = [];
for (const test of fixture.cases)
  output.push({
    id: test.id,
    observations: await (test.kind === 'pending' ? pending(fixture.nodes, test) : observe(fixture.nodes, test)),
  });
process.stdout.write(JSON.stringify(output) + '\n');
