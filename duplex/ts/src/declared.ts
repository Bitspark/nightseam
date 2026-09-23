import type { Message, Path, Wire } from '@bitspark/bitwire';
import { encodePath, WireError } from './wire.ts';

/** Explicit origin behavior for a node which only groups children. */
export const refusingOrigin: Wire = Object.freeze({
  send() {
    throw new WireError('no_route');
  },
});

/** Complete access: an endpoint, selected view, forwarder, guard or composite. */
export type DeclaredChild = readonly [string, Wire];
export interface DeclaredParts {
  readonly origin: Wire;
  readonly children: DeclaredChild[];
}

export class DeclaredError extends Error {
  readonly code: 'invalid_value' | 'child_exists';
  constructor(code: DeclaredError['code']) {
    super(`Declared composition ${code}.`);
    this.name = 'DeclaredError';
    this.code = code;
  }
}

function checkPath(path: Path): void {
  if (path.some((segment) => typeof segment !== 'string')) throw new WireError('invalid_path');
  encodePath(path);
}

/**
 * An immutable construction description for the assembler. Origin and child
 * capabilities are borrowed, retaining their state and identities. Callers receive
 * bind()'s separate send-only facade. Keep descriptions separately to reconstruct
 * a recursively declared tree; opaque children are retained whole.
 */
export class Declared {
  readonly #origin: Wire;
  readonly #children: ReadonlyMap<string, Wire>;

  private constructor(origin: Wire, children: ReadonlyMap<string, Wire>) {
    this.#origin = origin;
    this.#children = children;
  }

  /** Copy complete entries; refuse missing access and duplicate or non-scalar keys. */
  static compose(origin: Wire, children: Iterable<DeclaredChild>): Declared {
    if (!origin || typeof origin.send !== 'function') throw new DeclaredError('invalid_value');
    const routes = new Map<string, Wire>();
    for (const [key, child] of children) {
      checkPath([key]);
      if (!child || typeof child.send !== 'function') throw new DeclaredError('invalid_value');
      if (routes.has(key)) throw new DeclaredError('child_exists');
      routes.set(key, child);
    }
    return new Declared(origin, routes);
  }

  /** Copy containers in exact UTF-8 key order, preserving all capability identities. */
  decompose(): DeclaredParts {
    const encoder = new TextEncoder();
    const children = [...this.#children].sort(([a], [b]) => {
      const left = encoder.encode(a),
        right = encoder.encode(b);
      for (let i = 0; i < Math.min(left.length, right.length); i++) {
        if (left[i] !== right[i]) return left[i]! - right[i]!;
      }
      return left.length - right.length;
    });
    return { origin: this.#origin, children };
  }

  /**
   * Delegate each message unchanged to the origin at [] or to one complete child
   * with its first segment removed. All frame kinds follow this rule. Admission,
   * asynchronous dispatch and invocation lifetime belong to the destination and
   * profile; guards are ordinary wrappers. This access stays bound after assembler
   * reconstruction and acquires no receiver attachment or lifecycle authority.
   */
  bind(): Wire {
    return Object.freeze({
      send: (path: Path, message: Message): void => {
        checkPath(path);
        if (path.length === 0) return this.#origin.send([], message);
        const child = this.#children.get(path[0]!);
        if (!child) throw new WireError('no_route');
        child.send(path.slice(1), message);
      },
    });
  }
}
