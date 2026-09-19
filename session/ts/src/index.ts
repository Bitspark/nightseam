/**
 * A session over the seam's connections: an identity that outlives the
 * connections carrying it. One up connection, the one its machine speaks on;
 * any number of down connections, one per attached consumer, each in a role;
 * one holder of control among them; and a log of every frame the session
 * exchanged, in one order, replayed from a sequence.
 *
 * A connection is whatever the seam calls one — a channel of a tunnel where
 * one connection carries many sessions, a pipe where the machine is this
 * process, a bare socket where a consumer speaks over its own. The relay
 * sends, receives and closes, and asks nothing about what multiplexed it.
 *
 * What lives here is mechanism — what can be stated in terms of the profile
 * and a family's session tier, and is the same for every consumer: routing
 * by kind and method against the family's decides and asks, minting the ids
 * a consumer's request carries on to the machine, the holder as state, the
 * log's shape and an in-memory one. Who may attach, who may take control, how long a lease
 * lasts, where the log is kept: the consumer's, and the registry never asks.
 *
 * The relay reads a frame as the plain object it is and rewrites only `id`.
 * Every other member — the profile's, a trace's, one a later profile adds —
 * it forwards verbatim, by construction rather than by enumeration.
 */
import type { FrameConnection } from '@nightseam/duplex';
import { DuplexError } from '@nightseam/runtime';
import type { Observer, ObserverEvent, Trace } from '@nightseam/runtime';

/**
 * What a session tells its observer, declared into the runtime's registry so
 * that a consumer's `switch (event.type)` covers them beside the runtime's
 * and the tunnel's. Which observer that is follows from the connection its
 * machine speaks over: one that observes through a peer — a tunnel channel,
 * which carries `observe` — is the observer the consumer already chose, and
 * where there is none, `RegistryOptions.observer` is, and where there is
 * neither, nothing is told.
 *
 * They are the changes `onChange` hands a consumer, said the way the runtime
 * says things: an event that concerns a frame carries that frame's trace, and
 * none of them carries a payload — `frame.appended` says which sequence, which
 * direction, whose origin and how many bytes, and never the message.
 */
declare module '@nightseam/runtime' {
  interface ObserverEvents {
    /** A session is bound to the connection its machine speaks on, and stands until that connection closes. */
    'session.bound': { type: 'session.bound'; at: Date; session: string };
    /** The machine's connection closed, so the session is gone and every consumer of it was ended with this close. */
    'session.unbound': { type: 'session.unbound'; at: Date; session: string; code: number; reason: string };
    /** A consumer joined a session in a role, under an origin, resuming from a sequence. */
    'session.attached': { type: 'session.attached'; at: Date; session: string; role: Role; origin: string; after: number };
    /** A consumer left one, by detaching or by its connection closing; the session stands. */
    'session.detached': { type: 'session.detached'; at: Date; session: string; role: Role; origin: string };
    /** The machine sent a request the holder of control must answer; asking is whether the family's session tier counts it in attention. */
    'ask.raised': { type: 'ask.raised'; at: Date; session: string; id: string; method: string; asking: boolean; trace?: Trace };
    /** That request was handed to a holder — when it arrived, and again each time control moved while it stood open. */
    'ask.routed': { type: 'ask.routed'; at: Date; session: string; id: string; method: string; origin: string; trace?: Trace };
    /** The holder answered it, and the answer went to the machine. */
    'ask.answered': { type: 'ask.answered'; at: Date; session: string; id: string; method: string; origin: string; trace?: Trace };
    /** Control of a session moved; an origin absent is control released, left with nobody. */
    'control.changed': { type: 'control.changed'; at: Date; session: string; origin?: string; held: boolean };
    /** A frame was appended to the session's log: its sequence, its direction, the origin of the consumer whose frame it was — none for the machine's own — and its size, never its message. */
    'frame.appended': { type: 'frame.appended'; at: Date; session: string; sequence: number; direction: Direction; origin: string; bytes: number; method?: string; trace?: Trace };
    /** A consumer's frame the relay answered in the machine's place rather than forwarding: not_controlling where control is not held, busy where the session has too many requests open. */
    'session.refused': { type: 'session.refused'; at: Date; session: string; code: string; method: string; role: Role; origin: string; trace?: Trace };
  }
}

/** One frame of the seam, as the connection it travels on declares it. */
type Wire = Parameters<FrameConnection['send']>[0];

/**
 * A connection that observes through something of its own — a tunnel
 * channel, which hands its events to the peer its tunnel runs over. A
 * connection that carries no `observe` carries no such choice, and a session
 * over it takes the registry's observer instead.
 */
interface Observing { observe(event: ObserverEvent): void }

/** Whether a connection observes through one of its own. */
function observes(connection: FrameConnection): connection is FrameConnection & Observing {
  return typeof (connection as Partial<Observing>).observe === 'function';
}

/** One message of the profile, read as the plain object it is. */
type Envelope = Record<string, unknown>;

