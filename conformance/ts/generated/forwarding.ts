/** Generated conversion at B between two real connections, A-B and B-C.
 * Go serves the generated endpoint bindings; this testee runs the TypeScript
 * intermediary. The test transport's retain route imports generated Toolkit.
 */
import * as combinator from './api/ts/combinator-client/src/index.ts';
import { scopeOf, type LiveScope } from '@nightseam/live';

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

const handles = new Map<string, Middle>();
let next = 0;
const within = (args: Args) => typeof args.within_ms === 'number' ? args.within_ms : 5000;
const lookup = (args: Args): Middle => {
  const middle = handles.get(String(args.on));
  if (!middle) throw new ForwardingFailure('unknown_handle', String(args.on));
  return middle;
};

export function resetForwarding(): void {
  for (const middle of handles.values()) {
    middle.origin.close();
    middle.destination.close();
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

const unsupported = () => {
  throw new ForwardingFailure('unsupported', 'TypeScript renders a client and no binding; forwarding endpoints need the generated Go binding');
};

export const forwardingOps: Record<string, (args: Args) => unknown | Promise<unknown>> = {
  'gen.forwarding_serve': unsupported,
  'gen.forwarding_capture': unsupported,
  'gen.forwarding_invoke': unsupported,
  'gen.forwarding_applied': unsupported,
  'gen.forwarding_origin_refusal': unsupported,
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
    const middle = lookup(args);
    const expected = JSON.stringify(ordered(args.counts));
    const end = Date.now() + within(args);
    for (;;) {
      const counts = { origin: middle.upstream.counts(), destination: middle.downstream.counts() };
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
