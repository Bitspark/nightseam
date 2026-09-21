/**
 * The exposure — docs/auth/exposure.md, held to
 * conformance/tables/auth-exposure.json.
 *
 * A policy of one treatment per declared member, bound whole to a surface
 * or refused with every gap named; the request as a selector, never as
 * authority; the decision at dispatch and again at the owner's effect;
 * exported references decided by their export record under the invoking
 * connection; emissions decided per recipient. Pure over its arguments, the
 * export records the only state, held by the binding that made them.
 */
import { equal } from './bytes.ts';
import { call, type Context, type Refusal as ConnectionRefusal } from './connection.ts';
import * as grant from './grant.ts';

export interface Member {
  key: string;
  fields: string[];
}

export interface Surface {
  family: string;
  digest: string;
  members: Member[];
}

export type Kind = 'guarded' | 'public' | 'denied';

export interface Treatment {
  kind: Kind;
  action?: string;
  scope?: string;
}

export interface Policy {
  family: string;
  digest: string;
  treatments: { [member: string]: Treatment };
}

export type ConstructionCode =
  'auth.contract_mismatch' | 'auth.member_unbound' | 'auth.member_undeclared' | 'auth.template_invalid';

export interface ConstructionError {
  code: ConstructionCode;
  members?: string[];
}

export type Code =
  | ConnectionRefusal['code']
  | 'auth.unknown_member'
  | 'auth.member_denied'
  | 'auth.selector_invalid'
  | 'auth.reference_unknown';

/** The owner's own condition, refused with the owner's own code. */
export type OwnerCode = `owner:${string}`;

export interface Refusal {
  code: Code | OwnerCode;
  grant?: grant.Refusal;
}

export interface Decision {
  member: string;
  kind: Kind;
  subject?: Uint8Array;
  action?: string;
  scope?: string;
  grant?: grant.Verified;
}

export function isRefusal(value: Decision | Refusal): value is Refusal {
  return 'code' in value;
}

/** A hole `{name}` in a scope template. */
const hole = /\{([^{}]*)\}/g;

function hasControl(text: string): boolean {
  for (let i = 0; i < text.length; i++) {
    const c = text.charCodeAt(i);
    if (c < 0x20 || c === 0x7f) return true;
  }
  return false;
}

/**
 * `render` fills a template's holes from the request payload — a string, or
 * an integer spelled in decimal — or from the export record; the value must
 * be one scope segment. What comes out is a scope entry, or undefined.
 */
export function render(
  template: string,
  payload: { [field: string]: unknown },
  exportScope: string,
): string | undefined {
  let refused = false;
  const out = template.replace(hole, (_, name: string) => {
    if (name === 'export') {
      if (exportScope === '') refused = true;
      return exportScope;
    }
    const value = payload[name];
    let text: string;
    if (typeof value === 'string') text = value;
    else if (typeof value === 'number' && Number.isInteger(value)) text = String(value);
    else if (typeof value === 'bigint') text = value.toString();
    else {
      refused = true;
      return '';
    }
    if (text === '' || text.includes('/') || hasControl(text)) {
      refused = true;
      return '';
    }
    return text;
  });
  if (refused || out.length > grant.MAX_ENTRY_BYTES) return undefined;
  return out;
}

/** A constructed exposure: every member has exactly one treatment. */
export class Binding {
  readonly surface: Surface;
  readonly policy: Policy;
  readonly #members = new Map<string, Member>();
  readonly #exports = new Map<string, { member: string; scope: string }>();

  private constructor(surface: Surface, policy: Policy) {
    this.surface = surface;
    this.policy = policy;
    for (const m of surface.members) this.#members.set(m.key, m);
  }

  /** Holds the policy to the surface whole; constructs nothing on refusal, and names every member a refusal is about. */
  static bind(surface: Surface, policy: Policy): Binding | ConstructionError {
    if (policy.family !== surface.family || policy.digest !== surface.digest) return { code: 'auth.contract_mismatch' };
    const unbound: string[] = [];
    const undeclared: string[] = [];
    const invalid: string[] = [];
    const declared = new Set(surface.members.map((m) => m.key));
    for (const m of surface.members) {
      const t = policy.treatments[m.key];
      if (t === undefined) {
        unbound.push(m.key);
        continue;
      }
      if (!validTreatment(m, t)) invalid.push(m.key);
    }
    for (const key of Object.keys(policy.treatments)) {
      if (!declared.has(key)) undeclared.push(key);
    }
    unbound.sort();
    undeclared.sort();
    invalid.sort();
    if (unbound.length > 0) return { code: 'auth.member_unbound', members: unbound };
    if (undeclared.length > 0) return { code: 'auth.member_undeclared', members: undeclared };
    if (invalid.length > 0) return { code: 'auth.template_invalid', members: invalid };
    return new Binding(surface, policy);
  }

