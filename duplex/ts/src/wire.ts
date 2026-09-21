/** A relative path of Unicode-scalar strings, with no normalization. */
export type Path = readonly string[];

interface TracedFrame {
  readonly version: 1;
  readonly traceparent?: string;
  readonly tracestate?: string;
}
/** Public error data, without a dependency on the runtime's error class. */
export interface ProfileError {
  readonly code: string;
  readonly message: string;
  readonly data?: unknown;
}
/** The Send path supplies the method/event name; the frame has no second name. */
export type ProfileFrame = TracedFrame &
  (
    | {
        readonly kind: 'request';
        readonly id: string;
        readonly params: unknown;
        readonly meta?: Readonly<Record<string, string>>;
      }
    | ({ readonly kind: 'response'; readonly id: string } & (
        { readonly result: unknown; readonly error?: never } | { readonly error: ProfileError; readonly result?: never }
      ))
    | { readonly kind: 'event'; readonly data: unknown; readonly meta?: Readonly<Record<string, string>> }
    | { readonly kind: 'cancel'; readonly id: string }
  );
/** Local identity preserved through composition; never an envelope member. */
export interface ReturnAddress {
  readonly wire: Wire;
}
export interface Message {
  readonly frame: ProfileFrame;
  readonly return?: ReturnAddress;
}
export interface Receiver {
  message?: (path: Path, message: Message) => void;
  closed?: (code: number, reason: string) => void;
}
/**
 * An endpoint with an origin. Receive registers an exact dispatch path and
 * refuses a duplicate; detach is idempotent. Roots own bounded asynchronous
 * dispatch and carrier closure. Selection and mounting allocate neither peers
 * nor channels, and never invoke destination handlers inside send.
 */
export interface Wire {
  send(path: Path, message: Message): void;
  receive(path: Path, receiver: Receiver): () => void;
  close(code?: number, reason?: string): void;
}
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
    receive: (suffix, receiver) => root.receive([...prefix, ...suffix], receiver),
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
  return {
    send: (path, message) => destination(path).send(path.slice(1), message),
    receive: (path, receiver) => {
      const child = destination(path);
      const registration: Registration = { receiver, active: true };
      registrations.add(registration);
      try {
        registration.detach = child.receive(path.slice(1), {
          message: (suffix, message) => {
            if (registration.active) receiver.message?.(suffix, message);
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
    },
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
