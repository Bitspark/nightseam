/**
 * A session over the channels of a tunnel: an identity that outlives the
 * connections carrying it. One up channel, the one its machine speaks on;
 * any number of down channels, one per attached consumer, each in a role;
 * one holder of control among them; and a log of every frame the session
 * exchanged, in one order, replayed from a sequence.
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
import { DuplexError } from '@nightseam/runtime';
import type { ObserverEvent, Trace } from '@nightseam/runtime';
import type { Channel } from '@nightseam/tunnel';

/**
 * What a session tells the observer of the peer it runs over, declared into
 * the runtime's registry so that a consumer's `switch (event.type)` covers
 * them beside the runtime's and the tunnel's. The session takes no observer
 * of its own: every one of these goes out through the peer the up channel's
 * tunnel runs over, which is the observer the consumer already chose.
 *
 * They are the changes `onChange` hands a consumer, said the way the runtime
 * says things: an event that concerns a frame carries that frame's trace, and
 * none of them carries a payload — `frame.appended` says which sequence, which
 * direction, whose origin and how many bytes, and never the message.
 */
declare module '@nightseam/runtime' {
  interface ObserverEvents {
    /** A session is bound to the channel its machine speaks on, and stands until that channel closes. */
    'session.bound': { type: 'session.bound'; at: Date; session: string };
    /** The machine's channel closed, so the session is gone and every consumer of it was ended with this close. */
    'session.unbound': { type: 'session.unbound'; at: Date; session: string; code: number; reason: string };
    /** A consumer joined a session in a role, under an origin, resuming from a sequence. */
    'session.attached': { type: 'session.attached'; at: Date; session: string; role: Role; origin: string; after: number };
    /** A consumer left one, by detaching or by its channel closing; the session stands. */
    'session.detached': { type: 'session.detached'; at: Date; session: string; role: Role; origin: string };
    /** The machine sent a request the holder of control must answer; asking is whether the family's session tier counts it in attention. */
    'ask.raised': { type: 'ask.raised'; at: Date; session: string; id: string; method: string; asking: boolean; trace?: Trace };
    /** That request was handed to a holder — when it arrived, and again each time control moved while it stood open. */
    'ask.routed': { type: 'ask.routed'; at: Date; session: string; id: string; method: string; origin: string; trace?: Trace };
    /** The holder answered it, and the answer went to the machine. */
    'ask.answered': { type: 'ask.answered'; at: Date; session: string; id: string; method: string; origin: string; trace?: Trace };
    /** Control of a session moved; an origin absent is control released, left with nobody. */
    'control.changed': { type: 'control.changed'; at: Date; session: string; origin?: string };
    /** A frame was appended to the session's log: its sequence, its direction, the origin of the consumer whose frame it was — none for the machine's own — and its size, never its message. */
    'frame.appended': { type: 'frame.appended'; at: Date; session: string; sequence: number; direction: Direction; origin: string; bytes: number; method?: string; trace?: Trace };
    /** A consumer's frame the relay answered in the machine's place rather than forwarding: not_controlling where control is not held, busy where the session has too many requests open. */
    'session.refused': { type: 'session.refused'; at: Date; session: string; code: string; method: string; role: Role; origin: string; trace?: Trace };
  }
}

/** One frame of the seam, as the channel it travels on declares it. */
type Wire = Parameters<Channel['send']>[0];
/** One message of the profile, read as the plain object it is. */
type Envelope = Record<string, unknown>;

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

/** The consumer's store of a session's frames; the package ships an in-memory one. */
export interface Log {
  /** Records a frame and returns the sequence it was given, which counts from one. */
  append(frame: Frame): Promise<number>;
  /** Delivers every frame after a sequence, in order, waiting on each. */
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

export interface RegistryOptions {
  /** How many consumers may be attached to one session at once; an attach beyond it is refused. Default: 64. */
  maxAttachments?: number;
  /** How many requests a session may have open towards its machine at once; one beyond it is refused with busy. Default: 256. */
  maxInflight?: number;
}

/** The live sessions of one process: an id to the relay that carries it. */
export class Registry {
  private readonly limits: Required<RegistryOptions>;
  private readonly relays = new Map<string, Relay>();
  /** One entry per registration rather than one per function, so that the same hook registered twice is stopped once per registration. */
  private readonly hooks = new Set<{ fn: (change: Change) => void }>();

  constructor(options: RegistryOptions = {}) {
    this.limits = {
      maxAttachments: positive(options.maxAttachments ?? 64, 'maxAttachments'),
      maxInflight: positive(options.maxInflight ?? 256, 'maxInflight'),
    };
  }

