/** The live layer under control: scopes over peers, bindings exported and imported, and what each was asked. */
import { DuplexError } from '@nightseam/runtime';
import { forward, liveOver, type Invoke, type LiveOptions, type LiveScope, type Reference } from '@nightseam/live';
import { fail, invalid, intOf, stringOf, withinOf, type Args, type Op, type Testee } from './testee.ts';
import { Call, isPeer, type Peer } from './peer.ts';

/** What one invocation of an exported binding was: its contract and how it went, never what it carried. */
interface Invocation {
  contract: string;
  outcome: string;
}

/** A live scope of the driver's making, with what its exported bindings were asked. */
class ScopeOn {
  readonly scope: LiveScope;
  readonly peer: Peer;
  private readonly invocations: Invocation[] = [];
  private waiting: (() => void) | undefined;

  constructor(scope: LiveScope, peer: Peer) {
    this.scope = scope;
    this.peer = peer;
  }

  took(contract: string, outcome: string): void {
    this.invocations.push({ contract, outcome });
    const wake = this.waiting;
    this.waiting = undefined;
    wake?.();
  }

  async take(contract: string, within: number): Promise<Invocation | undefined> {
    const deadline = Date.now() + within;
    for (;;) {
      const at = this.invocations.findIndex((one) => contract === '' || one.contract === contract);
      if (at >= 0) return this.invocations.splice(at, 1)[0];
      const left = deadline - Date.now();
      if (left <= 0) return undefined;
      await new Promise<void>((resolve) => {
        const timer = setTimeout(resolve, Math.min(left, 25));
        this.waiting = () => {
          clearTimeout(timer);
          resolve();
        };
      });
    }
  }

  shutdown(): void {
    this.scope.close();
  }
}

const isScope = (object: unknown): object is ScopeOn => object instanceof ScopeOn;

/** One imported binding: the function to call it with, and the scope it belongs to. */
class BindingOn {
  readonly invoke: Invoke;
  readonly scope: ScopeOn;
  constructor(invoke: Invoke, scope: ScopeOn) {
    this.invoke = invoke;
    this.scope = scope;
  }
}

const isBinding = (object: unknown): object is BindingOn => object instanceof BindingOn;

const liveError = (error: unknown) => {
  if (error instanceof DuplexError) return fail(error.code, error.message);
  return fail('failed', String(error));
};

/** What an exported binding does, from the same canned set a peer's handler takes. */
function cannedInvoke(t: Testee, s: ScopeOn, contract: string, behavior: Args): Invoke {
  const kind = typeof behavior.kind === 'string' ? behavior.kind : 'echo';
  return async (request) => {
    switch (kind) {
      case '':
      case 'echo':
        s.took(contract, 'ok');
        return request ?? null;
      case 'return':
        s.took(contract, 'ok');
        return behavior.value ?? null;
      case 'through': {
        // A binding that calls another binding: what a callable returned by
        // one call does when it reaches a callable supplied by it.
        const a = t.lookup(behavior.attachment, isBinding, 'a live attachment');
        try {
          const result = await a.invoke(request ?? null);
          s.took(contract, 'ok');
          return result;
        } catch (error) {
          s.took(contract, 'error');
          throw error;
        }
      }
      case 'fail':
        s.took(contract, 'error');
        throw new DuplexError(
          typeof behavior.code === 'string' ? behavior.code : 'failed',
          typeof behavior.message === 'string' ? behavior.message : '',
          behavior.data,
        );
      case 'wait': {
        s.took(contract, 'started');
        // Never settles of its own accord; the scope closing or the peer
        // ending is what ends it, and the runner's own deadline bounds it.
        await new Promise<void>(() => {});
        return null;
      }
      case 'hold': {
        // Held until the remote emits what releases it, its own cancellation
        // notwithstanding: how a scenario holds *when* a withdrawn invocation
        // settles. The one binding that does not stop when it is told to.
        const until = typeof behavior.until === 'string' ? behavior.until : '';
        await new Promise<void>((resolve) => {
          const stop = s.peer.peer.onEvent(until, () => {
            clearTimeout(timer);
            stop();
            resolve();
          });
          const timer = setTimeout(() => {
            stop();
            resolve();
          }, 30_000);
          s.took(contract, 'started');
        });
        s.took(contract, 'ok');
        return behavior.value ?? null;
      }
    }
    throw invalid('a binding echoes, returns, calls through, fails, waits or holds');
  };
}

