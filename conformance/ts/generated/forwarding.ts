/** Generated conversion between two real connections, A-B and B-C.
 * This testee runs both generated endpoint bindings and the intermediary.
 * The test transport's retain route imports generated Toolkit.
 */
import * as combinator from './api/ts/combinator-client/src/index.ts';
import * as binding from './api/ts/combinator-binding/src/index.ts';
import { liveOver, type LiveScope } from '@nightseam/live';
import { callWire, handleWire } from '@nightseam/runtime';
import { CombinatorServer } from './combinator.ts';
import { Served, Session } from './server.ts';

type Args = Record<string, unknown>;

export class ForwardingFailure extends Error {
  readonly code: string;
  constructor(code: string, message: string) {
    super(message);
    this.code = code;
  }
}

interface Middle {
  origin: Session<combinator.Server>;
  destination: Session<combinator.Server>;
  upstream: LiveScope;
  downstream: LiveScope;
  toolkit: combinator.Toolkit;
  from: Record<string, unknown>;
  to: Record<string, unknown>;
}

class Endpoint {
  readonly server = new CombinatorServer();
  served!: Served<Session<combinator.Client>>;
  scope?: LiveScope;
  retained?: combinator.Toolkit;
  twice?: combinator.Unary;
  identity?: combinator.Unary;
}

const handles = new Map<string, Middle | Endpoint>();
let next = 0;
const within = (args: Args) => typeof args.within_ms === 'number' ? args.within_ms : 5000;
const lookup = (args: Args): Middle => {
  const middle = handles.get(String(args.on));
  if (!middle || middle instanceof Endpoint) throw new ForwardingFailure('unknown_handle', String(args.on));
  return middle;
};
const endpointOf = (args: Args): Endpoint => {
  const endpoint = handles.get(String(args.on));
  if (!(endpoint instanceof Endpoint)) throw new ForwardingFailure('unknown_handle', String(args.on));
  return endpoint;
};

export function resetForwarding(): void {
  for (const handle of handles.values()) {
    if (handle instanceof Endpoint) handle.served.shutdown();
    else { handle.origin.close(); handle.destination.close(); }
  }
  handles.clear();
}

function nonce(raw: unknown): string {
  const binding = (raw as { binding: string }).binding;
  const at = binding.indexOf('.');
  if (at <= 0) throw new ForwardingFailure('invalid', 'binding has no scope nonce');
  return binding.slice(0, at);
}

function ordered(value: unknown): unknown {
  if (!value || typeof value !== 'object') return value;
  return Object.fromEntries(Object.entries(value).sort(([a], [b]) => a.localeCompare(b)).map(([key, entry]) => [key, ordered(entry)]));
}