  /** Binds a session to the channel its machine speaks on, governed by its family's session tier and logged to `log`; the session lives until that channel closes. */
  bind(id: string, up: Channel, governance: Governance, log: Log): void {
    if (typeof id !== 'string' || id === '') throw new DuplexError('session_invalid', 'A session is bound under an id.');
    if (this.relays.has(id)) throw new DuplexError('session_exists', `Session ${id} is bound.`);
    const relay = new Relay(this, id, up, governance, log);
    this.relays.set(id, relay);
    relay.listen();
  }

  /** Attaches a consumer's channel to a bound session in a role, stamping what it sends with `origin`, after replaying the log from `after`. */
  attach(id: string, down: Channel, role: Role, origin: string, after: number): Attachment {
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
  get limit(): Readonly<Required<RegistryOptions>> { return this.limits; }

  /** @internal */
  forget(id: string): void { this.relays.delete(id); }
}

/** One consumer on a session: the channel it speaks on, the role it attached in and the origin its frames carry. */
export class Attachment {
  readonly role: Role;
  readonly origin: string;
  readonly channel: Channel;
  private readonly relay: Relay;
  /** What the channel handed back when the relay began listening, so that detaching leaves it as it was found. */
  private unlisten?: () => void;

  /** @internal */
  constructor(relay: Relay, channel: Channel, role: Role, origin: string) {
    this.relay = relay;
    this.channel = channel;
    this.role = role;
    this.origin = origin;
  }

  /** Detaches the consumer: the relay stops carrying its channel, releases control if it held it, and the session stands. The channel is the caller's to close. */
  detach(): void { this.relay.detach(this); }

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
 * One bound session: the up channel, the consumers attached to it, the
 * holder of control, and the log. Every frame the relay handles runs on one
 * queue, so that what the log records and what a channel receives are in the
 * same order — including a replay, which is queued before the frames of the
 * consumer that asked for it.
 */
class Relay {
  readonly id: string;
  private readonly registry: Registry;
  private readonly up: Channel;
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
  private queue: Promise<void> = Promise.resolve();
  private ended = false;

  constructor(registry: Registry, id: string, up: Channel, governance: Governance, log: Log) {
    this.registry = registry;
    this.id = id;
    this.up = up;
    this.governance = governance;
    this.log = log;
  }

  /** @internal */
  listen(): void {
    this.up.listen({
      frame: frame => this.run(() => this.fromUp(frame)),
      close: (code, reason) => this.run(() => this.end(code, reason)),
    });
    this.change('bound');
    this.observe({ type: 'session.bound', at: new Date(), session: this.id });
  }

  attach(down: Channel, role: Role, origin: string, after: number): Attachment {
    if (this.ended) throw new DuplexError('no_session', `No session ${this.id} is bound.`);
    if (this.attachments.size >= this.registry.limit.maxAttachments) throw new DuplexError('too_many_attachments', 'No room for another consumer on this session.');
    const attachment = new Attachment(this, down, role, origin);
    this.attachments.add(attachment);
    // The replay is queued before the channel is listened to, so that what
    // the consumer missed reaches it before anything live, in one order, and
    // it stops where the session stood when the consumer was added: a frame
    // recorded since reaches it live instead, once.
    const ceiling = this.sequence;
    this.run(() => this.log.replay(after, async frame => {
      // A cut message is not a message: what the log truncated is there for a
      // consumer that reads the log, not for a channel that speaks the family.
      if (frame.sequence > ceiling || frame.truncated) return;
      this.write(down, frame.message);
    }));
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
    this.change('control_changed', holder ? { attachment: holder } : {});
    this.observe({ type: 'control.changed', at: new Date(), session: this.id, ...(holder ? { origin: holder.origin } : {}) });
    // Every request the machine is waiting on follows control: the consumer
    // it stood with no longer answers it, and whoever holds control now is
    // asked it afresh.
    this.run(() => this.reroute());
  }

  asking(): boolean {
    for (const open of this.open.values()) if (open.asking) return true;
    return false;
  }

  detach(attachment: Attachment): void { this.run(() => this.drop(attachment)); }

  /** change tells the registry's hooks one domain change of this session, which the registry stamps with the session and the time. */
  private change(kind: ChangeKind, of: Of = {}): void { this.registry.changed(this.id, kind, of); }

  /** observe tells the observer of the peer the session runs over one event of the session's own; a peer given no observer is told nothing and pays nothing. */
  private observe(event: ObserverEvent): void { this.up.observe(event); }