export function liveOps(t: Testee): Record<string, Op> {
  const scopeOf = (args: Args, name = 'on') => t.lookup(args[name], isScope, 'a live scope');
  const bindingOf = (args: Args, name = 'on') => t.lookup(args[name], isBinding, 'a live attachment');

  /**
   * The reference a step carries, which is the value the other side's export
   * answered with: a scope decodes it, since a reference of no scope is not a
   * reference at all.
   */
  const referenceOf = (args: Args, s: ScopeOn, name = 'reference'): Reference => {
    if (args[name] === undefined) throw invalid(`${name} names no reference`);
    try {
      return s.scope.decode(args[name]);
    } catch (error) {
      throw liveError(error);
    }
  };

  return {
    'live.over': (args) => {
      const p = t.lookup(args.on, isPeer, 'a peer');
      const given = (args.options ?? {}) as Args;
      const options: LiveOptions = {};
      if (given.max_exports !== undefined) options.maxExports = intOf(given, 'max_exports', 0);
      if (given.max_imports !== undefined) options.maxImports = intOf(given, 'max_imports', 0);
      try {
        return { handle: t.mint('lv', new ScopeOn(liveOver(p.peer, options), p)) };
      } catch (error) {
        throw invalid(String(error));
      }
    },
    'live.export': (args) => {
      const s = scopeOf(args);
      const contract = stringOf(args, 'contract');
      const behavior = (args.behavior ?? {}) as Args;
      try {
        const reference = s.scope.export(contract, cannedInvoke(t, s, contract, behavior));
        return { reference: JSON.parse(JSON.stringify(reference)) };
      } catch (error) {
        throw liveError(error);
      }
    },
    'live.import': (args) => {
      const s = scopeOf(args);
      const contract = stringOf(args, 'contract');
      const reference = referenceOf(args, s);
      try {
        return { handle: t.mint('at', new BindingOn(s.scope.import(reference, contract), s)) };
      } catch (error) {
        throw liveError(error);
      }
    },
    'live.invoke': (args) => {
      const a = bindingOf(args);
      const timeout = intOf(args, 'timeout_ms', 0);
      const controller = new AbortController();
      if (timeout > 0) setTimeout(() => controller.abort(), timeout);
      // An invocation is a call like any other: it answers with a call handle,
      // and call.await and call.cancel act on it.
      const promise = a.invoke(args.request ?? null, { signal: controller.signal });
      return { handle: t.mint('call', new Call(a.scope.peer, promise, controller)) };
    },
    'live.release': (args) => {
      const s = scopeOf(args);
      const reference = referenceOf(args, s);
      try {
        s.scope.release(reference);
      } catch (error) {
        throw liveError(error);
      }
      return {};
    },
    'live.forward': (args) => {
      const s = scopeOf(args);
      const contract = stringOf(args, 'contract');
      const a = bindingOf(args, 'attachment');
      try {
        return { reference: JSON.parse(JSON.stringify(forward(s.scope, contract, a.invoke))) };
      } catch (error) {
        throw liveError(error);
      }
    },
    'live.counts': (args) => scopeOf(args).scope.counts(),
    'live.await_invocation': async (args) => {
      const s = scopeOf(args);
      const contract = stringOf(args, 'contract');
      const one = await s.take(contract, withinOf(args));
      if (one === undefined) throw fail('timeout', 'no invocation');
      return one;
    },
    'live.close': (args) => {
      scopeOf(args).scope.close();
      return {};
    },
  };
}
