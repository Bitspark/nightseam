/** Generated conversion between two real connections, A-B and B-C.
 * This testee runs both generated endpoint bindings and the intermediary.
 * The test transport's retain route imports generated Toolkit.
 */
import * as combinator from './api/ts/combinator-client/src/index.ts';
import * as binding from './api/ts/combinator-binding/src/index.ts';
import { scopeOf, type LiveScope } from '@nightseam/live';
import { CombinatorServer } from './combinator.ts';
import { Served } from './server.ts';

type Args = Record<string, unknown>;

export class ForwardingFailure extends Error {
  readonly code: string;
  constructor(code: string, message: string) {
    super(message);
    this.code = code;
  }
}

interface Middle {
  origin: combinator.Client;
  destination: combinator.Client;
  upstream: LiveScope;
  downstream: LiveScope;
  toolkit: combinator.Toolkit;
  from: Record<string, unknown>;
  to: Record<string, unknown>;
}

class Endpoint {
  readonly server = new CombinatorServer();
  served!: Served<binding.Remote>;
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
    endpoint.served = await new Served(socket => binding.serve(socket, {
      dispatch: (method, wire, context) => {
        if (method !== 'fixture.forwarding.retain') throw new combinator.DuplexError('method_not_found', 'unknown method ' + method);
        const scope = scopeOf(context.peer);
        if (!scope) throw new ForwardingFailure('invalid', 'generated binding installed no scope');
        combinator.validateWire('Toolkit', wire);
        endpoint.retained = combinator.importToolkit(scope, wire);
      },
    }, endpoint.server, {}).then(peer => {
      endpoint.scope = scopeOf(peer);
      if (!endpoint.scope) { peer.close(); throw new ForwardingFailure('invalid', 'generated binding installed no scope'); }
      return new binding.Remote(peer);
    })).listen();
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
    let origin: combinator.Client | undefined;
    let destination: combinator.Client | undefined;
    try {
      origin = await combinator.Client.dial(String(args.origin), {}, {}, {});
      destination = await combinator.Client.dial(String(args.destination), {}, {}, {});
      const upstream = scopeOf(origin.peer);
      const downstream = scopeOf(destination.peer);
      if (!upstream || !downstream) throw new ForwardingFailure('invalid', 'generated client installed no scope');
      const options = { signal: AbortSignal.timeout(within(args)) };
      const source = await origin.peer.call('toolkit', { seed: 3 }, options);
      combinator.validateWire('Toolkit', source);
      const toolkit = combinator.importToolkit(upstream.owner(), source);
      // Exporting the typed proxies installs wrappers that translate callable
      // requests and results between these scopes, unlike raw forward().
      const target = combinator.exportToolkit(downstream.owner(), toolkit);
      await destination.peer.call('fixture.forwarding.retain', target, options);
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
