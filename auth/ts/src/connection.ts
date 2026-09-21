/**
 * The authenticated connection — docs/auth/connection.md, held to
 * conformance/tables/auth-boot.json.
 *
 * The audience a connection binds to, the possession proof in
 * `nightseam-auth/1`, the challenge/prove exchange that makes a connection's
 * one immutable context, the decision at every protected call, and the
 * bootstrap that admits a login's delegation over a consumer-supplied
 * versioned store. `call`, `verify` and every transition are pure over their
 * arguments; the store is the only place state lives.
 */
import {
  deriveAudience,
  provePossession,
  verifyLogin,
  verifyPossession,
  verifyCollect,
  type LoginRequest,
} from '@bitspark/archon-sdk';
import { compare, concat, equal, hasControl, toHex, u16, utf8, wellFormed } from './bytes.ts';
import * as grant from './grant.ts';

export const DOMAIN = 'nightseam-auth/1';

/** The bounds of the packet, every one an input a test can see. */
export const NONCE_BYTES = 32;
export const ID_BYTES = 16;
export const LOGIN_EXPIRES_IN = 300n;
export const ANSWER_RETENTION = 300n;
export const COLLECT_INTERVAL = 5n;
export const ISSUER_CLOCK_SKEW = 60n;
export const MAX_VALID_FOR = 2592000;
export const MAX_PENDING = 4096;

const CONNECTION_ROLE = 0x01;

// ---- the audience ------------------------------------------------------------------

/** What `audience` throws: the URL is outside the grammar. */
export class AudienceError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'AudienceError';
  }
}

/**
 * The connection URL as the service knows itself: Archon's audience grammar,
 * one definition — the derivation given the URL with a login tail — and
 * refused rather than normalized outside it.
 */
export function audience(url: string): string {
  if (typeof url !== 'string' || url.includes('/login/')) throw new AudienceError('not a connection URL');
  try {
    return deriveAudience(url + '/login/00').audience;
  } catch (error) {
    throw new AudienceError(error instanceof Error ? error.message : String(error));
  }
}

/** `0x01 ‖ u16(len audience) ‖ audience`: what only this connection has. */
export function binding(aud: string): Uint8Array {
  const bytes = utf8(aud);
  if (bytes.length === 0 || bytes.length > 0xffff) throw new AudienceError('audience out of range');
  return concat(new Uint8Array([CONNECTION_ROLE]), u16(bytes.length), bytes);
}

/** The possession proof of the key behind `seed` over this connection's challenge. */
export function prove(seed: Uint8Array, aud: string, nonce: Uint8Array): Uint8Array {
  return provePossession(seed, DOMAIN, nonce, binding(aud));
}

/** Whether `proof` is the key's possession over this connection's challenge. Total. */
export function verify(pubkey: Uint8Array, aud: string, nonce: Uint8Array, proof: Uint8Array): boolean {
  let bound: Uint8Array;
  try {
    bound = binding(aud);
  } catch {
    return false;
  }
  return verifyPossession(pubkey, DOMAIN, nonce, bound, proof);
}

// ---- the context and the exchange ----------------------------------------------------

/** What a protected connection has exactly one of. */
export interface Context {
  subject: Uint8Array;
  chain: Uint8Array[];
  validity: grant.Validity;
  since: bigint;
}

export type Code =
  | 'auth.unsupported'
  | 'auth.no_challenge'
  | 'auth.possession_invalid'
  | 'auth.chain_refused'
  | 'auth.subject_mismatch'
  | 'auth.established'
  | 'auth.malformed'
  | 'auth.unauthenticated'
  | 'auth.denied';

export interface Refusal {
  code: Code;
  grant?: grant.Refusal;
}

export function isRefusal<T extends object>(value: T | Refusal): value is Refusal {
  return 'code' in value && typeof (value as Refusal).code === 'string';
}

/**
 * The per-connection exchange, pure over its inputs: the nonce the server
 * minted is handed to `challenge`, the decision time to `prove`. A connection
 * with no audience — a pipe — refuses both.
 */
export class Connection {
  readonly root: grant.Root;
  readonly audience: string | undefined;
  #pending: Uint8Array | undefined;
  #context: Context | undefined;

  constructor(root: grant.Root, aud: string | undefined) {
    this.root = root;
    this.audience = aud;
  }

