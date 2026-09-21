/**
 * The profile over a peer — the adapter #356 owes on the packets: the
 * `auth.challenge` / `auth.prove` exchange installed beside `live.*` on a
 * peer, the one context the connection then has, reachable from any handler
 * of the connection through the peer the runtime hands it; the guard that
 * calls `decide` before a handler's body and `effect` at the owner's
 * boundary, refusing as the profile's public errors; and the surface of a
 * generated side derived from its declaration, never written by hand.
 *
 * The runtime knows nothing of any of it: the exchange is two named
 * handlers, the context lives with the layer, and a guard is what the
 * consumer's implementation calls. A peer with no audience — a pipe — runs
 * no exchange and is given a context by construction, which is trust.
 */
import { DuplexError, type DuplexPeer } from '@nightseam/runtime';
import { fromHex, toHex } from './bytes.ts';
import { Connection, type Context, type Refusal as ConnectionRefusal } from './connection.ts';
import { Binding, type Condition, type Decision, type Delivery, type Refusal, type Surface } from './exposure.ts';
import * as grant from './grant.ts';

export const CHALLENGE_METHOD = 'auth.challenge';
export const PROVE_METHOD = 'auth.prove';

export interface OverOptions {
  root: grant.Root;
  /** The audience the connection reached, as the service knows itself; absent for a presentation with no URL. */
  audience?: string;
  /** The decision time, read at each decision — never by the layer on its own. */
  now: () => grant.Time;
  /** 32 bytes of the server's entropy per challenge. */
  nonce: () => Uint8Array;
  /** A context given by construction — trust, not authentication — for a peer that runs no exchange. */
  trusted?: Context;
}

const OVER = new WeakMap<DuplexPeer, AuthLayer>();

/** The layer over one peer: its connection state and the options it was given. */
export class AuthLayer {
  readonly peer: DuplexPeer;
  readonly options: OverOptions;
  readonly connection: Connection;
  #trusted: Context | undefined;

  constructor(peer: DuplexPeer, options: OverOptions) {
    this.peer = peer;
    this.options = options;
    this.connection = new Connection(options.root, options.audience);
    this.#trusted = options.trusted;
    peer.handle(CHALLENGE_METHOD, () => this.challenge());
    peer.handle(PROVE_METHOD, (params) => this.prove(params));
    OVER.set(peer, this);
  }

  /** The connection's one context, established or given by construction; undefined before either. */
  context(): Context | undefined {
    return this.connection.context() ?? this.#trusted;
  }

