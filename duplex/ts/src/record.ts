import { encodePath, WireError, type Message, type Path, type Receiver, type Wire } from './wire.ts';

/** An immutable opaque message; replay preserves its original reference scope. */
export interface WireRecord {
  readonly sequence: number;
  readonly path: Path;
  readonly message: Message;
}
/**
 * Consumer storage with one exclusive RecordedWire writer. Append assigns the
 * next contiguous positive safe integer; zero is the empty head. Read runs
 * concurrently with append. Operations honor cancellation; records stay
 * immutable. Storage supplies persistence, never a new capability lifetime.
 */
export interface WireLog {
  head(signal: AbortSignal): Promise<number>;
  append(path: Path, message: Message, signal: AbortSignal): Promise<number>;
  read(sequence: number, signal: AbortSignal): Promise<WireRecord>;
}
export class RecordError extends Error {
  readonly code: 'sequence' | 'overflow';
  constructor(code: RecordError['code']) {
    super(`Record ${code}.`);
    this.name = 'RecordError';
    this.code = code;
  }
}
function sequence(value: number): boolean {
  return Number.isSafeInteger(value) && value >= 0;
}
/** Retains immutable messages and local capabilities without serializing them. */
export class MemoryWireLog implements WireLog {
  private readonly entries: WireRecord[] = [];
  async head(signal: AbortSignal): Promise<number> {
    signal.throwIfAborted();
    return this.entries.length;
  }
  async append(path: Path, message: Message, signal: AbortSignal): Promise<number> {
    signal.throwIfAborted();
    const next = this.entries.length + 1;
    if (!sequence(next)) throw new RecordError('sequence');
    this.entries.push({ sequence: next, path: [...path], message });
    return next;
  }
  async read(at: number, signal: AbortSignal): Promise<WireRecord> {
    signal.throwIfAborted();
    if (!sequence(at) || at === 0 || at > this.entries.length) throw new RecordError('sequence');
    const entry = this.entries[at - 1]!;
    return { ...entry, path: [...entry.path] };
  }
}
export interface RecordOptions {
  /** One bound for writer admission and each live handoff; defaults to 64. */
  readonly maxQueuedMessages?: number;
  /** Called asynchronously after the recorded carrier ends, outside send. */
  readonly onClose?: (error: unknown | undefined) => void;
}
type Command = { readonly path: Path; readonly message: Message } | { readonly control: (head: number) => void };
/** Bounded append-before-forward composition. Successful send is admission only. */
export interface RecordedWire extends Wire {
  /** Fences earlier admissions, without promising delivery at the target. */
  head(signal?: AbortSignal): Promise<number>;
  /** Replay (after,head], then the live handoff. The signal owns this follower. */
  follow(after: number, target: Wire, signal?: AbortSignal): Promise<Follower>;
}
/** Owns one target carrier, replay worker and bounded live handoff. */
export interface Follower {
  readonly head: number;
  /** Resolves after writer and carrier cleanup, including on failure. */
  readonly done: Promise<void>;
  readonly error: unknown | undefined;
  close(): void;
}

/**
 * Takes exclusive append ownership of log. Setup reads its initial head.
 * The target is this composition's carrier and closes when the recorder ends.
 * Messages must remain immutable after admission. This function never infers
 * ownership or translates references inside opaque frames.
 */