/**
 * The session's own vocabulary on the wire. A layer that speaks on the wire
 * does it as the tunnel does — ordinary frames of the profile under a prefix
 * the layer reserves, which the peer forwards and reads nothing into
 * (docs/layers.md); `channel.open` and `channel.credit` are the tunnel's.
 * These two are the relay's alone: the relay produces them, a consumer reads
 * them, a machine that sends one has its connection ended, and neither is
 * logged, a session's own frames being state rather than messages of it.
 */
/** What every name of the session's vocabulary begins with, and what no family declares a method or an event under. */
export const PREFIX = 'session.';
/** Who holds control: `{holder: "<origin>"}`, or `{holder: null}` where nobody does. Every attachment is sent it when control changes, and a consumer once on attach, before its replay. */
export const CONTROL_EVENT = 'session.control';
/** Where in the session's one order the frame delivered just before it stood: `{sequence: N}`, the log's own sequence, which is what a consumer resumes from. A frame the log cut is delivered as nothing and carries none. */
export const CURSOR_EVENT = 'session.cursor';

/** A family's session tier, as the generated client states it. */
export interface Governance {
  /** Whether a method needs control to send. */
  decides(method: string): boolean;
  /** Whether a method the machine sends raises a request the holder of control must answer. */
  asks(method: string): boolean;
}

/** What an attached consumer may do: a participant may hold control and decide while it does; an observer never decides. */
export type Role = 'participant' | 'observer';

/** Where a frame went: up, to the machine, or down, from it. */
export type Direction = 'up' | 'down';

/** One message a session exchanged, verbatim, in one order, with the provenance its sender was attached under. */
export interface Frame {
  sequence: number;
  direction: Direction;
  /** The origin its consumer attached under; a frame from the machine carries none. */
  origin: string;
  at: Date;
  /** The message as it was forwarded, ids and all — or, when it was over the log's bound, the text it was cut to. */
  message: unknown;
  truncated: boolean;
}

/**
 * The consumer's store of a session's frames; the package ships an in-memory
 * one. A log handed to `bind` that already holds frames is bound at its head:
 * `bind` reads it once, through `replay` from after zero, and the session goes
 * on from the last sequence that read delivered.
 */
export interface Log {
  /** Records a frame and returns the sequence it was given, which counts from one. */
  append(frame: Frame): Promise<number>;
  /**
   * Delivers every frame after a sequence, in ascending sequence order,
   * waiting on each. The order is the contract rather than a convenience of
   * the in-memory log's: it is what makes the last sequence `bind`'s read is
   * given the log's head, so a log that delivers out of order binds its
   * session below its own end.
   */
  replay(after: number, deliver: (frame: Frame) => Promise<void>): Promise<void>;
}

/**
 * A log in memory, bounded per frame: a message whose JSON is longer than
 * maxFrameBytes is stored as the text it was cut to and replayed truncated,
 * so that one frame cannot grow a long-lived session without bound.
 */
export function memoryLog(maxFrameBytes: number): Log {
  const bound = positive(maxFrameBytes, 'maxFrameBytes');
  const frames: Frame[] = [];
  return {
    append(frame: Frame): Promise<number> {
      const bytes = encoder.encode(JSON.stringify(frame.message ?? null));
      const over = bytes.byteLength > bound;
      const sequence = frames.length + 1;
      frames.push({ ...frame, sequence, truncated: over, message: over ? decoder.decode(bytes.subarray(0, bound)) : frame.message });
      return Promise.resolve(sequence);
    },
    async replay(after: number, deliver: (frame: Frame) => Promise<void>): Promise<void> {
      // A frame's sequence is its place in the log, so what follows `after` begins there.
      for (const frame of frames.slice(Math.max(after, 0))) await deliver(frame);
    },
  };
}

/**
 * What a registry did to a session: one member per domain change it makes,
 * the same set in both languages. A consumer builds its own events from
 * these — an attention list, a change feed — and this is the one place they
 * are computed.
 */
export type ChangeKind =
  | 'bound'
  | 'unbound'
  | 'attached'
  | 'detached'
  | 'ask_raised'
  | 'ask_routed'
  | 'ask_answered'
  | 'control_changed'
  | 'frame_appended'
  | 'refused';

/**
 * One domain change, as the registry computed it. It says which session, who
 * it concerns, which method and which frame — names, ids and sequences, and
 * never a payload: what a frame carried is the log's, not a hook's.
 */
export interface Change {
  at: Date;
  /** The session it is a change of. */
  session: string;
  kind: ChangeKind;
  /** The consumer it concerns: the one attached or detached, given control, asked, answering, or refused. A change of the session itself names none. */
  attachment?: Attachment;
  /** The sequence the log reached, for a frame appended; the sequence a consumer resumed from, for one attached. */
  sequence?: number;
  /** The method of the frame it concerns, where that frame names one. */
  method?: string;
  /** The trace of the frame it concerns, as that frame carried it. */
  trace?: Trace;
}