  /** Keeps the minted nonce as the one pending challenge; a second replaces the first. */
  challenge(nonce: Uint8Array): { nonce: Uint8Array } | Refusal {
    if (this.audience === undefined) return { code: 'auth.unsupported' };
    if (this.#context !== undefined) return { code: 'auth.established' };
    if (!(nonce instanceof Uint8Array) || nonce.length !== NONCE_BYTES) return { code: 'auth.malformed' };
    this.#pending = nonce;
    return { nonce };
  }

  /** In the packet's order, and whatever the outcome consumes the pending nonce. */
  prove(subject: Uint8Array, proof: Uint8Array, chain: Uint8Array[], now: grant.Time): Context | Refusal {
    if (this.audience === undefined) return { code: 'auth.unsupported' };
    if (this.#context !== undefined) return { code: 'auth.established' };
    if (
      !(subject instanceof Uint8Array) ||
      subject.length !== 32 ||
      !(proof instanceof Uint8Array) ||
      !Array.isArray(chain) ||
      !chain.every((e) => e instanceof Uint8Array)
    ) {
      this.#pending = undefined;
      return { code: 'auth.malformed' };
    }
    const nonce = this.#pending;
    this.#pending = undefined;
    if (nonce === undefined) return { code: 'auth.no_challenge' };
    if (!verify(subject, this.audience, nonce, proof)) return { code: 'auth.possession_invalid' };
    const held = grant.inspect(this.root, chain, now);
    if (grant.isRefusal(held)) return { code: 'auth.chain_refused', grant: held };
    if (!equal(held.subject, subject)) return { code: 'auth.subject_mismatch' };
    this.#context = {
      subject,
      chain: chain.map((e) => e.slice()),
      validity: held.validity,
      since: now.present ? now.now : 0n,
    };
    return this.#context;
  }

  context(): Context | undefined {
    return this.#context;
  }
}

/** The decision at the call: structure and time, then identity, then coverage. */
export function call(
  root: grant.Root,
  ctx: Context | undefined,
  request: grant.Request,
  now: grant.Time,
): grant.Verified | Refusal {
  if (ctx === undefined) return { code: 'auth.unauthenticated' };
  const held = grant.inspect(root, ctx.chain, now);
  if (grant.isRefusal(held)) return { code: 'auth.denied', grant: held };
  if (!equal(held.subject, ctx.subject)) return { code: 'auth.subject_mismatch' };
  const verified = grant.verify(root, ctx.chain, request, now);
  if (grant.isRefusal(verified)) return { code: 'auth.denied', grant: verified };
  return verified;
}

// ---- the terms ------------------------------------------------------------------------

export interface Terms {
  actions: string[];
  delegable: string[];
  scope: string[];
  depth: number;
}

/** The tagged grammar, in its one canonical order, or undefined. */
export function parseTerms(entries: string[]): Terms | undefined {
  const terms: Terms = { actions: [], delegable: [], scope: [], depth: 0 };
  const order = ['action:', 'delegable:', 'scope:', 'depth:'];
  let stage = 0;
  let depthSeen = false;
  const lists: string[][] = [terms.actions, terms.delegable, terms.scope];
  for (const entry of entries) {
    if (typeof entry !== 'string') return undefined;
    const tag = order.findIndex((t) => entry.startsWith(t));
    if (tag < 0 || tag < stage) return undefined;
    stage = tag;
    const value = entry.slice(order[tag]!.length);
    if (tag === 3) {
      if (depthSeen || !/^(0|[1-9][0-9]*)$/.test(value)) return undefined;
      const depth = Number(value);
      if (depth > grant.MAX_DEPTH) return undefined;
      terms.depth = depth;
      depthSeen = true;
      continue;
    }
    const list = lists[tag]!;
    if (!validEntry(value)) return undefined;
    if (list.length > 0 && compare(utf8(list[list.length - 1]!), utf8(value)) >= 0) return undefined;
    if (list.length >= grant.MAX_ENTRIES) return undefined;
    list.push(value);
  }
  if (!terms.delegable.every((d) => terms.actions.includes(d))) return undefined;
  return terms;
}

function validEntry(value: string): boolean {
  if (!wellFormed(value) || hasControl(value)) return false;
  const n = utf8(value).length;
  return n >= 1 && n <= grant.MAX_ENTRY_BYTES;
}

/** The terms in the grammar's canonical order; depth last, and only when asked. */
export function renderTerms(t: Terms, withDepth: boolean): string[] {
  const out = [
    ...t.actions.map((a) => 'action:' + a),
    ...t.delegable.map((d) => 'delegable:' + d),
    ...t.scope.map((s) => 'scope:' + s),
  ];
  if (withDepth) out.push('depth:' + t.depth);
  return out;
}

// ---- the bootstrap ---------------------------------------------------------------------

export type State = 'pending' | 'answered' | 'collected';

export type LoginCode = 'invalid_request' | 'invalid_grant' | 'expired_token' | 'authorization_pending' | 'slow_down';

export interface LoginRefusal {
  code: LoginCode;
  grant?: grant.Refusal;
}

/** One pending login as the store keeps it, with the version the store compares. */
export interface Record {
  id: Uint8Array;
  version: number;
  state: State;
  nonce: Uint8Array;
  browser: Uint8Array;
  scope: string[];
  terms: Terms;
  validFor: number;
  expiresAt: bigint;
  retainedTo?: bigint;
  lastCollect?: bigint;
  answer?: Answer;
}

export interface Answer {
  principal: Uint8Array;
  possession: Uint8Array;
  authority: Uint8Array[];
}

/**
 * Any store offering a versioned compare-and-set. The version counts the
 * record's transitions — begun, answered, collected — and is what two
 * replicas answering the same login resolve by; a write that moves only the
 * pacing reference keeps it, so that pacing a poll is not a transition.
 */
export interface Store {
  get(id: Uint8Array): Record | undefined;
  /** Writes when the stored version is `expectVersion` (0 for a new record); a state change advances the version by one. */
  put(record: Record, expectVersion: number): boolean;
  /** Drops what is gone at `now` and returns the ids dropped. */
  sweep(now: bigint): Uint8Array[];
  /** How many records are live. */
  size(): number;
}

/** A store in memory: the reference shape a consumer's own must offer. */
export class MemoryStore implements Store {
  readonly #records = new Map<string, Record>();
  get(id: Uint8Array): Record | undefined {
    const r = this.#records.get(toHex(id));
    return r === undefined ? undefined : cloneRecord(r);
  }
  put(record: Record, expectVersion: number): boolean {
    const k = toHex(record.id);
    const current = this.#records.get(k);
    if ((current?.version ?? 0) !== expectVersion) return false;
    const transition = current === undefined || current.state !== record.state;
    this.#records.set(k, { ...cloneRecord(record), version: transition ? expectVersion + 1 : expectVersion });
    return true;
  }
  sweep(now: bigint): Uint8Array[] {
    const gone: Uint8Array[] = [];
    for (const [k, r] of this.#records) {
      if (isGone(r, now)) {
        gone.push(r.id);
        this.#records.delete(k);
      }
    }
    return gone;
  }
  size(): number {
    return this.#records.size;
  }
}

function cloneRecord(r: Record): Record {
  return {
    ...r,
    scope: [...r.scope],
    terms: { ...r.terms, actions: [...r.terms.actions], delegable: [...r.terms.delegable], scope: [...r.terms.scope] },
  };
}

function isGone(r: Record, now: bigint): boolean {
  return r.state === 'pending' ? now >= r.expiresAt : now >= (r.retainedTo ?? 0n);
}

function loginRequest(r: Record): LoginRequest {
  return { id: r.id, nonce: r.nonce, browser: r.browser, scope: r.scope, validFor: r.validFor };
}

/**
 * The service side of the bootstrap: one audience, one root, one store. Each
 * transition is decided at the `now` the caller supplies and written through
 * the store's compare-and-set.
 */
export class Service {
  readonly root: grant.Root;
  readonly audience: string;
  readonly store: Store;

