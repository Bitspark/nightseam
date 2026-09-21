import type { Message, Path, Receiver, Wire } from '@bitspark/bitwire';

// The public Nightseam names present the shared contract's actual declarations.
export type { Path, ProfileKind, ProfileFrame, ProfileError, ReturnAddress, Message, Receiver, Wire } from '@bitspark/bitwire';

export class WireError extends Error {
  readonly code: 'closed' | 'no_route' | 'receiver_exists' | 'invalid_path';
  constructor(code: WireError['code']) {
    super(`Wire ${code}.`);
    this.name = 'WireError';
    this.code = code;
  }
}

function scalar(value: string): void {
  for (let i = 0; i < value.length; i++) {
    const unit = value.charCodeAt(i);
    if (unit >= 0xd800 && unit <= 0xdbff) {
      const low = value.charCodeAt(++i);
      if (!(low >= 0xdc00 && low <= 0xdfff)) throw new WireError('invalid_path');
    } else if (unit >= 0xdc00 && unit <= 0xdfff) throw new WireError('invalid_path');
  }
}

/** UTF-8 byte-length-prefixed segments; [] is '', whereas [''] is '0:'. */
export function encodePath(path: Path): string {
  const encoder = new TextEncoder();
  return path
    .map((segment) => {
      scalar(segment);
      return `${encoder.encode(segment).length}:${segment}`;
    })
    .join('');
}
/** Accepts only the canonical encoding, retaining dots, empty strings and BOMs. */
export function decodePath(encoded: string): string[] {
  scalar(encoded);
  const bytes = new TextEncoder().encode(encoded);
  const decoder = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true });
  const path: string[] = [];
  for (let offset = 0; offset < bytes.length;) {
    const start = offset;
    let length = 0;
    while (offset < bytes.length && bytes[offset] !== 58) {
      const digit = bytes[offset++]! - 48;
      if (digit < 0 || digit > 9) throw new WireError('invalid_path');
      length = length * 10 + digit;
      if (!Number.isSafeInteger(length)) throw new WireError('invalid_path');
    }
    if (offset === start || offset === bytes.length || (offset - start > 1 && bytes[start] === 48))
      throw new WireError('invalid_path');
    offset++;
    if (length > bytes.length - offset) throw new WireError('invalid_path');
    try {
      path.push(decoder.decode(bytes.subarray(offset, offset + length)));
    } catch {
      throw new WireError('invalid_path');
    }
    offset += length;
  }
  return path;
}

/** Selects a path. Closing the view closes its existing endpoint. */
export function at(root: Wire, path: Path): Wire {
  const prefix = [...path];
  return {
    send: (suffix, message) => root.send([...prefix, ...suffix], message),
    receive: (suffix, receiver) =>
      root.receive([...prefix, ...suffix], {
        ...receiver,
        message: (delivered, message) => receiver.message?.(delivered.slice(prefix.length), message),
      }),
    close: (code, reason) => root.close(code, reason),
  };
}

interface Registration {
  receiver: Receiver;
  active: boolean;
  detach?: () => void;
}
/**
 * Consumes one path segment; [] has no leaf, and [''] can select an empty key.
 * Copies the map. Closing detaches this mount's registrations, never children.
 */
export function mount(children: ReadonlyMap<string, Wire>): Wire {
  const routes = new Map(children);
  const registrations = new Set<Registration>();
  let closed = false;
  const destination = (path: Path): Wire => {
    if (closed) throw new WireError('closed');
    encodePath(path);
    const child = path.length ? routes.get(path[0]!) : undefined;
    if (!child) throw new WireError('no_route');
    return child;
  };
  const remove = (registration: Registration, ending?: { code: number; reason: string }): void => {
    if (!registration.active) return;
    registration.active = false;
    registrations.delete(registration);
    registration.detach?.();
    if (ending) registration.receiver.closed?.(ending.code, ending.reason);
  };
  const receive = (path: Path, receiver: Receiver): (() => void) => {
    if (!path.length && receiver.namespace) {
      if (closed) throw new WireError('closed');
      const keys = [...routes.keys()];
      const detaches: (() => void)[] = [];
      const registration: Registration = {
        receiver,
        active: true,
        detach: () => {
          for (const detach of detaches.splice(0)) detach();
        },
      };
      registrations.add(registration);
      let remaining = keys.length;
      try {
        for (const key of keys) {
          const detach = receive([key], {
            namespace: true,
            message: receiver.message,
            closed: (code, reason) => {
              if (--remaining === 0) remove(registration, { code, reason });
            },
          });
          if (!registration.active) {
            detach();
            throw new WireError('closed');
          }
          detaches.push(detach);
        }
      } catch (error) {
        remove(registration);
        throw error;
      }
      return () => remove(registration);
    }
    const child = destination(path);
    const registration: Registration = { receiver, active: true };
    registrations.add(registration);
    try {
      registration.detach = child.receive(path.slice(1), {
        namespace: receiver.namespace,
        message: (suffix, message) => {
          // Captured requests retain this receiver for later cancellation.
          return receiver.message?.([path[0]!, ...suffix], message);
        },
        closed: (code, reason) => remove(registration, { code, reason }),
      });
    } catch (error) {
      registration.active = false;
      registrations.delete(registration);
      throw error;
    }
    if (!registration.active) {
      registration.detach();
      throw new WireError('closed');
    }
    return () => remove(registration);
  };
  return {
    send: (path, message) => destination(path).send(path.slice(1), message),
    receive,
    close: (code = 1000, reason = '') => {
      if (closed) return;
      closed = true;
      const held = [...registrations];
      registrations.clear();
      for (const registration of held) registration.active = false;
      for (const registration of held) registration.detach?.();
      for (const registration of held) registration.receiver.closed?.(code, reason);
    },
  };
}