/** What a change carries beyond its kind, which the relay knows and the registry stamps the rest onto. */
type Of = Omit<Change, 'at' | 'session' | 'kind'>;

/** How a registry is made; every member is optional and takes its default, and a member that is not a positive integer is refused with `invalid_options`. */
export interface RegistryOptions {
  /** How many consumers may be attached to one session at once; an attach beyond it is refused. Default: 64. */
  maxAttachments?: number;
  /** How many requests a session may have open towards its machine at once; one beyond it is refused with busy. Default: 256. */
  maxInflight?: number;
  /**
   * Where a session bound over a connection that observes through nothing of
   * its own — a pipe, a bare socket, an in-process machine — tells what it
   * does. A connection that carries `observe`, a tunnel channel being the
   * one that does, tells through that instead and never this: it is the
   * observer the consumer already chose. Default: none, which observes
   * nothing and costs nothing.
   */
  observer?: Observer;
}

/** The bounds a registry settled on, which is what `limit` reads back: an observer is a choice rather than a bound and is not among them. */
type Limits = Required<Pick<RegistryOptions, 'maxAttachments' | 'maxInflight'>>;

/** The live sessions of one process: an id to the relay that carries it. */
export class Registry {
  private readonly limits: Limits;
  /** Where a session of this registry tells what it does when its up connection observes through nothing of its own. */
  private readonly watcher?: Observer;
  private readonly relays = new Map<string, Relay>();
  /** One entry per registration rather than one per function, so that the same hook registered twice is stopped once per registration. */
  private readonly hooks = new Set<{ fn: (change: Change) => void }>();

  constructor(options: RegistryOptions = {}) {
    this.limits = {
      maxAttachments: positive(options.maxAttachments ?? 64, 'maxAttachments'),
      maxInflight: positive(options.maxInflight ?? 256, 'maxInflight'),
    };
    if (options.observer !== undefined) this.watcher = options.observer;
  }

  /**
   * Binds a session to the channel its machine speaks on, governed by its
   * family's session tier and logged to `log`; the session lives until that
   * channel closes. The log is read once here, from its beginning, and the
   * session goes on from its head: a durable log bound with frames already in
   * it replays them to a consumer that attaches after nothing, rather than
   * waiting for the machine to speak for the session to learn where it is.
   *
   * The connection is the seam's — a channel of a tunnel, a pipe, a bare
   * socket — and where the session tells what it does follows from it: one
   * that carries `observe` tells through that, one that does not tells
   * `RegistryOptions.observer`, and where there is neither, nothing.
   */
  bind(id: string, up: FrameConnection, governance: Governance, log: Log): void {
    if (typeof id !== 'string' || id === '') throw new DuplexError('session_invalid', 'A session is bound under an id.');
    if (this.relays.has(id)) throw new DuplexError('session_exists', `Session ${id} is bound.`);
    const relay = new Relay(this, id, up, governance, log);
    this.relays.set(id, relay);
    relay.listen();
  }

  /** Attaches a consumer's connection to a bound session in a role, stamping what it sends with `origin`, after replaying the log from `after`. */
  attach(id: string, down: FrameConnection, role: Role, origin: string, after: number): Attachment {
    if (role !== 'participant' && role !== 'observer') throw new DuplexError('role_invalid', 'A consumer attaches as a participant or an observer.');
    if (typeof origin !== 'string') throw new DuplexError('origin_invalid', 'An origin is the caller\'s fact about the consumer, as text.');
    if (!Number.isInteger(after) || after < 0) throw new DuplexError('sequence_invalid', 'after must be a sequence.');
    return this.relay(id).attach(down, role, origin, after);
  }

  /** Gives control of a session to one of its attached participants, or releases it with null; a request the machine has open follows to the new holder. */
  control(id: string, holder: Attachment | null): void {
    this.relay(id).control(holder);
  }

  /** The sessions with an open ask: a request the machine sent that the holder of control has not answered. */
  attention(): string[] {
    return [...this.relays.values()].filter(relay => relay.asking()).map(relay => relay.id);
  }

  /**
   * Every domain change of every session bound here, as the registry makes
   * it, in the order it makes them. The returned function stops exactly this
   * registration and no other, and a hook that throws interrupts nothing: a
   * session goes on whatever a consumer's hook does with what it hears.
   */
  onChange(fn: (change: Change) => void): () => void {
    const hook = { fn };
    this.hooks.add(hook);
    return () => { this.hooks.delete(hook); };
  }

  /** @internal */
  changed(session: string, kind: ChangeKind, of: Of = {}): void {
    if (this.hooks.size === 0) return;
    // A member a change does not concern is absent rather than undefined: a
    // change says what it is about and nothing more.
    const change: Change = { at: new Date(), session, kind };
    if (of.attachment) change.attachment = of.attachment;
    if (of.sequence !== undefined) change.sequence = of.sequence;
    if (of.method !== undefined) change.method = of.method;
    if (of.trace) change.trace = of.trace;
    for (const hook of [...this.hooks]) {
      try { hook.fn(change); } catch { /* A hook cannot interrupt the session it hears about. */ }
    }
  }