  constructor(root: grant.Root, aud: string, store: Store) {
    this.root = root;
    this.audience = aud;
    this.store = store;
  }

  /** Mints nothing: the id and nonce are the server's entropy, given here. */
  begin(
    now: bigint,
    id: Uint8Array,
    nonce: Uint8Array,
    browser: Uint8Array,
    scope: string[],
    validFor: number,
  ): Record | LoginRefusal {
    if (id.length !== ID_BYTES || nonce.length !== NONCE_BYTES || browser.length !== 32)
      return { code: 'invalid_request' };
    if (!Number.isInteger(validFor) || validFor < 1 || validFor > MAX_VALID_FOR) return { code: 'invalid_request' };
    const terms = parseTerms(scope);
    if (terms === undefined) return { code: 'invalid_request' };
    const existing = this.store.get(id);
    if (existing !== undefined && !isGone(existing, now)) return { code: 'invalid_request' };
    if (this.store.size() >= MAX_PENDING) return { code: 'slow_down' };
    const record: Record = {
      id,
      version: 0,
      state: 'pending',
      nonce,
      browser,
      scope: [...scope],
      terms,
      validFor,
      expiresAt: now + LOGIN_EXPIRES_IN,
    };
    if (!this.store.put(record, existing?.version ?? 0)) return { code: 'invalid_request' };
    return this.store.get(id)!;
  }

  /** Answers only a pending record. */
  read(now: bigint, id: Uint8Array): Record | LoginRefusal {
    const r = this.store.get(id);
    if (r === undefined || isGone(r, now)) return { code: 'expired_token' };
    if (r.state !== 'pending') return { code: 'invalid_request' };
    return r;
  }