export async function record(target: Wire, log: WireLog, options: RecordOptions = {}): Promise<RecordedWire> {
  const bound = options.maxQueuedMessages ?? 64;
  if (!Number.isSafeInteger(bound) || bound < 1) throw new Error('Record queue bound must be a positive safe integer.');
  const life = new AbortController();
  let head = await log.head(life.signal);
  if (!sequence(head)) throw new RecordError('sequence');
  const queue: Command[] = [];
  const followers = new Set<Following>();
  let closed = false;
  let running = false;
  function end(code: number, reason: string, error: unknown) {
    if (closed) return;
    closed = true;
    life.abort(new WireError('closed'));
    queue.length = 0;
    queueMicrotask(() => {
      for (const follower of [...followers]) follower.end(code, reason, error);
      target.close(code, reason);
      options.onClose?.(error);
    });
  }
  async function run() {
    try {
      while (queue.length && !closed) {
        const command = queue.shift()!;
        if ('control' in command) {
          command.control(head);
          continue;
        }
        const next = await log.append(command.path, command.message, life.signal);
        if (closed) return;
        if (next !== head + 1 || !sequence(next)) throw new RecordError('sequence');
        head = next;
        const entry = { sequence: head, path: command.path, message: command.message };
        for (const follower of [...followers])
          if (!follower.offer(entry)) follower.end(1008, 'Record handoff queue is full.', new RecordError('overflow'));
        target.send(entry.path, entry.message);
      }
    } catch (error) {
      end(1011, 'Record storage or target failed.', error);
    } finally {
      running = false;
    }
  }
  function admit(command: Command) {
    if (closed) throw new WireError('closed');
    if (queue.length >= bound) {
      const error = new RecordError('overflow');
      end(1008, 'Record queue is full.', error);
      throw error;
    }
    queue.push(command);
    if (!running) {
      running = true;
      queueMicrotask(() => void run());
    }
  }
  function control<T>(action: (head: number) => T, signal?: AbortSignal): Promise<T> {
    return new Promise<T>((resolve, reject) => {
      if (signal?.aborted) {
        reject(signal.reason);
        return;
      }
      let pending = true;
      const cleanup = () => {
        pending = false;
        signal?.removeEventListener('abort', aborted);
        life.signal.removeEventListener('abort', ended);
      };
      const aborted = () => {
        cleanup();
        reject(signal!.reason);
      };
      const ended = () => {
        cleanup();
        reject(new WireError('closed'));
      };
      signal?.addEventListener('abort', aborted, { once: true });
      life.signal.addEventListener('abort', ended, { once: true });
      try {
        admit({
          control: (cut) => {
            if (!pending) return;
            cleanup();
            try {
              resolve(action(cut));
            } catch (error) {
              reject(error);
            }
          },
        });
      } catch (error) {
        cleanup();
        reject(error);
      }
    });
  }
  class Following implements Follower {
    readonly head: number;
    readonly done: Promise<void>;
    error: unknown | undefined;
    private readonly target: Wire;
    private readonly life = new AbortController();
    private readonly live: WireRecord[] = [];
    private stopped = false;
    private wake: (() => void) | undefined;
    private readonly carrierClosed: Promise<void>;
    private finishCarrier!: () => void;
    private readonly parentSignal: AbortSignal | undefined;
    private readonly aborted: () => void;
    constructor(cut: number, after: number, target: Wire, signal?: AbortSignal) {
      this.head = cut;
      this.target = target;
      this.parentSignal = signal;
      this.aborted = () => this.end(1000, 'Follow cancelled.', signal!.reason);
      this.carrierClosed = new Promise((resolve) => {
        this.finishCarrier = resolve;
      });
      signal?.addEventListener('abort', this.aborted, { once: true });
      followers.add(this);
      this.done = Promise.resolve().then(() => this.run(after));
    }
    close() {
      this.end(1000, 'Follow closed.', undefined);
    }
    offer(entry: WireRecord): boolean {
      if (this.stopped) return true;
      if (this.live.length >= bound) return false;
      this.live.push(entry);
      this.wake?.();
      this.wake = undefined;
      return true;
    }
    end(code: number, reason: string, error: unknown) {
      if (this.stopped) return;
      this.stopped = true;
      this.error = error;
      this.life.abort(error);
      followers.delete(this);
      this.live.length = 0;
      this.wake?.();
      this.wake = undefined;
      this.parentSignal?.removeEventListener('abort', this.aborted);
      queueMicrotask(() => {
        try {
          this.target.close(code, reason);
        } finally {
          this.finishCarrier();
        }
      });
    }
    private send(entry: WireRecord) {
      if (this.stopped) return;
      this.target.send([...entry.path], entry.message);
    }
    private async run(after: number) {
      try {
        for (let at = after + 1; at <= this.head && !this.stopped; at++) {
          const entry = await log.read(at, this.life.signal);
          if (entry.sequence !== at) throw new RecordError('sequence');
          this.send(entry);
        }
        while (!this.stopped) {
          const entry = this.live.shift();
          if (entry) this.send(entry);
          else
            await new Promise<void>((resolve) => {
              this.wake = resolve;
            });
        }
      } catch (error) {
        this.end(1011, 'Record replay or target failed.', error);
      } finally {
        this.end(1000, 'Follow ended.', undefined);
        await this.carrierClosed;
      }
    }
  }
  return {
    send(path: Path, message: Message) {
      encodePath(path);
      admit({ path: [...path], message });
    },
    receive(path: Path, receiver: Receiver) {
      if (closed) throw new WireError('closed');
      return target.receive(path, receiver);
    },
    close(code = 1000, reason = '') {
      end(code, reason, undefined);
    },
    head(signal) {
      return control((cut) => cut, signal);
    },
    follow(after, target, signal) {
      return control((cut) => {
        if (!sequence(after) || after > cut) throw new RecordError('sequence');
        return new Following(cut, after, target, signal);
      }, signal);
    },
  };
}