  private relay(id: string): Relay {
    const relay = this.relays.get(id);
    if (!relay) throw new DuplexError('no_session', `No session ${id} is bound.`);
    return relay;
  }

  /** @internal */
  get limit(): Readonly<Limits> { return this.limits; }

  /** @internal Where a session of this registry tells what it does when its up connection observes through nothing of its own. */
  get observer(): Observer | undefined { return this.watcher; }

  /** @internal */
  forget(id: string): void { this.relays.delete(id); }
}

/** One consumer on a session: the connection it speaks on, the role it attached in and the origin its frames carry. */
export class Attachment {
  readonly role: Role;
  readonly origin: string;
  readonly channel: FrameConnection;
  private readonly relay: Relay;
  /** What the channel handed back when the relay began listening, so that detaching leaves it as it was found. */
  private unlisten?: () => void;
  /** What the relay last told this consumer of the session's own vocabulary: who holds control, and where in the log it stands. */
  private control: string | null = null;
  private cursor = 0;
  /** One entry per registration rather than one per function, so that the same hook registered twice is stopped once per registration. */
  private readonly watching = new Set<{ fn: (holder: string | null) => void }>();

  /** @internal */
  constructor(relay: Relay, channel: FrameConnection, role: Role, origin: string) {
    this.relay = relay;
    this.channel = channel;
    this.role = role;
    this.origin = origin;
  }

  /**
   * Who holds control of the session, as the relay last told this consumer:
   * the origin that consumer was attached under, or null where nobody holds
   * it. It is what the last `session.control` this attachment was sent said,
   * so a consumer reading it here and one reading the wire agree.
   */
  get holder(): string | null { return this.control; }

  /**
   * The log's sequence of the last frame delivered to this consumer — what
   * the last `session.cursor` said — and zero where it has been delivered
   * none. It is what the consumer reattaches after, and the relay's own
   * count rather than one kept by counting frames, which a message the log
   * cut would put out by one.
   */
  get sequence(): number { return this.cursor; }

  /**
   * Asks to be told each time this consumer is sent a `session.control`, and
   * returns the way to stop asking. It is not called with the state the
   * consumer joined at — that frame is sent before there is anywhere to call
   * — and `holder` reads it instead. A hook that throws interrupts nothing.
   */
  onControl(fn: (holder: string | null) => void): () => void {
    const hook = { fn };
    this.watching.add(hook);
    return () => { this.watching.delete(hook); };
  }

  /** @internal What the relay told this consumer of who holds control, and the registrations waiting on it. */
  told(holder: string | null): void {
    this.control = holder;
    for (const hook of [...this.watching]) {
      try { hook.fn(holder); } catch { /* A hook cannot interrupt the session it hears about. */ }
    }
  }

  /** @internal Where the frame just delivered to this consumer stood. */
  at(sequence: number): void { this.cursor = sequence; }

  /** Detaches the consumer: the relay stops carrying its connection, releases control if it held it, and the session stands; the connection is closed with 1000 "detached", as the Go twin closes it, so a consumer learns it was let go. */
  detach(): void {
    this.relay.detach(this);
    if (this.channel.state === 'open') this.channel.close(1000, 'detached');
  }

  /** @internal */
  attachTo(unlisten: () => void): void { this.unlisten = unlisten; }

  /** @internal */
  release(): void { this.unlisten?.(); this.unlisten = undefined; }
}

/** A consumer's request the machine has not answered: who sent it and the id it knows it by; the id the relay minted is what it is filed under. */
interface Inflight { at: Attachment; id: string }

/** A request the machine sent: the message as it arrived, whether its method asks, and which consumer it stands with, if any. */
interface Open { message: Envelope; asking: boolean; at: Attachment | null }

/**
 * One bound session: the up connection, the consumers attached to it, the
 * holder of control, and the log. Every frame the relay handles runs on one
 * queue, so that what the log records and what a channel receives are in the
 * same order — including a replay, which is queued before the frames of the
 * consumer that asked for it.
 */
class Relay {
  readonly id: string;
  private readonly registry: Registry;
  private readonly up: FrameConnection;
  private readonly governance: Governance;
  private readonly log: Log;
  private readonly attachments = new Set<Attachment>();
  private holder: Attachment | null = null;
  /** A consumer's request the machine has not answered: the id the relay minted, to the consumer and the id it knows it by. */
  private readonly inflight = new Map<string, Inflight>();
  /** The machine's requests no consumer has answered, in the order they arrived. */
  private readonly open = new Map<string, Open>();
  /**
   * Every peer mints `c:N` per connection, so two consumers attached over a
   * session's life both send `c:1`. The relay is the family's client towards
   * the machine and mints the ids it sends up itself — `c:N`, unique per
   * session — keeping which consumer's request each stands for, and the log
   * records the minted one. The machine's own ids, `s:N`, are one peer's and
   * travel down as they are.
   */
  private upNext = 0;
  /** The sequence the log has reached: what a consumer attaching now is replayed up to, and no further. */
  private sequence = 0;
  /** Whether the cursor above has been seated from the log, which is the first step of the queue and so runs before any frame is recorded. */
  private seated = false;
  private queue: Promise<void> = Promise.resolve();
  private ended = false;