  /** run puts one step on the relay's queue; a step that throws leaves the session standing. */
  private run(step: () => void | Promise<void>): void {
    this.queue = this.queue.then(step).catch(() => { /* A channel that failed is dropped where it failed. */ });
  }

  /** A frame from a consumer: what it may send depends on its role and on who holds control. */
  private async fromDown(attachment: Attachment, frame: Wire): Promise<void> {
    if (!this.attachments.has(attachment)) return;
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
          if (id) this.write(attachment.channel, { version: 1, kind: 'response', id, error: { code: 'not_controlling', message: 'Only the holder of control decides on this session.' } });
          this.refuse('not_controlling', attachment, method, envelope);
          return;
        }
        if (!id) return;
        if (this.inflight.size >= this.registry.limit.maxInflight) {
          this.write(attachment.channel, { version: 1, kind: 'response', id, error: { code: 'busy', message: 'The session has too many requests open.' } });
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
    const envelope = read(frame);
    if (!envelope) {
      this.up.close(1008, 'a frame that is not a message of the profile');
      return;
    }
    // The log is the session's conversation with its machine: every frame
    // the machine sent is recorded once, as it sent it — under the id the
    // session asked with, whether or not there is a consumer to hand it to.
    const attached = await this.record('down', null, envelope);
    const id = typeof envelope.id === 'string' ? envelope.id : undefined;
    switch (envelope.kind) {
      case 'response': {
        if (!id) return;
        const held = this.inflight.get(id);
        if (!held) return;
        this.inflight.delete(id);
        this.write(held.at.channel, { ...envelope, id: held.id });
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
          this.write(this.holder.channel, envelope);
          this.routed(id, method, this.holder, trace);
        }
        return;
      }
      case 'cancel': {
        if (!id) return;
        const open = this.open.get(id);
        if (!open) return;
        this.open.delete(id);
        if (open.at) this.write(open.at.channel, envelope);
        return;
      }
      default:
        // An event reaches every attached consumer, and the log once.
        for (const attachment of attached) this.write(attachment.channel, envelope);
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
    // is about.
    for (const [id, open] of this.open) {
      this.write(holder.channel, open.message);
      this.routed(id, named(open.message) ?? '', holder, traceOf(open.message));
    }
  }

  /** sendUp records a consumer's frame and writes it to the machine, in the order it recorded them. */
  private async sendUp(from: Attachment, envelope: Envelope): Promise<void> {
    await this.record('up', from, envelope);
    this.write(this.up, envelope);
  }

  /** record appends a frame to the session's log and returns the consumers there were when its sequence was assigned; one attaching between the two is not among them and takes the frame from the log's replay instead. A frame from the machine is nobody's, and carries no origin. */
  private async record(direction: Direction, from: Attachment | null, envelope: Envelope): Promise<Attachment[]> {
    const origin = from?.origin ?? '';
    const sequence = await this.log.append({ sequence: 0, direction, origin, at: new Date(), message: envelope, truncated: false });
    this.sequence = sequence;
    const method = named(envelope);
    const trace = traceOf(envelope);
    this.change('frame_appended', { sequence, attachment: from ?? undefined, method, trace });
    // What the observer is told of a frame is its size and never its bytes:
    // the message is the log's, which is the one place a session keeps one.
    this.observe({ type: 'frame.appended', at: new Date(), session: this.id, sequence, direction, origin, bytes: encoder.encode(JSON.stringify(envelope)).byteLength, ...(method !== undefined ? { method } : {}), ...(trace ? { trace } : {}) });
    return [...this.attachments];
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

  /** write hands a message to a channel; a channel that refuses it has closed, and its own close detaches it. */
  private write(channel: Channel, message: unknown): void {
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
      this.observe({ type: 'control.changed', at: new Date(), session: this.id });
    }
    for (const [minted, held] of this.inflight) if (held.at === attachment) this.inflight.delete(minted);
    for (const open of this.open.values()) if (open.at === attachment) open.at = null;
    this.change('detached', { attachment });
    this.observe({ type: 'session.detached', at: new Date(), session: this.id, role: attachment.role, origin: attachment.origin });
  }

  /** end ends the session: the machine's channel closed, so every consumer's ends with the same close. */
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

/** read is a frame as the message it carries, or nothing when it carries none: the profile is JSON text. */
function read(frame: Wire): Envelope | undefined {
  if (frame.kind !== 'text') return undefined;
  let value: unknown;
  try { value = JSON.parse(frame.data); } catch { return undefined; }
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return undefined;
  return value as Envelope;
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
