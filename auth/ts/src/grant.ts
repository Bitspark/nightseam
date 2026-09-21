/**
 * The grant — docs/auth/grant.md, held to conformance/tables/auth-grant.json.
 *
 * A body in an Archon envelope sealed in `nightseam-grant/1`; a chain of
 * them, root first, held to a request by one pure function; issuance under
 * the issuer's own chain. Every refusal is a code and a hop index; nothing
 * here does I/O, reads a clock or keeps state.
 */
import { getPublicKey } from '@bitspark/archon';
import { open as openEnvelope, seal as sealEnvelope } from '@bitspark/archon-sdk';
import { sha256 } from '@noble/hashes/sha2.js';
import { Reader, compare, concat, equal, fromUtf8, hasControl, u16, u64, utf8, wellFormed } from './bytes.ts';

export const DOMAIN = 'nightseam-grant/1';

/** The bounds of docs/auth/grant.md, by which the work of every function is finite. */
export const MAX_DEPTH = 15;
export const MAX_CHAIN = 16;
export const MAX_ENTRIES = 64;
export const MAX_ENTRY_BYTES = 1024;
export const MAX_BODY_BYTES = 65535;
export const MAX_DOMAIN_BYTES = 255;

const BODY_VERSION = 0x01;

export type Validity = { finite: false } | { finite: true; expiresAt: bigint };

export interface Grant {
  domain: string;
  subject: Uint8Array;
  parent?: Uint8Array;
  scope: string[];
  actions: string[];
  delegable: string[];
  depth: number;
  validity: Validity;
}

export interface Root {
  key: Uint8Array;
  domain: string;
}

export interface Request {
  domain: string;
  action: string;
  scope: string;
}

export type Time = { present: false } | { present: true; now: bigint };

export interface Verified {
  subject: Uint8Array;
  depth: number;
  validity: Validity;
  hops: number;
}

export type Code =
  | 'malformed'
  | 'unsupported_version'
  | 'envelope_invalid'
  | 'chain_too_long'
  | 'domain_mismatch'
  | 'root_mismatch'
  | 'parent_mismatch'
  | 'issuer_mismatch'
  | 'widened_actions'
  | 'widened_scope'
  | 'widened_depth'
  | 'widened_validity'
  | 'not_covered'
  | 'time_required'
  | 'expired';

export const CODES: readonly Code[] = [
  'malformed',
  'unsupported_version',
  'envelope_invalid',
  'chain_too_long',
  'domain_mismatch',
  'root_mismatch',
  'parent_mismatch',
  'issuer_mismatch',
  'widened_actions',
  'widened_scope',
  'widened_depth',
  'widened_validity',
  'not_covered',
  'time_required',
  'expired',
];

export interface Refusal {
  code: Code;
  hop: number;
}

export function isRefusal(value: Verified | Refusal): value is Refusal {
  return 'code' in value;
}

/** What `encode` throws for a body outside the rules: a code, never prose about the body. */
export class GrantError extends Error {
  readonly code: Code;
  constructor(code: Code) {
    super(code);
    this.name = 'GrantError';
    this.code = code;
  }
}

// ---- the body ------------------------------------------------------------------------

/** A domain or an entry: UTF-8, no control character, within its byte bound. */
function textBytes(text: unknown, max: number): Uint8Array | undefined {
  if (typeof text !== 'string' || !wellFormed(text) || hasControl(text)) return undefined;
  const bytes = utf8(text);
  if (bytes.length === 0 || bytes.length > max) return undefined;
  return bytes;
}

/** A list of entries: at most 64, each within bound, strictly ascending by bytes. */
function listBytes(entries: unknown): Uint8Array[] | undefined {
  if (!Array.isArray(entries) || entries.length > MAX_ENTRIES) return undefined;
  const out: Uint8Array[] = [];
  for (const entry of entries) {
    const bytes = textBytes(entry, MAX_ENTRY_BYTES);
    if (bytes === undefined) return undefined;
    if (out.length > 0 && compare(out[out.length - 1]!, bytes) >= 0) return undefined;
    out.push(bytes);
  }
  return out;
}