  constructor(registry: Registry, id: string, up: FrameConnection, governance: Governance, log: Log) {
    this.registry = registry;
    this.id = id;
    this.up = up;
    // Which observer a session has follows from the connection its machine
    // speaks over and is settled here, once: the connection's own where it
    // has one, then the registry's, then none — a connection that observes
    // through something of its own never falls back to the registry's, that
    // something being the observer the consumer already chose.
    const watcher = registry.observer;
    this.tell = observes(up) ? event => up.observe(event)
      : watcher ? event => { try { watcher.observe(event); } catch { /* An observer cannot interrupt the session it hears about. */ } }
        : () => { /* No peer and no registry observer is the no-op an observer already means. */ };
    this.governance = governance;
    this.log = log;
  }

  /**
   * seat places the cursor at the log's head, so that a session bound over a
   * log that already holds frames goes on from its end rather than from
   * nothing: a consumer attaching before the machine has spoken is replayed
   * what the log holds. It reads the log once, from after zero, and takes the
   * last sequence `replay` delivered, which is the head because `replay`
   * delivers in ascending sequence order.
   *
   * It is the first step of the relay's queue, put there before the machine's
   * channel is listened to: a frame the machine sends while the cursor is
   * being seated is recorded behind the read, above the head, rather than
   * under a sequence the log has already given out. A `Log` that knows its
   * head without a read may later say so as a member the relay prefers where
   * a log has one, which leaves every existing `Log` valid and this read what
   * a log without it is bound by.
   */
  private async seat(): Promise<void> {
    let head = 0;
    await this.log.replay(0, frame => { head = frame.sequence; return Promise.resolve(); });
    this.sequence = head;
    this.seated = true;
  }

  /** @internal */
  listen(): void {
    this.run(() => this.seat());
    this.up.listen({
      frame: frame => this.run(() => this.fromUp(frame)),
      close: (code, reason) => this.run(() => this.end(code, reason)),
    });
    this.change('bound');
    this.observe({ type: 'session.bound', at: new Date(), session: this.id });
  }

  attach(down: FrameConnection, role: Role, origin: string, after: number): Attachment {
    if (this.ended) throw new DuplexError('no_session', `No session ${this.id} is bound.`);
    if (this.attachments.size >= this.registry.limit.maxAttachments) throw new DuplexError('too_many_attachments', 'No room for another consumer on this session.');
    const attachment = new Attachment(this, down, role, origin);
    this.attachments.add(attachment);
    // The replay is queued before the channel is listened to, so that what
    // the consumer missed reaches it before anything live, in one order, and
    // it stops where the session stood when the consumer was added: a frame
    // recorded since reaches it live instead, once.
    // Until the cursor is seated no frame has been recorded — every record
    // runs on the queue behind the seat — so a consumer that attaches that
    // early is replayed up to the head the seat finds, read where the replay
    // runs rather than pinned to the zero the cursor still stands at.
    const pinned = this.seated ? this.sequence : undefined;
    // Who holds control as this consumer joined, taken here rather than
    // where the replay runs, so that control moving meanwhile is the change
    // it is told next rather than the state it is told it joined at.
    const joined = this.holder;
    this.run(() => {
      // Who holds control is the first thing on the connection, before the
      // replay and before any frame of the family, so that the consumer
      // knows the state it joined before it reads what it missed.
      this.tellControl([attachment], joined);
      const ceiling = pinned ?? this.sequence;
      return this.log.replay(after, async frame => {
        // A cut message is not a message: what the log truncated is there for a
        // consumer that reads the log, not for a channel that speaks the family.
        // It carries no cursor either, the next one naming the next sequence.
        if (frame.sequence > ceiling || frame.truncated) return;
        this.deliver(attachment, frame.sequence, frame.message);
      });
    });
    attachment.attachTo(down.listen({
      frame: frame => this.run(() => this.fromDown(attachment, frame)),
      close: () => this.run(() => this.drop(attachment)),
    }));
    // The sequence a consumer attached from is the one fact about it the
    // attachment does not carry, so the change says what it resumed from.
    this.change('attached', { attachment, sequence: after });
    this.observe({ type: 'session.attached', at: new Date(), session: this.id, role, origin, after });
    return attachment;
  }