export const forwardingOps: Record<string, (args: Args) => unknown | Promise<unknown>> = {
  'gen.forwarding_serve': async () => {
    const endpoint = new Endpoint();
    endpoint.served = await new Served(socket => {
      const connection = new Session<combinator.Client>({ role: 'server' });
      const scope = liveOver(connection.peer);
      endpoint.scope = scope;
      handleWire(connection.peer.wire(), ['fixture.forwarding.retain'], wire => {
        combinator.validateWire('Toolkit', wire);
        endpoint.retained = combinator.importToolkit(scope.owner(), wire);
      });
      connection.expose(binding.toWire(remote => {
        connection.model = remote;
        return { methods: endpoint.server, events: {} };
      }, { scope }));
      return connection.attach(socket);
    }).listen();
    const handle = 'forwardendpoint' + String(++next);
    handles.set(handle, endpoint);
    return { handle, url: endpoint.served.url };
  },
  'gen.forwarding_capture': async args => {
    const endpoint = endpointOf(args);
    const toolkit = endpoint.retained;
    if (!toolkit) throw new ForwardingFailure('invalid', 'no retained toolkit');
    const options = { signal: AbortSignal.timeout(within(args)) };
    const twice = await toolkit.twice(async value => value + 1, options);
    const identity = await toolkit.identity(options);
    await toolkit.apply(async value => value * 10, options);
    endpoint.twice = twice;
    endpoint.identity = identity;
    return {};
  },
  'gen.forwarding_invoke': async args => {
    const { twice, identity } = endpointOf(args);
    if (!twice || !identity) throw new ForwardingFailure('invalid', 'no retained results');
    const options = { signal: AbortSignal.timeout(within(args)) };
    return { twice: await twice(Number(args.with), options), identity: await identity(Number(args.identity_with), options) };
  },
  'gen.forwarding_applied': args => ({ values: [...endpointOf(args).server.applied] }),
  'gen.forwarding_origin_refusal': async args => {
    const toolkit = endpointOf(args).retained;
    if (!toolkit) throw new ForwardingFailure('invalid', 'no retained producer');
    try {
      await toolkit.identity({ signal: AbortSignal.timeout(within(args)) });
      return { unexpected_success: true };
    } catch (error) {
      return { error: { code: error instanceof combinator.DuplexError ? error.code : 'failed', message: error instanceof Error ? error.message : String(error) } };
    }
  },
  'gen.forwarding_dial': async args => {
    let origin: Session<combinator.Server> | undefined;
    let destination: Session<combinator.Server> | undefined;
    try {
      origin = new Session<combinator.Server>();
      destination = new Session<combinator.Server>();
      const upstream = liveOver(origin.peer);
      const downstream = liveOver(destination.peer);
      for (const [connection, scope] of [[origin, upstream], [destination, downstream]] as const) {
        connection.expose(combinator.toWire(remote => {
          connection.model = remote;
          return { methods: {}, events: {} };
        }, { scope }));
      }
      await origin.connect(String(args.origin));
      await destination.connect(String(args.destination));
      const options = { signal: AbortSignal.timeout(within(args)) };
      const source = await callWire(origin.peer.wire(), ['toolkit'], { seed: 3 }, options);
      combinator.validateWire('Toolkit', source);
      const toolkit = combinator.importToolkit(upstream.owner(), source);
      // Exporting the typed proxies installs wrappers that translate callable
      // requests and results between these scopes, unlike raw forward().
      const target = combinator.exportToolkit(downstream.owner(), toolkit);
      await callWire(destination.peer.wire(), ['fixture.forwarding.retain'], target, options);
      const from = source as Record<string, unknown>;
      const to = target as Record<string, unknown>;
      const handle = 'forwardmiddle' + String(++next);
      handles.set(handle, { origin, destination, upstream, downstream, toolkit, from, to });
      return { handle, different_nonces: nonce(from.twice) !== nonce(to.twice) };
    } catch (error) {
      origin?.close();
      destination?.close();
      throw error;
    }
  },
  'gen.forwarding_await_counts': async args => {
    const handle = handles.get(String(args.on));
    if (!handle) throw new ForwardingFailure('unknown_handle', String(args.on));
    const expected = JSON.stringify(ordered(args.counts));
    const end = Date.now() + within(args);
    for (;;) {
      const counts = handle instanceof Endpoint ? handle.scope?.counts() ?? null : { origin: handle.upstream.counts(), destination: handle.downstream.counts() };
      if (JSON.stringify(ordered(counts)) === expected) return counts;
      if (Date.now() >= end) throw new ForwardingFailure('timeout', 'counts: got ' + JSON.stringify(counts) + ', want ' + expected);
      await new Promise(resolve => setTimeout(resolve, 5));
    }
  },
  'gen.forwarding_release_destination': args => {
    const middle = lookup(args);
    middle.downstream.release(middle.downstream.decode(middle.to.twice));
    return {};
  },
  'gen.forwarding_origin_alive': async args => {
    const middle = lookup(args);
    const options = { signal: AbortSignal.timeout(within(args)) };
    const twice = await middle.toolkit.twice(async value => value + 10, options);
    return { value: await twice(1, options) };
  },
  'gen.forwarding_release_origin': args => {
    const middle = lookup(args);
    middle.upstream.release(middle.upstream.decode(middle.from.identity));
    return {};
  },
};