  /** The law: the proof, the chain, the leaf, the terms, the bound — then the one transition. */
  answer(
    now: bigint,
    id: Uint8Array,
    principal: Uint8Array,
    proof: Uint8Array,
    authority: Uint8Array[],
    expectVersion: number,
  ): 'answered' | LoginRefusal {
    const r = this.store.get(id);
    if (r === undefined || isGone(r, now)) return { code: 'expired_token' };
    if (r.state !== 'pending' || r.version !== expectVersion) return { code: 'invalid_request' };
    const admitted = admitAuthority(this.root, this.audience, r, principal, proof, authority, now);
    if (admitted !== undefined) return admitted;
    const answered: Record = {
      ...r,
      state: 'answered',
      retainedTo: now + ANSWER_RETENTION,
      answer: { principal, possession: proof, authority: authority.map((e) => e.slice()) },
    };
    if (!this.store.put(answered, expectVersion)) return { code: 'invalid_request' };
    return 'answered';
  }

  /** The collect proof first, then pacing, then the record's state. */
  collect(now: bigint, id: Uint8Array, proof: Uint8Array): Answer | LoginRefusal {
    const r = this.store.get(id);
    if (r === undefined || isGone(r, now)) return { code: 'expired_token' };
    if (!verifyCollect(this.audience, loginRequest(r), proof)) return { code: 'invalid_grant' };
    if (r.lastCollect !== undefined && now < r.lastCollect + COLLECT_INTERVAL) return { code: 'slow_down' };
    const paced: Record = { ...r, lastCollect: now };
    if (r.state === 'pending') {
      this.store.put(paced, r.version);
      return { code: 'authorization_pending' };
    }
    if (r.state === 'answered') {
      // The first transition wins; a lost compare-and-set re-reads and hands out the same answer.
      if (!this.store.put({ ...paced, state: 'collected' }, r.version)) {
        const again = this.store.get(id);
        if (again === undefined || isGone(again, now) || again.answer === undefined) return { code: 'expired_token' };
        return again.answer;
      }
      return r.answer!;
    }
    this.store.put(paced, r.version);
    return r.answer!;
  }

  /** Drops what is gone; returns what was dropped. */
  sweep(now: bigint): Uint8Array[] {
    return this.store.sweep(now);
  }
}

/** `AdmitAuthority`: undefined when admitted, the refusal otherwise; refused answers change nothing. */
export function admitAuthority(
  root: grant.Root,
  aud: string,
  r: Record,
  principal: Uint8Array,
  proof: Uint8Array,
  authority: Uint8Array[],
  now: bigint,
): LoginRefusal | undefined {
  if (principal.length !== 32) return { code: 'invalid_grant' };
  if (!verifyLogin(principal, aud, loginRequest(r), proof)) return { code: 'invalid_grant' };
  const time: grant.Time = { present: true, now };
  const held = grant.inspect(root, authority, time);
  if (grant.isRefusal(held)) return { code: 'invalid_grant', grant: held };
  if (!equal(held.subject, r.browser)) return { code: 'invalid_grant' };
  const leafOpened = grant.open(authority[authority.length - 1]!);
  if (typeof leafOpened === 'string') return { code: 'invalid_grant' };
  const leaf = leafOpened.grant;
  const hop = authority.length - 1;
  if (equal(r.browser, principal)) {
    // The holder as itself: its own chain must cover every requested action at every requested scope.
    for (const action of r.terms.actions) {
      for (const scope of r.terms.scope) {
        const covered = grant.verify(root, authority, { domain: root.domain, action, scope }, time);
        if (grant.isRefusal(covered)) return { code: 'invalid_grant', grant: covered };
      }
    }
    return undefined;
  }
  if (!equal(leafOpened.issuer, principal)) return { code: 'invalid_grant', grant: { code: 'issuer_mismatch', hop } };
  if (!leaf.actions.every((a) => r.terms.actions.includes(a)))
    return { code: 'invalid_grant', grant: { code: 'widened_actions', hop } };
  if (!leaf.delegable.every((d) => r.terms.delegable.includes(d)))
    return { code: 'invalid_grant', grant: { code: 'widened_actions', hop } };
  if (!leaf.scope.every((s) => r.terms.scope.some((t) => grant.covers(t, s))))
    return { code: 'invalid_grant', grant: { code: 'widened_scope', hop } };
  if (leaf.depth > r.terms.depth) return { code: 'invalid_grant', grant: { code: 'widened_depth', hop } };
  if (!leaf.validity.finite || leaf.validity.expiresAt > now + BigInt(r.validFor) + ISSUER_CLOCK_SKEW) {
    return { code: 'invalid_grant', grant: { code: 'widened_validity', hop } };
  }
  return undefined;
}