function encodeList(entries: Uint8Array[]): Uint8Array {
  const parts: Uint8Array[] = [u16(entries.length)];
  for (const e of entries) parts.push(u16(e.length), e);
  return concat(...parts);
}

function within(inner: Uint8Array[], outer: Uint8Array[]): boolean {
  return inner.every((e) => outer.some((o) => equal(e, o)));
}

/** `a/b` covers `a/b` and everything under `a/b/`. */
export function covers(entry: string, scope: string): boolean {
  return scope === entry || scope.startsWith(entry + '/');
}

function validValidity(v: unknown): v is Validity {
  if (typeof v !== 'object' || v === null) return false;
  const x = v as { finite?: unknown; expiresAt?: unknown };
  if (x.finite === false) return true;
  return (
    x.finite === true && typeof x.expiresAt === 'bigint' && x.expiresAt >= 0n && x.expiresAt <= 0xffffffffffffffffn
  );
}

/** The one encoding of a grant; throws `GrantError('malformed')` for a body outside the rules. */
export function encode(g: Grant): Uint8Array {
  const domain = textBytes(g.domain, MAX_DOMAIN_BYTES);
  const scope = listBytes(g.scope);
  const actions = listBytes(g.actions);
  const delegable = listBytes(g.delegable);
  if (
    domain === undefined ||
    scope === undefined ||
    actions === undefined ||
    delegable === undefined ||
    !(g.subject instanceof Uint8Array) ||
    g.subject.length !== 32 ||
    (g.parent !== undefined && (!(g.parent instanceof Uint8Array) || g.parent.length !== 32)) ||
    !Number.isInteger(g.depth) ||
    g.depth < 0 ||
    g.depth > MAX_DEPTH ||
    !validValidity(g.validity) ||
    !within(delegable, actions)
  ) {
    throw new GrantError('malformed');
  }
  const body = concat(
    new Uint8Array([BODY_VERSION, domain.length]),
    domain,
    g.subject,
    g.parent === undefined ? new Uint8Array([0x00]) : concat(new Uint8Array([0x01]), g.parent),
    encodeList(scope),
    encodeList(actions),
    encodeList(delegable),
    new Uint8Array([g.depth]),
    g.validity.finite ? concat(new Uint8Array([0x01]), u64(g.validity.expiresAt)) : new Uint8Array([0x00]),
  );
  if (body.length > MAX_BODY_BYTES) throw new GrantError('malformed');
  return body;
}

function readList(r: Reader): string[] | undefined {
  const count = r.u16();
  if (count === undefined || count > MAX_ENTRIES) return undefined;
  const out: string[] = [];
  let previous: Uint8Array | undefined;
  for (let i = 0; i < count; i++) {
    const n = r.u16();
    if (n === undefined || n === 0 || n > MAX_ENTRY_BYTES) return undefined;
    const bytes = r.take(n);
    if (bytes === undefined) return undefined;
    const text = fromUtf8(bytes);
    if (text === undefined || hasControl(text)) return undefined;
    if (previous !== undefined && compare(previous, bytes) >= 0) return undefined;
    previous = bytes;
    out.push(text);
  }
  return out;
}

/** Total: a grant, or the code that says why these bytes are not one. */
export function decode(body: Uint8Array): Grant | Code {
  if (body.length === 0 || body.length > MAX_BODY_BYTES) return 'malformed';
  if (body[0] !== BODY_VERSION) return 'unsupported_version';
  const r = new Reader(body);
  r.u8();
  const dlen = r.u8();
  if (dlen === undefined || dlen === 0) return 'malformed';
  const domainBytes = r.take(dlen);
  if (domainBytes === undefined) return 'malformed';
  const domain = fromUtf8(domainBytes);
  if (domain === undefined || hasControl(domain)) return 'malformed';
  const subject = r.take(32);
  if (subject === undefined) return 'malformed';
  const parentKind = r.u8();
  let parent: Uint8Array | undefined;
  if (parentKind === 0x01) {
    parent = r.take(32);
    if (parent === undefined) return 'malformed';
  } else if (parentKind !== 0x00) {
    return 'malformed';
  }
  const scope = readList(r);
  const actions = readList(r);
  const delegable = readList(r);
  if (scope === undefined || actions === undefined || delegable === undefined) return 'malformed';
  if (!delegable.every((d) => actions.includes(d))) return 'malformed';
  const depth = r.u8();
  if (depth === undefined || depth > MAX_DEPTH) return 'malformed';
  const validityKind = r.u8();
  let validity: Validity;
  if (validityKind === 0x00) {
    validity = { finite: false };
  } else if (validityKind === 0x01) {
    const expiresAt = r.u64();
    if (expiresAt === undefined) return 'malformed';
    validity = { finite: true, expiresAt };
  } else {
    return 'malformed';
  }
  if (r.remaining !== 0) return 'malformed';
  return { domain, subject, parent, scope, actions, delegable, depth, validity };
}

