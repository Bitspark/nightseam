/** Acceptance-only recorded wire; the generated record/follow API is a later lane. */
import {
  at,
  mount,
  WireError,
  type Message,
  type Path,
  type Receiver,
  type Wire,
  type Endpoint,
} from '@nightseam/duplex';
import { createDispatcher, type HandlerRegistry, type WireDispatcher } from '@nightseam/runtime';

class Gate {
  readonly promise: Promise<void>;
  open!: () => void;
  constructor() {
    this.promise = new Promise((resolve) => {
      this.open = resolve;
    });
  }
}

class Queue<T> {
  readonly items: T[] = [];
  private readonly waiters: Array<(value: T) => void> = [];
  put(value: T): void {
    const waiter = this.waiters.shift();
    if (waiter) waiter(value);
    else this.items.push(value);
  }
  take(): Promise<T> {
    return this.items.length
      ? Promise.resolve(this.items.shift()!)
      : new Promise((resolve) => this.waiters.push(resolve));
  }
}

interface Entry {
  path: Path;
  message: Message;
  sequence: number;
}
class Follower {
  readonly live = new Queue<Entry>();
  readonly sent = new Queue<number>();
  readonly paused = new Gate();
  readonly resume = new Gate();
  readonly stop = new Gate();
  stopped = false;
  done: Promise<void> = Promise.resolve();
  end(): void {
    this.stopped = true;
    this.stop.open();
  }
}

class RecordedWire implements Wire {
  private readonly entries: Entry[] = [];
  private readonly followers = new Map<Follower, number>();
  private appending = false;

  // A callback must be able to reenter the store without append exclusion.
  head(): number {
    if (this.appending) throw new Error('application callback ran under append exclusion');
    return this.entries.length;
  }

  send(path: Path, message: Message): void {
    this.appending = true;
    try {
      const entry = { path: [...path], message, sequence: this.entries.length + 1 };
      this.entries.push(entry);
      for (const [follower, bound] of this.followers) {
        if (follower.live.items.length === bound) {
          this.followers.delete(follower);
          // Signal only: the subscriber writer closes its own carrier outside
          // append and off the producer's stack.
          follower.end();
        } else follower.live.put(entry);
      }
    } finally {
      this.appending = false;
    }
  }
  close(): void {
    for (const follower of this.followers.keys()) follower.end();
    this.followers.clear();
  }

  attach(after: number, target: HandlerRegistry, bound: number, pause: boolean): { head: number; follower: Follower } {
    const follower = new Follower();
    // One synchronous critical section, without an await: append cannot occur
    // between capturing this head and registering its bounded handoff.
    this.appending = true;
    const head = this.entries.length;
    const history = this.entries.slice(after, head);
    this.followers.set(follower, bound);
    this.appending = false;
    follower.done = Promise.resolve().then(async () => {
      const send = (entry: Entry): boolean => {
        if (follower.stopped) return false;
        target.send(entry.path, entry.message);
        follower.sent.put(entry.sequence);
        return true;
      };
      try {
        for (let i = 0; i < history.length; i++) {
          if (!send(history[i]!)) return;
          if (i === 0 && pause) {
            follower.paused.open();
            await Promise.race([follower.resume.promise, follower.stop.promise]);
            if (follower.stopped) return;
          }
        }
        while (!follower.stopped) {
          const entry = await Promise.race([follower.live.take(), follower.stop.promise.then(() => undefined)]);
          if (!entry || !send(entry)) return;
        }
      } finally {
        target.close(1008, 'recorded handoff ended');
      }
    });
    return { head, follower };
  }
}

/** Test root providing queued asynchronous delivery, without a transport API. */
class RecordedRoot implements Endpoint {
  private receiver: Receiver | undefined;
  private readonly queue: Array<{ path: Path; message: Message }> = [];
  private scheduled = false;
  private closed = false;

  send(path: Path, message: Message): void {
    if (this.closed) throw new WireError('closed');
    if (this.queue.length === 16) throw new Error('witness output queue full');
    this.queue.push({ path: [...path], message });
    if (this.scheduled) return;
    this.scheduled = true;
    queueMicrotask(() => {
      this.scheduled = false;
      while (!this.closed && this.queue.length) {
        const entry = this.queue.shift()!;
        this.receiver?.message?.([...entry.path], entry.message);
      }
    });
  }
  receive(receiver: Receiver): () => void {
    if (this.receiver) throw new WireError('receiver_exists');
    this.receiver = receiver;
    return () => {
      if (this.receiver === receiver) this.receiver = undefined;
    };
  }
  close(): void {
    this.closed = true;
    this.queue.length = 0;
    this.receiver = undefined;
  }
}

const message = (value: number): Message => ({ frame: { version: 1, kind: 'event', data: value } });
type Wait = <T>(work: Promise<T>) => Promise<T>;

class Presentation {
  readonly root = new RecordedRoot();
  private readonly end = new RecordedRoot();
  readonly values = new Queue<number>();
  readonly closed = new Queue<number>();
  readonly wire: HandlerRegistry;
  readonly rootDispatch: WireDispatcher;
  private readonly owned: Endpoint[] = [];
  private readonly registries: WireDispatcher[] = [];
  private failure?: unknown;