  /** Exactly the declared members, each once. */
  routes(): string[] {
    return [...this.#members.keys()].sort();
  }

  /** The decision at dispatch, in the packet's order. */
  decide(
    root: grant.Root,
    member: string,
    payload: { [field: string]: unknown },
    exportScope: string,
    ctx: Context | undefined,
    now: grant.Time,
  ): Decision | Refusal {
    const m = this.#members.get(member);
    if (m === undefined) return { code: 'auth.unknown_member' };
    const t = this.policy.treatments[m.key]!;
    if (t.kind === 'denied') return { code: 'auth.member_denied' };
    if (t.kind === 'public')
      return ctx === undefined
        ? { member: m.key, kind: 'public' }
        : { member: m.key, kind: 'public', subject: ctx.subject };
    if (ctx === undefined) return { code: 'auth.unauthenticated' };
    const scope = render(t.scope!, payload, exportScope);
    if (scope === undefined) return { code: 'auth.selector_invalid' };
    const decided = call(root, ctx, { domain: root.domain, action: t.action!, scope }, now);
    if ('code' in decided) return decided;
    return { member: m.key, kind: 'guarded', subject: ctx.subject, action: t.action!, scope, grant: decided };
  }

  /** The decision at the effect: the same decision at the effect's own time, then the owner's condition over the resolved target. */
  effect(
    root: grant.Root,
    decision: Decision,
    ctx: Context | undefined,
    now: grant.Time,
    condition?: Condition,
  ): Refusal | undefined {
    if (decision.kind === 'guarded') {
      // The decision is the context's: one made under another connection is nobody's here.
      if (ctx === undefined) return { code: 'auth.unauthenticated' };
      if (decision.subject === undefined || !equal(decision.subject, ctx.subject))
        return { code: 'auth.subject_mismatch' };
      const again = call(root, ctx, { domain: root.domain, action: decision.action!, scope: decision.scope! }, now);
      if ('code' in again) return again;
    }
    if (condition !== undefined && decision.scope !== undefined) {
      const verdict = condition(decision.scope);
      if (!verdict.ok) return { code: `owner:${verdict.reason}` };
    }
    return undefined;
  }

  /**
   * Records what a callable reference is to this exposure: which callable
   * member, at which scope. Recording the same again is nothing; recording
   * a reference as something else is refused — a record is not rewritten.
   */
  export(ref: string, member: string, scope: string): boolean {
    const m = this.#members.get(member);
    if (ref === '' || m === undefined || !member.startsWith('callable:')) return false;
    if (scope === '' || hasControl(scope) || scope.length > grant.MAX_ENTRY_BYTES) return false;
    const existing = this.#exports.get(ref);
    if (existing !== undefined && (existing.member !== member || existing.scope !== scope)) return false;
    this.#exports.set(ref, { member, scope });
    return true;
  }

  /** `decide` for a reference this exposure exported, under the invoking connection's context. */
  invoke(
    root: grant.Root,
    ref: string,
    payload: { [field: string]: unknown },
    ctx: Context | undefined,
    now: grant.Time,
  ): Decision | Refusal {
    const record = this.#exports.get(ref);
    if (record === undefined) return { code: 'auth.reference_unknown' };
    return this.decide(root, record.member, payload, record.scope, ctx, now);
  }

  /** An event decided per recipient at emission time: delivered where admitted, dropped where refused. */
  emit(
    root: grant.Root,
    event: string,
    data: { [field: string]: unknown },
    to: Recipient[],
    now: grant.Time,
  ): Delivery[] {
    return to.map((r) => {
      const decided = this.decide(root, event, data, '', r.ctx, now);
      return { recipient: r.name, refused: 'code' in decided ? decided : undefined };
    });
  }
}

/** The owner's own predicate over the resolved target, evaluated inside its transaction. */
export type Condition = (scope: string) => { ok: true } | { ok: false; reason: string };

export interface Recipient {
  name: string;
  ctx: Context | undefined;
}

export interface Delivery {
  recipient: string;
  refused?: Refusal;
}

export const bind = Binding.bind;

function validTreatment(m: Member, t: Treatment): boolean {
  if (t.kind === 'guarded') {
    if (typeof t.action !== 'string' || t.action === '' || hasControl(t.action)) return false;
    if (typeof t.scope !== 'string' || t.scope === '' || hasControl(t.scope)) return false;
    for (const match of t.scope.matchAll(hole)) {
      const name = match[1]!;
      if (name === 'export') {
        if (!m.key.startsWith('callable:')) return false;
      } else if (!m.fields.includes(name)) {
        return false;
      }
    }
    return true;
  }
  if (t.kind === 'public' || t.kind === 'denied') return t.action === undefined && t.scope === undefined;
  return false;
}