  control(holder: Attachment | null): void {
    if (holder !== null) {
      if (!this.attachments.has(holder)) throw new DuplexError('not_attached', 'Control is held by a consumer attached to the session.');
      if (holder.role !== 'participant') throw new DuplexError('not_controlling', 'An observer never holds control.');
    }
    this.holder = holder;
    // Whom to tell is taken where control moved, so that a consumer
    // attaching after this is told by its own attach rather than twice.
    const attached = [...this.attachments];
    this.change('control_changed', holder ? { attachment: holder } : {});
    this.observe({ type: 'control.changed', at: new Date(), session: this.id, ...(holder ? { origin: holder.origin } : {}), held: holder !== null });
    // Every consumer is told who holds control, and before the request the
    // machine is waiting on follows it: the consumer it stood with no longer
    // answers it, and whoever holds control now is asked it afresh.
    this.run(() => { this.tellControl(attached, holder); this.reroute(); });
  }

  asking(): boolean {
    for (const open of this.open.values()) if (open.asking) return true;
    return false;
  }

  detach(attachment: Attachment): void { this.run(() => this.drop(attachment)); }

  /** change tells the registry's hooks one domain change of this session, which the registry stamps with the session and the time. */
  private change(kind: ChangeKind, of: Of = {}): void { this.registry.changed(this.id, kind, of); }

  /** What this session tells its events to, settled where it was bound; an observer that is absent is told nothing and costs nothing. */
  private readonly tell: (event: ObserverEvent) => void;

  /** observe tells this session's observer one event of its own. */
  private observe(event: ObserverEvent): void { this.tell(event); }

  /** run puts one step on the relay's queue; a step that throws leaves the session standing. */
  private run(step: () => void | Promise<void>): void {
    this.queue = this.queue.then(step).catch(() => { /* A channel that failed is dropped where it failed. */ });
  }

  /** A frame from a consumer: what it may send depends on its role and on who holds control. */
  private async fromDown(attachment: Attachment, frame: Wire): Promise<void> {
    if (!this.attachments.has(attachment)) return;
    // What kind of frame it is comes before what it says: a frame that is
    // not text carries no message of the profile at all, which is
    // unsupported data and not a message the profile refuses.
    if (frame.kind !== 'text') {
      attachment.channel.close(1003, BINARY_FRAME);
      return;
    }
    const envelope = read(frame);
    if (!envelope) {
      attachment.channel.close(1008, 'a frame that is not a message of the profile');
      return;
    }
    const id = typeof envelope.id === 'string' ? envelope.id : undefined;
    switch (envelope.kind) {
      case 'request': {
        const method = typeof envelope.method === 'string' ? envelope.method : '';
        if (this.governance.decides(method) && this.holder !== attachment) {
          // The consumer may not decide, so the relay answers in the machine's
          // place; nothing of the request reaches it or the log.
          if (id) this.deliver(attachment, 0, { version: 1, kind: 'response', id, error: { code: 'not_controlling', message: 'Only the holder of control decides on this session.' } });
          this.refuse('not_controlling', attachment, method, envelope);
          return;
        }
        if (!id) return;
        if (this.inflight.size >= this.registry.limit.maxInflight) {
          this.deliver(attachment, 0, { version: 1, kind: 'response', id, error: { code: 'busy', message: 'The session has too many requests open.' } });
          this.refuse('busy', attachment, method, envelope);
          return;
        }
        const minted = 'c:' + ++this.upNext;
        this.inflight.set(minted, { at: attachment, id });
        await this.sendUp(attachment, { ...envelope, id: minted });
        return;
      }
      case 'cancel': {
        // A cancel decides: it withdraws what the holder asked for.
        if (this.holder !== attachment || !id) return;
        for (const [minted, held] of this.inflight) {
          if (held.at !== attachment || held.id !== id) continue;
          this.inflight.delete(minted);
          await this.sendUp(attachment, { ...envelope, id: minted });
          return;
        }
        return;
      }
      case 'response': {
        // Answering what the machine asked decides, so only the consumer the
        // request stands with answers it, under the id the machine gave it.
        if (!id) return;
        const open = this.open.get(id);
        if (!open || open.at !== attachment) return;
        this.open.delete(id);
        const answered = named(open.message) ?? '';
        const trace = traceOf(envelope);
        this.change('ask_answered', { attachment, method: answered, trace });
        this.observe({ type: 'ask.answered', at: new Date(), session: this.id, id, method: answered, origin: attachment.origin, ...(trace ? { trace } : {}) });
        await this.sendUp(attachment, { ...envelope });
        return;
      }
      default:
        // An event names no method, so nothing about it decides.
        await this.sendUp(attachment, { ...envelope });
    }
  }