  private challenge(): { nonce: string } {
    if (this.#trusted !== undefined) throw refusalError({ code: 'auth.established' });
    const nonce = this.options.nonce();
    const outcome = this.connection.challenge(nonce);
    if ('code' in outcome) throw refusalError(outcome);
    return { nonce: toHex(outcome.nonce) };
  }

  private prove(params: unknown): { expires_at?: number } {
    if (this.#trusted !== undefined) throw refusalError({ code: 'auth.established' });
    const parsed = parseProve(params);
    if (parsed === undefined) {
      // A malformed prove is an attempt: it consumes the pending challenge.
      this.connection.prove(new Uint8Array(0), new Uint8Array(0), [], this.options.now());
      throw refusalError({ code: 'auth.malformed' });
    }
    const outcome = this.connection.prove(parsed.subject, parsed.possession, parsed.chain, this.options.now());
    if ('code' in outcome) throw refusalError(outcome);
    return outcome.validity.finite ? { expires_at: Number(outcome.validity.expiresAt) } : {};
  }
}

function parseProve(params: unknown): { subject: Uint8Array; possession: Uint8Array; chain: Uint8Array[] } | undefined {
  if (typeof params !== 'object' || params === null) return undefined;
  const p = params as { subject?: unknown; possession?: unknown; chain?: unknown };
  if (typeof p.subject !== 'string' || !p.subject.startsWith('ed25519:')) return undefined;
  const subject = fromHex(p.subject.slice('ed25519:'.length));
  if (subject === undefined || subject.length !== 32) return undefined;
  if (typeof p.possession !== 'string') return undefined;
  const possession = fromHex(p.possession);
  if (possession === undefined || possession.length !== 64) return undefined;
  if (!Array.isArray(p.chain)) return undefined;
  const chain: Uint8Array[] = [];
  for (const entry of p.chain) {
    if (typeof entry !== 'string') return undefined;
    const bytes = fromHex(entry);
    if (bytes === undefined || bytes.length === 0) return undefined;
    chain.push(bytes);
  }
  return { subject, possession, chain };
}

/** A refusal as it crosses the wire: the code, and the grant's code and hop as data where a chain refused. */
export function refusalError(refusal: ConnectionRefusal | Refusal): DuplexError {
  return new DuplexError(
    refusal.code,
    refusal.code,
    refusal.grant === undefined ? undefined : { grant: refusal.grant },
  );
}

/** Installs the exchange on a peer and keeps its connection's context with the peer. */
export function authOver(peer: DuplexPeer, options: OverOptions): AuthLayer {
  if (OVER.has(peer)) throw new DuplexError('duplicate_handler', 'the peer already carries the authority profile');
  return new AuthLayer(peer, options);
}

/** The layer over a peer, if one was installed. */
export function authOf(peer: DuplexPeer): AuthLayer | undefined {
  return OVER.get(peer);
}

/**
 * The connection's context from a handler's context: the runtime hands
 * every handler — a method's, an event's, an exported callable's — the peer
 * of the connection that carried the frame in, and the layer over that peer
 * has the context. Nothing captured at export, nothing copied across a hop.
 */
export function contextOf(context: { peer?: DuplexPeer } | undefined): Context | undefined {
  const peer = context?.peer;
  return peer === undefined ? undefined : OVER.get(peer)?.context();
}

// ---- the guard ------------------------------------------------------------------------------

export interface GuardOptions {
  root: grant.Root;
  now: () => grant.Time;
}

/**
 * What an implementation calls: `decide` at the top of a handler, `effect`
 * inside its transaction, `invoke` from an exported callable, `emit` before
 * an event leaves. Each refusal is thrown as the profile's public error and
 * crosses the wire with its code.
 */
export class Guard {
  readonly binding: Binding;
  readonly options: GuardOptions;

  constructor(binding: Binding, options: GuardOptions) {
    this.binding = binding;
    this.options = options;
  }

  decide(
    context: { peer?: DuplexPeer } | undefined,
    member: string,
    payload: { [field: string]: unknown } = {},
  ): Decision {
    const decided = this.binding.decide(this.options.root, member, payload, '', contextOf(context), this.options.now());
    if ('code' in decided) throw refusalError(decided);
    return decided;
  }

  effect(context: { peer?: DuplexPeer } | undefined, decision: Decision, condition?: Condition): void {
    const refused = this.binding.effect(this.options.root, decision, contextOf(context), this.options.now(), condition);
    if (refused !== undefined) throw refusalError(refused);
  }

  export(ref: string, member: string, scope: string): void {
    if (!this.binding.export(ref, member, scope))
      throw new DuplexError('auth.unknown_member', `${member} is not a callable member at ${scope}`);
  }

  invoke(
    context: { peer?: DuplexPeer } | undefined,
    ref: string,
    payload: { [field: string]: unknown } = {},
  ): Decision {
    const decided = this.binding.invoke(this.options.root, ref, payload, contextOf(context), this.options.now());
    if ('code' in decided) throw refusalError(decided);
    return decided;
  }

  emit(event: string, data: { [field: string]: unknown }, to: { name: string; peer: DuplexPeer }[]): Delivery[] {
    return this.binding.emit(
      this.options.root,
      event,
      data,
      to.map((r) => ({ name: r.name, ctx: contextOf(r) })),
      this.options.now(),
    );
  }
}

/** A guard over a bound policy; construction refuses as `bind` does, with every member named. */
export function guard(binding: Binding, options: GuardOptions): Guard {
  return new Guard(binding, options);
}

// ---- the surface ----------------------------------------------------------------------------

/**
 * A side's members from the declaration the generator emitted — its methods
 * and events, and the family's callable types — with the top-level names of
 * each request payload, which is what a scope template may name. Nothing is
 * written by hand, so a member the declaration gains is a construction
 * failure until the policy names it.
 */
export function surfaceOf(family: string, side: 'server' | 'client', declaration: string, digest: string): Surface {
  const parsed = JSON.parse(declaration) as { definitions: Record<string, Definition> };
  const definitions = parsed.definitions;
  const fam = definitions[family];
  if (fam === undefined) throw new DuplexError('auth.contract_mismatch', `the declaration has no family ${family}`);
  const sideRef = side === 'server' ? fam.server?.ref : fam.client?.ref;
  const sd = sideRef === undefined ? undefined : definitions[sideRef];
  const fieldsOf = (expr: { ref?: string } | undefined): string[] => {
    const def = expr?.ref === undefined ? undefined : definitions[expr.ref];
    if (def === undefined || def.kind !== 'record') return [];
    return (def.fields ?? []).map((f) => f.name).sort();
  };
  const members: Surface['members'] = [];
  for (const [name, m] of Object.entries(sd?.methods ?? {}))
    members.push({ key: `method:${name}`, fields: fieldsOf(m.request) });
  for (const [name, data] of Object.entries(sd?.events ?? {}))
    members.push({ key: `event:${name}`, fields: fieldsOf(data) });
  for (const [name, t] of Object.entries(fam.types ?? {})) {
    const def = t.ref === undefined ? undefined : definitions[t.ref];
    if (def?.kind === 'callable') members.push({ key: `callable:${name}`, fields: fieldsOf(def.request) });
  }
  members.sort((a, b) => (a.key < b.key ? -1 : a.key > b.key ? 1 : 0));
  return { family, digest, members };
}

interface Definition {
  kind?: string;
  server?: { ref: string };
  client?: { ref: string };
  types?: Record<string, { ref?: string }>;
  methods?: Record<string, { request?: { ref?: string } }>;
  events?: Record<string, { ref?: string }>;
  fields?: { name: string }[];
  request?: { ref?: string };
}

/** The top-level members of a typed request, as the wire spells them, for a scope template's holes. */
export function payloadOf(params: unknown): { [field: string]: unknown } {
  if (typeof params !== 'object' || params === null || Array.isArray(params)) return {};
  return JSON.parse(JSON.stringify(params, (_, v: unknown) => (typeof v === 'bigint' ? v.toString() : v))) as {
    [field: string]: unknown;
  };
}