// ---- the envelope --------------------------------------------------------------------

/** Seals the encoded body by the issuer's key in the grant domain. */
export function seal(issuerSeed: Uint8Array, g: Grant): Uint8Array {
  return sealEnvelope(issuerSeed, DOMAIN, encode(g));
}

/** Opens an envelope in the grant domain: the issuer and the grant, or the code. */
export function open(envelope: Uint8Array): { issuer: Uint8Array; grant: Grant } | Code {
  let opened: { pubkey: Uint8Array; payload: Uint8Array };
  try {
    opened = openEnvelope(envelope, DOMAIN);
  } catch {
    return 'envelope_invalid';
  }
  const grant = decode(opened.payload);
  if (typeof grant === 'string') return grant;
  return { issuer: opened.pubkey, grant };
}

/** The SHA-256 of the whole envelope: what a child names as its parent. */
export function digest(envelope: Uint8Array): Uint8Array {
  return sha256(envelope);
}

// ---- the chain ------------------------------------------------------------------------

interface Hop {
  issuer: Uint8Array;
  grant: Grant;
  digest: Uint8Array;
}

/** Steps 1 and 2 of the evaluation: length, then each hop's envelope, ancestry, domain and attenuation. */
function openChain(root: Root, chain: Uint8Array[]): Hop[] | Refusal {
  if (chain.length === 0) return { code: 'malformed', hop: 0 };
  if (chain.length > MAX_CHAIN) return { code: 'chain_too_long', hop: 0 };
  const hops: Hop[] = [];
  for (let i = 0; i < chain.length; i++) {
    const opened = open(chain[i]!);
    if (typeof opened === 'string') return { code: opened, hop: i };
    const { issuer, grant } = opened;
    const previous = hops[i - 1];
    if (previous === undefined) {
      if (grant.parent !== undefined) return { code: 'parent_mismatch', hop: i };
      if (!equal(issuer, root.key)) return { code: 'root_mismatch', hop: i };
    } else {
      if (grant.parent === undefined || !equal(grant.parent, previous.digest))
        return { code: 'parent_mismatch', hop: i };
      if (!equal(issuer, previous.grant.subject)) return { code: 'issuer_mismatch', hop: i };
    }
    if (grant.domain !== root.domain) return { code: 'domain_mismatch', hop: i };
    if (previous !== undefined) {
      const widened = attenuates(previous.grant, grant);
      if (widened !== undefined) return { code: widened, hop: i };
    }
    hops.push({ issuer, grant, digest: digest(chain[i]!) });
  }
  return hops;
}

/** The attenuation rule between a parent and its child, in the packet's order. */
function attenuates(parent: Grant, child: Grant): Code | undefined {
  if (!(parent.depth > 0 && child.depth < parent.depth)) return 'widened_depth';
  if (!child.actions.every((a) => parent.delegable.includes(a))) return 'widened_actions';
  if (!child.scope.every((s) => parent.scope.some((p) => covers(p, s)))) return 'widened_scope';
  if (parent.validity.finite) {
    if (!child.validity.finite || child.validity.expiresAt > parent.validity.expiresAt) return 'widened_validity';
  }
  return undefined;
}

/** Step 3: the request covered at every hop, delegable at every hop but the last. */
function coverage(root: Root, hops: Hop[], request: Request): Refusal | undefined {
  if (request.domain !== root.domain) return { code: 'domain_mismatch', hop: 0 };
  for (let i = 0; i < hops.length; i++) {
    const g = hops[i]!.grant;
    const last = i === hops.length - 1;
    if (!g.actions.includes(request.action)) return { code: 'not_covered', hop: i };
    if (!g.scope.some((s) => covers(s, request.scope))) return { code: 'not_covered', hop: i };
    if (!last && !g.delegable.includes(request.action)) return { code: 'not_covered', hop: i };
  }
  return undefined;
}