  /** A frame from the machine: recorded as the machine sent it, then a response to the one consumer that asked, an event to all of them, a request to the holder. */
  private async fromUp(frame: Wire): Promise<void> {
    // As on a consumer's connection: the frame's kind is read before what
    // it says, a frame that is not text being unsupported data rather than
    // a message the profile refuses.
    if (frame.kind !== 'text') {
      this.up.close(1003, BINARY_FRAME);
      return;
    }
    const envelope = read(frame);
    if (!envelope) {
      this.up.close(1008, 'a frame that is not a message of the profile');
      return;
    }
    // The session's own vocabulary is the relay's to produce: a machine that
    // sends one speaks for the layer above it, which is no frame of the
    // family and ends the connection as a malformed one does. It is refused
    // before the log, being no message of the session either.
    const names = nameOf(envelope);
    if (names.startsWith(PREFIX)) {
      this.up.close(1002, `a machine does not send ${names}`);
      return;
    }
    // The log is the session's conversation with its machine: every frame
    // the machine sent is recorded once, as it sent it — under the id the
    // session asked with, whether or not there is a consumer to hand it to.
    const { sequence, attached } = await this.record('down', null, envelope);
    const id = typeof envelope.id === 'string' ? envelope.id : undefined;
    switch (envelope.kind) {
      case 'response': {
        if (!id) return;
        const held = this.inflight.get(id);
        if (!held) return;
        this.inflight.delete(id);
        this.deliver(held.at, sequence, { ...envelope, id: held.id });
        return;
      }
      case 'request': {
        // What the machine asks is the holder's to answer, and waits while
        // there is none; asks names the ones a session is waiting on, which
        // is what attention counts.
        if (!id) return;
        const method = typeof envelope.method === 'string' ? envelope.method : '';
        const trace = traceOf(envelope);
        this.open.set(id, { message: envelope, asking: this.governance.asks(method), at: this.holder });
        this.change('ask_raised', { method, trace });
        this.observe({ type: 'ask.raised', at: new Date(), session: this.id, id, method, asking: this.governance.asks(method), ...(trace ? { trace } : {}) });
        if (this.holder) {
          this.deliver(this.holder, sequence, envelope);
          this.routed(id, method, this.holder, trace);
        }
        return;
      }
      case 'cancel': {
        if (!id) return;
        const open = this.open.get(id);
        if (!open) return;
        this.open.delete(id);
        if (open.at) this.deliver(open.at, sequence, envelope);
        return;
      }
      default:
        // An event reaches every attached consumer, and the log once.
        for (const attachment of attached) this.deliver(attachment, sequence, envelope);
    }
  }

  /** reroute hands every request the machine is waiting on to whoever holds control now; released, it stands with nobody until control is given again. */
  private reroute(): void {
    const holder = this.holder;
    for (const open of this.open.values()) open.at = holder;
    if (!holder) return;
    // The frame the new holder is asked is the one the machine sent, which
    // the log already holds: routing a frame again is no second frame — but
    // it is a change of who stands with it, which is what an attention list
    // is about. It is no new place in the order either, so it carries no
    // cursor and never moves the new holder's backwards.
    for (const [id, open] of this.open) {
      this.deliver(holder, 0, open.message);
      this.routed(id, named(open.message) ?? '', holder, traceOf(open.message));
    }
  }

  /** sendUp records a consumer's frame and writes it to the machine, in the order it recorded them. */
  private async sendUp(from: Attachment, envelope: Envelope): Promise<void> {
    await this.record('up', from, envelope);
    this.write(this.up, envelope);
  }

  /** record appends a frame to the session's log and returns its sequence and the consumers there were when that sequence was assigned; one attaching between the two is not among them and takes the frame from the log's replay instead. A frame from the machine is nobody's, and carries no origin. The sequence is what a consumer the frame is delivered to is told its cursor stands at. */
  private async record(direction: Direction, from: Attachment | null, envelope: Envelope): Promise<{ sequence: number; attached: Attachment[] }> {
    const origin = from?.origin ?? '';
    const sequence = await this.log.append({ sequence: 0, direction, origin, at: new Date(), message: envelope, truncated: false });
    this.sequence = sequence;
    const method = named(envelope);
    const trace = traceOf(envelope);
    this.change('frame_appended', { sequence, attachment: from ?? undefined, method, trace });
    // What the observer is told of a frame is its size and never its bytes:
    // the message is the log's, which is the one place a session keeps one.
    this.observe({ type: 'frame.appended', at: new Date(), session: this.id, sequence, direction, origin, bytes: encoder.encode(JSON.stringify(envelope)).byteLength, ...(method !== undefined ? { method } : {}), ...(trace ? { trace } : {}) });
    return { sequence, attached: [...this.attachments] };
  }

  /**
   * deliver hands one frame to a consumer and, where that frame has a place
   * in the session's log, the cursor that names it, in that order and with
   * nothing between them.
   *
   * A frame with no place — the relay's own refusal, a request handed again
   * as control moves — carries none: a cursor says where a consumer stands,
   * and a frame it has already been given is no new place to stand.
   */
  private deliver(attachment: Attachment, sequence: number, message: unknown): void {
    this.write(attachment.channel, message);
    if (sequence <= 0) return;
    this.write(attachment.channel, { version: 1, kind: 'event', event: CURSOR_EVENT, data: { sequence } });
    attachment.at(sequence);
  }

