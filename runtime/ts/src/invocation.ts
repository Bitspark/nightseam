import type { Message } from '@nightseam/duplex';

/** Resolve only invocations admitted by the supplied profile integration. */
export interface RoutingFacility {
  resolve(message: Message): InvocationRouting;
}
/** Execution authority is supplied separately to an execution owner or binder. */
export interface ExecutionFacility {
  resolve(message: Message): InvocationExecution;
}
export interface InvocationRouting {
  capture(control: (message: Message) => void): InvocationCapture;
}
export interface InvocationCapture {
  delivered(): void;
}
export interface InvocationExecution {
  begin(): InvocationBody;
}
export interface InvocationBody {
  done(): void;
}
export interface InvocationControl {
  deliver(): void;
}
export interface InvocationOwner {
  dispatchDone(): void;
  settle(): void;
  queueControl(message: Message): InvocationControl | undefined;
}
export interface InvocationLimits {
  captures: number;
  bodies: number;
}

export class InvocationError extends Error {
  readonly code: 'ended' | 'limit' | 'invalid';
  constructor(code: 'ended' | 'limit' | 'invalid') {
    super(`Invocation ${code}`);
    this.code = code;
  }
}

/**
 * Separate profile-owner, routing and execution facades. Possession does not
 * establish admission into another endpoint or confer verified runtime context.
 * Retirement releases accounting only after outcome, bodies and controls finish.
 */
export function createInvocation(
  limits: InvocationLimits,
  retired: () => void,
): {
  owner: InvocationOwner;
  routing: InvocationRouting;
  execution: InvocationExecution;
} {
  if (![limits.captures, limits.bodies].every((n) => Number.isSafeInteger(n) && n > 0))
    throw new InvocationError('invalid');
  const bounds = { ...limits };
  let settled = false,
    ended = false,
    dispatchDone = false;
  let deliveries = 1,
    bodies = 0,
    controls = 0;
  let control: Message | undefined;
  let controlDelivered = false;
  let onRetired: (() => void) | undefined = retired;
  const captures: Array<{ callback: ((message: Message) => void) | undefined; ready: boolean; notified: boolean }> = [];
  const finish = () => {
    if (ended || !settled || deliveries || bodies || controls) return;
    ended = true;
    for (const capture of captures) capture.callback = undefined;
    captures.length = 0;
    control = undefined;
    const notify = onRetired;
    onRetired = undefined;
    notify?.();
  };
  const invoke = (callbacks: Array<(message: Message) => void>, message: Message) => {
    try {
      for (const callback of callbacks) {
        try {
          callback(message);
        } catch {
          /* A failed receiver cannot prevent sibling control drain. */
        }
      }
    } finally {
      controls--;
      finish();
    }
  };
  const owner: InvocationOwner = {
    dispatchDone() {
      if (dispatchDone) return;
      dispatchDone = true;
      deliveries--;
      finish();
    },
    settle() {
      settled = true;
      finish();
    },
    queueControl(message) {
      if (ended) throw new InvocationError('ended');
      if (control) return undefined;
      if (message.frame.kind !== 'cancel') throw new InvocationError('invalid');
      control = message;
      controls++;
      let delivered = false;
      return {
        deliver() {
          if (delivered) return;
          delivered = true;
          controlDelivered = true;
          const callbacks: Array<(message: Message) => void> = [];
          for (const capture of captures) {
            if (!capture.ready || capture.notified) continue;
            capture.notified = true;
            if (capture.callback) callbacks.push(capture.callback);
          }
          invoke(callbacks, message);
        },
      };
    },
  };
  const routing: InvocationRouting = {
    capture(callback) {
      if (settled || ended) throw new InvocationError('ended');
      if (captures.length >= bounds.captures) throw new InvocationError('limit');
      const capture = { callback: callback as ((message: Message) => void) | undefined, ready: false, notified: false };
      captures.push(capture);
      deliveries++;
      return {
        delivered() {
          if (capture.ready) return;
          capture.ready = true;
          deliveries--;
          if (controlDelivered && !capture.notified) {
            capture.notified = true;
            controls++;
            invoke(capture.callback ? [capture.callback] : [], control!);
          } else finish();
        },
      };
    },
  };
  const execution: InvocationExecution = {
    begin() {
      if (settled || ended) throw new InvocationError('ended');
      if (bodies >= bounds.bodies) throw new InvocationError('limit');
      bodies++;
      let done = false;
      return {
        done() {
          if (done) return;
          done = true;
          bodies--;
          finish();
        },
      };
    },
  };
  return { owner, routing, execution };
}