/** Step 4: one decision time, required where any hop is finite, strictly before each finite expiry. */
function time(hops: Hop[], now: Time): Refusal | undefined {
  const first = hops.findIndex((h) => h.grant.validity.finite);
  if (first < 0) return undefined;
  if (!now.present) return { code: 'time_required', hop: first };
  for (let i = 0; i < hops.length; i++) {
    const v = hops[i]!.grant.validity;
    if (v.finite && now.now >= v.expiresAt) return { code: 'expired', hop: i };
  }
  return undefined;
}

function verified(hops: Hop[]): Verified {
  const leaf = hops[hops.length - 1]!.grant;
  let validity: Validity = { finite: false };
  for (const h of hops) {
    const v = h.grant.validity;
    if (v.finite && (!validity.finite || v.expiresAt < validity.expiresAt))
      validity = { finite: true, expiresAt: v.expiresAt };
  }
  return { subject: leaf.subject, depth: leaf.depth, validity, hops: hops.length };
}

/** Steps 1, 2 and 4: the chain held without a request. */
export function inspect(root: Root, chain: Uint8Array[], now: Time): Verified | Refusal {
  const hops = openChain(root, chain);
  if (!Array.isArray(hops)) return hops;
  const late = time(hops, now);
  if (late !== undefined) return late;
  return verified(hops);
}

/** The whole evaluation: the chain held to the request at this time. */
export function verify(root: Root, chain: Uint8Array[], request: Request, now: Time): Verified | Refusal {
  const hops = openChain(root, chain);
  if (!Array.isArray(hops)) return hops;
  const uncovered = coverage(root, hops, request);
  if (uncovered !== undefined) return uncovered;
  const late = time(hops, now);
  if (late !== undefined) return late;
  return verified(hops);
}

// ---- issuance -------------------------------------------------------------------------

/**
 * Signs a child under the issuer's own chain — root first, ending in a grant
 * to the issuer — after holding that chain as `inspect` does and the child to
 * the attenuation rule against its leaf. The child's parent is the leaf's
 * digest, set here; with `inherit` its validity resolves to the leaf's before
 * signing. A root grant has an empty chain, is issued by the root key alone
 * and states its validity. Deterministic: the same inputs seal the same bytes.
 */
export function issue(
  root: Root,
  parents: Uint8Array[],
  issuerSeed: Uint8Array,
  child: Grant,
  inherit: boolean,
  now: Time,
): Uint8Array | Refusal {
  let issuerKey: Uint8Array;
  try {
    issuerKey = getPublicKey(issuerSeed);
  } catch {
    return { code: 'malformed', hop: 0 };
  }
  const hop = parents.length;
  let grant: Grant;
  if (parents.length === 0) {
    if (inherit) return { code: 'malformed', hop: 0 };
    if (child.parent !== undefined) return { code: 'parent_mismatch', hop: 0 };
    if (!equal(issuerKey, root.key)) return { code: 'root_mismatch', hop: 0 };
    grant = { ...child };
  } else {
    const hops = openChain(root, parents);
    if (!Array.isArray(hops)) return hops;
    const late = time(hops, now);
    if (late !== undefined) return late;
    const leaf = hops[hops.length - 1]!;
    if (!equal(issuerKey, leaf.grant.subject)) return { code: 'issuer_mismatch', hop };
    if (child.parent !== undefined && !equal(child.parent, leaf.digest)) return { code: 'parent_mismatch', hop };
    grant = { ...child, parent: leaf.digest, validity: inherit ? leaf.grant.validity : child.validity };
    const widened = attenuates(leaf.grant, grant);
    if (widened !== undefined) return { code: widened, hop };
  }
  try {
    return seal(issuerSeed, grant);
  } catch (error) {
    if (error instanceof GrantError) return { code: error.code, hop };
    return { code: 'malformed', hop };
  }
}
