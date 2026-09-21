import { encodePath, WireError } from '@nightseam/duplex';
import type { Endpoint, Message, Path, Receiver, Wire } from '@nightseam/duplex';
import { DuplexError } from './error.ts';
import { response, wireContext } from './wire.ts';

/** Registration authority; close releases this registry, never its borrowed carrier. */
export interface HandlerRegistry extends Wire {
  register(path: Path, receiver: Receiver): () => void;
  close(code?: number, reason?: string): void;
}
interface Registration {
  path: Path;
  receiver: Receiver;
}
interface Capture {
  registration: Registration;
  path: Path;
}
/** @internal Owned by one admitted runtime invocation, not a global router ledger. */
export interface DispatchRoutes {
  retired: boolean;
  captures: Map<Dispatcher, Map<string, Capture>>;
}
/** @internal Every new carrier admission has a fresh capture lifetime. */
export function createDispatchRoutes(): DispatchRoutes {
  return { retired: false, captures: new Map() };
}
/** @internal Called at terminal retirement, after queued cancellation drains. */
export function retireDispatchRoutes(routes: DispatchRoutes | undefined): void {
  if (!routes) return;
  routes.retired = true;
  routes.captures.clear();
}

/** One attachment, with explicit exact-before-longest-prefix dispatch. */
export class Dispatcher implements HandlerRegistry {
  private readonly exact = new Map<string, Registration>();
  private readonly prefixes = new Map<string, Registration>();
  private ended = false;
  private detach: (() => void) | undefined;

  constructor(private readonly root: Endpoint) {
    const detach = root.receive({
      message: (path, message) => this.deliver(path, message),
      closed: (code, reason) => this.close(code, reason),
    });
    if (this.ended) {
      detach();
      throw new WireError('closed');
    }
    this.detach = detach;
  }
  send(path: Path, message: Message): void {
    if (this.ended) throw new WireError('closed');
    this.root.send(path, message);
  }
  register(path: Path, receiver: Receiver): () => void {
    return this.install(path, receiver, this.exact);
  }
  registerPrefix(path: Path, receiver: Receiver): () => void {
    return this.install(path, receiver, this.prefixes);
  }
  private install(path: Path, receiver: Receiver, routes: Map<string, Registration>): () => void {
    const name = encodePath(path);
    if (this.ended) throw new WireError('closed');
    if (routes.has(name)) throw new WireError('receiver_exists');
    const registration = { path: [...path], receiver };
    routes.set(name, registration);
    return () => {
      if (routes.get(name) === registration) routes.delete(name);
    };
  }
  private match(path: Path, name: string): Registration | undefined {
    const exact = this.exact.get(name);
    if (exact) return exact;
    let selected: Registration | undefined;
    for (const candidate of this.prefixes.values()) {
      if (
        candidate.path.length <= path.length &&
        (!selected || candidate.path.length > selected.path.length) &&
        candidate.path.every((part, index) => part === path[index])
      )
        selected = candidate;
    }
    return selected;
  }
  private deliver(path: Path, message: Message): void | Promise<void> {
    const name = encodePath(path);
    const lifetime = message.return ? wireContext(message.return)?.routes : undefined;
    if (message.frame.kind === 'cancel') {
      const capture = lifetime?.captures.get(this)?.get(name);
      return capture?.registration.receiver.message?.([...capture.path], message);
    }
    if (message.frame.kind === 'request' && !lifetime) {
      response(
        message,
        undefined,
        new DuplexError('invalid_message', 'Invocation requires a profile-owned lifetime association.'),
      );
      return;
    }
    let registration = this.ended ? undefined : this.match(path, name);
    if (registration && message.frame.kind === 'request') {
      if (lifetime!.retired) registration = undefined;
      else {
        let captures = lifetime!.captures.get(this);
        if (!captures) {
          captures = new Map();
          lifetime!.captures.set(this, captures);
        }
        const previous = captures.get(name);
        if (previous) registration = previous.registration;
        else captures.set(name, { registration, path: [...path] });
      }
    }
    if (!registration?.receiver.message) {
      if (message.frame.kind === 'request')
        response(message, undefined, new DuplexError('method_not_found', 'Unknown method.'));
      return;
    }
    return registration.receiver.message([...path], message);
  }
  select(path: Path): SelectedEndpoint {
    return new SelectedEndpoint(this, [...path]);
  }
  close(code = 1000, reason = ''): void {
    if (this.ended) return;
    this.ended = true;
    const detach = this.detach;
    this.detach = undefined;
    const ending = [...this.exact.values(), ...this.prefixes.values()];
    this.exact.clear();
    this.prefixes.clear();
    detach?.();
    for (const registration of ending) {
      try {
        registration.receiver.closed?.(code, reason);
      } catch {
        /* Each owner receives its end. */
      }
    }
  }
}
export function createDispatcher(endpoint: Endpoint): Dispatcher {
  return new Dispatcher(endpoint);
}

interface Attachment {
  receiver: Receiver;
  detach?: () => void;
}
/** A route owned by one dispatcher, with no borrowed-root closure authority. */
export class SelectedEndpoint implements Endpoint {
  private ended = false;
  private attachment: Attachment | undefined;
  constructor(
    private readonly owner: Dispatcher,
    private readonly prefix: Path,
  ) {}
  select(path: Path): SelectedEndpoint {
    return this.owner.select([...this.prefix, ...path]);
  }
  send(path: Path, message: Message): void {
    if (this.ended) throw new WireError('closed');
    this.owner.send([...this.prefix, ...path], message);
  }
  receive(receiver: Receiver): () => void {
    if (this.ended) throw new WireError('closed');
    if (this.attachment) throw new WireError('receiver_exists');
    const attachment: Attachment = { receiver };
    this.attachment = attachment;
    try {
      const detach = this.owner.registerPrefix(this.prefix, {
        message: (path, message) => {
          if (receiver.message) return receiver.message(path.slice(this.prefix.length), message);
          if (message.frame.kind === 'request')
            response(message, undefined, new DuplexError('method_not_found', 'Unknown method.'));
        },
        closed: (code, reason) => this.remove(attachment, { code, reason }),
      });
      if (this.attachment !== attachment) {
        detach();
        throw new WireError('closed');
      }
      attachment.detach = detach;
    } catch (error) {
      if (this.attachment === attachment) this.attachment = undefined;
      throw error;
    }
    return () => this.remove(attachment);
  }
  private remove(attachment: Attachment, ending?: { code: number; reason: string }): void {
    if (this.attachment !== attachment) return;
    this.attachment = undefined;
    attachment.detach?.();
    if (ending) attachment.receiver.closed?.(ending.code, ending.reason);
  }
  close(code = 1000, reason = ''): void {
    this.ended = true;
    if (this.attachment) this.remove(this.attachment, { code, reason });
  }
}