  /** tellControl sends the session's control event to every consumer it names and sets the state it says, which is what makes who holds control something a consumer has rather than something only an observer of the service is given. */
  private tellControl(attached: Iterable<Attachment>, holder: Attachment | null): void {
    const named = holder ? holder.origin : null;
    for (const attachment of attached) {
      this.write(attachment.channel, { version: 1, kind: 'event', event: CONTROL_EVENT, data: { holder: named } });
      attachment.told(named);
    }
  }

  /** routed says one request of the machine's stands with one consumer — where it arrived, and again wherever control moved while it stood open. */
  private routed(id: string, method: string, holder: Attachment, trace: Trace | undefined): void {
    this.change('ask_routed', { attachment: holder, method, trace });
    this.observe({ type: 'ask.routed', at: new Date(), session: this.id, id, method, origin: holder.origin, ...(trace ? { trace } : {}) });
  }

  /** refuse records what the relay answered in the machine's place: the frame reached neither the machine nor the log, and is a change all the same. */
  private refuse(code: string, attachment: Attachment, method: string, envelope: Envelope): void {
    const trace = traceOf(envelope);
    this.change('refused', { attachment, method, trace });
    this.observe({ type: 'session.refused', at: new Date(), session: this.id, code, method, role: attachment.role, origin: attachment.origin, ...(trace ? { trace } : {}) });
  }

  /** write hands a message to a connection; one that refuses it has closed, and its own close detaches it. */
  private write(channel: FrameConnection, message: unknown): void {
    try { channel.send({ kind: 'text', data: JSON.stringify(message) }); } catch { /* The channel's close is what detaches it. */ }
  }

  /** drop detaches one consumer: the session stands, and what it left open stands with it. */
  private drop(attachment: Attachment): void {
    if (!this.attachments.delete(attachment)) return;
    attachment.release();
    // A consumer that left holding control leaves the session with nobody
    // holding it, which is the same change Control releasing it makes: a
    // hook cannot tell the two apart from a detachment alone.
    if (this.holder === attachment) {
      this.holder = null;
      this.change('control_changed');
      this.observe({ type: 'control.changed', at: new Date(), session: this.id, held: false });
      // The consumers still attached are told control stands with nobody,
      // which is the change releasing it by hand makes.
      this.tellControl([...this.attachments], null);
    }
    for (const [minted, held] of this.inflight) if (held.at === attachment) this.inflight.delete(minted);
    for (const open of this.open.values()) if (open.at === attachment) open.at = null;
    this.change('detached', { attachment });
    this.observe({ type: 'session.detached', at: new Date(), session: this.id, role: attachment.role, origin: attachment.origin });
  }

  /** end ends the session: the machine's connection closed, so every consumer's ends with the same close. */
  private end(code: number, reason: string): void {
    if (this.ended) return;
    this.ended = true;
    this.registry.forget(this.id);
    for (const attachment of [...this.attachments]) {
      this.attachments.delete(attachment);
      attachment.release();
      attachment.channel.close(code, reason);
    }
    this.holder = null;
    this.inflight.clear();
    this.open.clear();
    this.change('unbound');
    this.observe({ type: 'session.unbound', at: new Date(), session: this.id, code, reason });
  }
}

const encoder = new TextEncoder();
const decoder = new TextDecoder();

/**
 * What a session's connections are closed with when a frame of the wrong
 * kind arrives on one: 1003, unsupported data, in both languages. Text that
 * is no message of the profile is a different fault and carries a different
 * code — 1008 here, and the reason that names it.
 */
const BINARY_FRAME = 'a session speaks JSON text frames';

/** read is a frame as the message it carries, or nothing when it carries none: the profile is JSON text. Its caller has already refused a frame that is not text, and the guard below is what makes that a type the body may read. */
function read(frame: Wire): Envelope | undefined {
  if (frame.kind !== 'text') return undefined;
  let value: unknown;
  try { value = JSON.parse(frame.data); } catch { return undefined; }
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return undefined;
  return value as Envelope;
}

/** What a frame names: an event's name, a request's method; a response and a cancel name nothing. It is what the reserved prefix is read off. */
function nameOf(envelope: Envelope): string {
  const event = typeof envelope.event === 'string' ? envelope.event : '';
  if (event) return event;
  return typeof envelope.method === 'string' ? envelope.method : '';
}

/** The method an envelope names, where it names one: a request does, and a response or an event does not. */
function named(envelope: Envelope): string | undefined {
  return typeof envelope.method === 'string' ? envelope.method : undefined;
}

/** The trace a frame carries, as it carries it: the two members of the profile verbatim, an absent one being none. */
function traceOf(envelope: Envelope): Trace | undefined {
  const traceparent = typeof envelope.traceparent === 'string' ? envelope.traceparent : '';
  const tracestate = typeof envelope.tracestate === 'string' ? envelope.tracestate : '';
  if (!traceparent && !tracestate) return undefined;
  return tracestate ? { traceparent, tracestate } : { traceparent };
}

function positive(value: unknown, what: string): number {
  if (typeof value !== 'number' || !Number.isInteger(value) || value <= 0) throw new DuplexError('invalid_options', `${what} must be a positive integer.`);
  return value;
}
