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
 * a frame carries downstream, the holder as state, the log's shape and an
 * in-memory one. Who may attach, who may take control, how long a lease
 * lasts, where the log is kept: the consumer's, and the registry never asks.
 *
 * The relay reads a frame as the plain object it is and rewrites only `id`.
 * Every other member — the profile's, a trace's, one a later profile adds —
 * it forwards verbatim, by construction rather than by enumeration.
 */
import { DuplexError } from '@nightseam/runtime';
import type { Channel } from '@nightseam/tunnel';

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

export interface RegistryOptions {
  /** How many consumers may be attached to one session at once; an attach beyond it is refused. Default: 64. */
  maxAttachments?: number;
}

/** The live sessions of one process: an id to the relay that carries it. */
export class Registry {
  private readonly limits: Required<RegistryOptions>;
  private readonly relays = new Map<string, Relay>();

  constructor(options: RegistryOptions = {}) {
    this.limits = { maxAttachments: positive(options.maxAttachments ?? 64, 'maxAttachments') };
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

  private relay(id: string): Relay {
    const relay = this.relays.get(id);
    if (!relay) throw new DuplexError('no_session', `No session ${id} is bound.`);
    return relay;
  }

  /** @internal */
  get maxAttachments(): number { return this.limits.maxAttachments; }

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

/** A request the machine sent: the message as it arrived, whether its method asks, and which consumer it stands with, under which id. */
interface Open { message: Envelope; asking: boolean; at?: Attachment; minted?: string }

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
   * The ids the relay mints. A machine serves its family, so it reads the
   * client's ids, `c:N`; a consumer calls it, so it reads the server's,
   * `s:N`. Two consumers that each mint `c:1` are distinct here by
   * construction, and the log records the minted id, unique per session.
   */
  private upNext = 0;
  private downNext = 0;
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
  }

  attach(down: Channel, role: Role, origin: string, after: number): Attachment {
    if (this.ended) throw new DuplexError('no_session', `No session ${this.id} is bound.`);
    if (this.attachments.size >= this.registry.maxAttachments) throw new DuplexError('too_many_attachments', 'No room for another consumer on this session.');
    const attachment = new Attachment(this, down, role, origin);
    this.attachments.add(attachment);
    // The replay is queued before the channel is listened to, so that what
    // the consumer missed reaches it before anything live, in one order.
    this.run(() => this.log.replay(after, async frame => { this.write(down, frame.message); }));
    attachment.attachTo(down.listen({
      frame: frame => this.run(() => this.fromDown(attachment, frame)),
      close: () => this.run(() => this.drop(attachment)),
    }));
    return attachment;
  }

  control(holder: Attachment | null): void {
    if (holder !== null) {
      if (!this.attachments.has(holder)) throw new DuplexError('not_attached', 'Control is held by a consumer attached to the session.');
      if (holder.role !== 'participant') throw new DuplexError('not_controlling', 'An observer never holds control.');
    }
    this.holder = holder;
    // A request the machine has open follows control: the holder it was
    // routed to no longer answers it, and the new one is asked afresh.
    this.run(() => this.route());
  }

  asking(): boolean {
    for (const open of this.open.values()) if (open.asking) return true;
    return false;
  }

  detach(attachment: Attachment): void { this.run(() => this.drop(attachment)); }

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
          return;
        }
        if (!id) return;
        const minted = 'c:' + ++this.upNext;
        this.inflight.set(minted, { at: attachment, id });
        await this.send('up', attachment.origin, { ...envelope, id: minted });
        return;
      }
      case 'cancel': {
        // A cancel decides: it withdraws what the holder asked for.
        if (this.holder !== attachment || !id) return;
        for (const [minted, held] of this.inflight) {
          if (held.at !== attachment || held.id !== id) continue;
          this.inflight.delete(minted);
          await this.send('up', attachment.origin, { ...envelope, id: minted });
          return;
        }
        return;
      }
      case 'response': {
        // Answering what the machine asked decides, and only the consumer the
        // ask stands with knows the id it stands under.
        if (!id) return;
        for (const [asked, open] of this.open) {
          if (open.at !== attachment || open.minted !== id) continue;
          this.open.delete(asked);
          await this.send('up', attachment.origin, { ...envelope, id: asked });
          return;
        }
        return;
      }
      default:
        // An event names no method, so nothing about it decides.
        await this.send('up', attachment.origin, { ...envelope });
    }
  }

  /** A frame from the machine: a response to the one consumer that asked, an event to all of them, a request to the holder. */
  private async fromUp(frame: Wire): Promise<void> {
    const envelope = read(frame);
    if (!envelope) {
      this.up.close(1008, 'a frame that is not a message of the profile');
      return;
    }
    const id = typeof envelope.id === 'string' ? envelope.id : undefined;
    switch (envelope.kind) {
      case 'response': {
        if (!id) return;
        const held = this.inflight.get(id);
        if (!held) return;
        this.inflight.delete(id);
        await this.send('down', '', { ...envelope, id: held.id }, held.at);
        return;
      }
      case 'request': {
        // What the machine asks is the holder's to answer, and waits while
        // there is none; asks names the ones a session is waiting on, which
        // is what attention counts.
        if (!id) return;
        const method = typeof envelope.method === 'string' ? envelope.method : '';
        this.open.set(id, { message: envelope, asking: this.governance.asks(method) });
        await this.route();
        return;
      }
      case 'cancel': {
        if (!id) return;
        const open = this.open.get(id);
        if (!open) return;
        this.open.delete(id);
        if (open.at && open.minted) await this.send('down', '', { ...envelope, id: open.minted }, open.at);
        return;
      }
      default:
        // An event reaches every attached consumer, and the log once.
        await this.send('down', '', { ...envelope });
    }
  }

  /** route hands every request the machine has open to the holder of control, and to no one while there is none. */
  private async route(): Promise<void> {
    for (const open of this.open.values()) {
      if (!this.holder || open.at === this.holder) continue;
      open.at = this.holder;
      open.minted = 's:' + ++this.downNext;
      await this.send('down', '', { ...open.message, id: open.minted }, this.holder);
    }
  }

  /** send records a frame in the log and writes it to one consumer, to every consumer, or to the machine. */
  private async send(direction: Direction, origin: string, envelope: Envelope, to?: Attachment): Promise<void> {
    await this.log.append({ sequence: 0, direction, origin, at: new Date(), message: envelope, truncated: false });
    if (direction === 'up') this.write(this.up, envelope);
    else if (to) this.write(to.channel, envelope);
    else for (const attachment of [...this.attachments]) this.write(attachment.channel, envelope);
  }

  /** write hands a message to a channel; a channel that refuses it has closed, and its own close detaches it. */
  private write(channel: Channel, message: unknown): void {
    try { channel.send({ kind: 'text', data: JSON.stringify(message) }); } catch { /* The channel's close is what detaches it. */ }
  }

  /** drop detaches one consumer: the session stands, and what it left open stands with it. */
  private drop(attachment: Attachment): void {
    if (!this.attachments.delete(attachment)) return;
    attachment.release();
    if (this.holder === attachment) this.holder = null;
    for (const [minted, held] of this.inflight) if (held.at === attachment) this.inflight.delete(minted);
    for (const open of this.open.values()) {
      if (open.at !== attachment) continue;
      open.at = undefined;
      open.minted = undefined;
    }
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

function positive(value: unknown, what: string): number {
  if (typeof value !== 'number' || !Number.isInteger(value) || value <= 0) throw new DuplexError('invalid_options', `${what} must be a positive integer.`);
  return value;
}
