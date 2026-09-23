import type { Message, Path, Wire } from '@bitspark/bitwire';
import { encodePath, WireError } from './wire.ts';

/** A bounded synchronous admission check. It must not mutate the message. */
export interface AdmissionPolicy {
  admit(path: Path, message: Message): void;
}

/** An explicit identity policy. Stateful policies retain their original instances. */
export const permitAdmission: AdmissionPolicy = Object.freeze({ admit() {} });

/** Explicit own-origin behavior for a node which only groups children. */
export const refusingOrigin: Wire = Object.freeze({
  send() {
    throw new WireError('no_route');
  },
});

/** ADR0005's own value: origin access and the policy on this entire occurrence. */
export interface DeclaredValue {
  readonly own: Wire;
  readonly policy: AdmissionPolicy;
}

export type DeclaredChild = readonly [string, Declared];
export interface DeclaredParts {
  readonly value: DeclaredValue;
  readonly children: DeclaredChild[];
}

export class DeclaredError extends Error {
  readonly code: 'invalid_value' | 'child_exists' | 'invalid_frame';
  constructor(code: DeclaredError['code']) {
    super(`Declared composition ${code}.`);
    this.name = 'DeclaredError';
    this.code = code;
  }
}

/**
 * An immutable construction description. Own capabilities and policy state are
 * borrowed, never cloned or closed. Descriptions expose raw parts and belong to
 * the assembler; callers who need guarded access receive bind(), not this object.
 */
export class Declared {
  readonly #value: DeclaredValue;
  readonly #children: ReadonlyMap<string, Declared>;

  private constructor(value: DeclaredValue, children: ReadonlyMap<string, Declared>) {
    this.#value = Object.freeze({ own: value.own, policy: value.policy });
    this.#children = children;
  }

  /** Copy complete child entries, refusing duplicate or invalid scalar keys. */
  static compose(value: DeclaredValue, children: Iterable<DeclaredChild>): Declared {
    if (
      !value?.own ||
      typeof value.own.send !== 'function' ||
      !value.policy ||
      typeof value.policy.admit !== 'function'
    ) {
      throw new DeclaredError('invalid_value');
    }
    const routes = new Map<string, Declared>();
    for (const [key, child] of children) {
      encodePath([key]);
      if (!(child instanceof Declared)) throw new DeclaredError('invalid_value');
      if (routes.has(key)) throw new DeclaredError('child_exists');
      routes.set(key, child);
    }
    return new Declared(value, routes);
  }

  /** Raw complete parts. This copies containers, retaining all live identities. */
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
    return { value: { ...this.#value }, children };
  }

  /** Raw assembler lookup. Use at(node.bind(), path) for guarded caller access. */
  at(path: Path): Declared | undefined {
    encodePath(path);
    let current: Declared | undefined = this;
    for (const key of path) {
      current = current.#children.get(key);
      if (!current) return undefined;
    }
    return current;
  }

  /** Exact immutable attachment: existing parent, fresh key, valid child. */
  attach(parent: Path, key: string, child: Declared): Declared {
    encodePath([...parent, key]);
    if (!(child instanceof Declared)) throw new DeclaredError('invalid_value');
    const target = this.at(parent);
    if (!target) throw new WireError('no_route');
    if (target.#children.has(key)) throw new DeclaredError('child_exists');
    const extend = (node: Declared, depth: number): Declared => {
      const { value, children } = node.decompose();
      if (depth === parent.length) children.push([key, child]);
      else {
        const index = children.findIndex(([name]) => name === parent[depth]);
        const [name, previous] = children[index]!;
        children[index] = [name, extend(previous, depth + 1)];
      }
      return Declared.compose(value, children);
    };
    return extend(this, 0);
  }

  /**
   * Send-only access, with one check per ancestor occurrence. Missing children
   * refuse after the preceding policies; own access never handles a nonempty
   * suffix. Responses/cancellation use the admitted invocation's captured access.
   * Destination endpoints supply asynchronous dispatch and invocation lifetime.
   */
  bind(): Wire {
    return Object.freeze({
      send: (path: Path, message: Message): void => {
        encodePath(path);
        if (message.frame.kind !== 'request' && message.frame.kind !== 'event')
          throw new DeclaredError('invalid_frame');
        let current: Declared = this;
        for (let i = 0; ; i++) {
          current.#value.policy.admit(Object.freeze(path.slice(i)), message);
          if (i === path.length) return current.#value.own.send([], message);
          const child = current.#children.get(path[i]!);
          if (!child) throw new WireError('no_route');
          current = child;
        }
      },
    });
  }
}