  constructor(store: RecordedWire) {
    const registry = (endpoint: Endpoint) => {
      const dispatcher = createDispatcher(endpoint);
      this.registries.push(dispatcher);
      return dispatcher;
    };
    this.rootDispatch = registry(this.root);
    const end = registry(this.end);
    const out = mount(new Map([['out', end.select(['destination'])]]));
    const inner = mount(new Map([['in', this.rootDispatch.select(['source'])]]));
    const outer = mount(new Map([['outer', inner]]));
    this.owned.push(out, inner, outer);
    const destination = registry(registry(out).select(['out']));
    this.wire = registry(registry(outer).select(['outer', 'in']));
    destination.register(['tick'], {
      message: (path, message) => {
        try {
          if (path.length !== 1 || path[0] !== 'tick') throw new Error('recorded destination received wrong path');
          store.head(); // Real application reentry, outside append exclusion.
          if (message.frame.kind !== 'event' || typeof message.frame.data !== 'number')
            throw new Error('expected event');
          this.values.put(message.frame.data);
        } catch (error) {
          this.failure = error;
          this.values.put(0);
        }
      },
    });
    this.wire.register(['tick'], {
      message: (path, message) => {
        try {
          destination.send(path, message);
        } catch (error) {
          this.failure = error;
          this.values.put(0);
        }
      },
      closed: (code) => {
        store.head();
        this.closed.put(code);
      },
    });
  }

  async collect(wait: Wait): Promise<number[]> {
    // A fence on the same route follows every earlier replay/live delivery.
    this.wire.send(['tick'], message(0));
    const values: number[] = [];
    for (;;) {
      const value = await wait(this.values.take());
      if (this.failure) throw this.failure;
      if (value === 0) return values;
      values.push(value);
    }
  }
  close(): void {
    for (const registry of [...this.registries].reverse()) registry.close();
    for (const endpoint of [...this.owned].reverse()) endpoint.close();
    this.root.close();
    this.end.close();
  }
}

async function sent(wait: Wait, follower: Follower, last: number): Promise<void> {
  while ((await wait(follower.sent.take())) !== last) {
    /* Wait for the writer's acceptance barrier. */
  }
}

async function headCase(wait: Wait, before: boolean): Promise<unknown> {
  const store = new RecordedWire();
  const first = new Presentation(store);
  const late = new Presentation(store);
  const followers: Follower[] = [];
  try {
    const source = at(mount(new Map([['record', store]])), ['record']);
    for (let value = 1; value <= 3; value++) source.send(['tick'], message(value));
    if (before) source.send(['tick'], message(4));
    const attached = store.attach(0, first.wire, 2, true);
    followers.push(attached.follower);
    await wait(attached.follower.paused.promise);
    // Await producer completion while replay remains at its barrier.
    await wait(
      Promise.resolve().then(() => {
        for (let value = before ? 5 : 4; value <= 5; value++) source.send(['tick'], message(value));
      }),
    );
    attached.follower.resume.open();
    await sent(wait, attached.follower, 5);
    source.send(['tick'], message(6));
    await sent(wait, attached.follower, 6);
    const values = await first.collect(wait);
    const second = store.attach(3, late.wire, 2, false);
    followers.push(second.follower);
    await sent(wait, second.follower, 6);
    return {
      cut: before ? 'append_before_head' : 'head_before_append',
      head: attached.head,
      first: values,
      after_three: await late.collect(wait),
      producer_progress: true,
      callbacks_outside_append: true,
    };
  } finally {
    store.close();
    await Promise.all(followers.map((follower) => follower.done));
    first.close();
    late.close();
  }
}

async function stallCase(wait: Wait): Promise<unknown> {
  const store = new RecordedWire();
  const stalled = new Presentation(store);
  const healthy = new Presentation(store);
  const followers: Follower[] = [];
  try {
    for (let value = 1; value <= 3; value++) store.send(['tick'], message(value));
    const slow = store.attach(0, stalled.wire, 2, true).follower;
    followers.push(slow);
    await wait(slow.paused.promise);
    const fast = store.attach(3, healthy.wire, 2, false).follower;
    followers.push(fast);
    for (let value = 4; value <= 5; value++) {
      store.send(['tick'], message(value));
      await sent(wait, fast, value);
    }
    const queued = slow.live.items.length;
    store.send(['tick'], message(6));
    await sent(wait, fast, 6);
    await wait(slow.done);
    const code = await wait(stalled.closed.take());
    let refused = false;
    try {
      stalled.wire.send(['tick'], message(99));
    } catch (error) {
      refused = error instanceof WireError && error.code === 'closed';
    }
    if (!refused) throw new Error('stalled carrier accepted after close');
    const underneath = new Queue<number>();
    stalled.rootDispatch.register(['probe'], {
      message: (_path, message) => {
        if (message.frame.kind === 'event') underneath.put(message.frame.data as number);
      },
    });
    stalled.root.send(['probe'], message(99));
    const probe = await wait(underneath.take());
    store.send(['tick'], message(7));
    await sent(wait, fast, 7);
    return {
      bound: 2,
      queued_at_bound: queued,
      closed: 1 + stalled.closed.items.length,
      close_code: code,
      healthy: await healthy.collect(wait),
      underneath: [probe],
      head: store.head(),
      producer_progress: true,
    };
  } finally {
    store.close();
    await Promise.all(followers.map((follower) => follower.done));
    stalled.close();
    healthy.close();
  }
}

export async function recordedWireWitness(withinMs: number): Promise<unknown> {
  let timer: NodeJS.Timeout | undefined;
  const deadline = new Promise<never>((_, reject) => {
    timer = setTimeout(() => reject(new Error('recorded wire witness deadline')), withinMs);
  });
  const wait: Wait = (work) => Promise.race([work, deadline]);
  try {
    return { cases: [await headCase(wait, false), await headCase(wait, true)], stalled: await stallCase(wait) };
  } finally {
    clearTimeout(timer);
  }
}
