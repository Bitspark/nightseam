import type { TypeBinding } from './validate.ts';

/** Primitive conversion with no retained operation context or lifetime.
 * Acquiring conversions run inside the chosen environment's matching boundary,
 * using its active build context throughout the whole value traversal.
 */
export interface ValueAdapter<T> {
  readonly binding: TypeBinding;
  readonly needsContext: boolean;
  readonly export: (context: unknown, value: T) => unknown;
  readonly import: (context: unknown, value: unknown) => T;
}

/** Consumer-supplied effects; the runtime never interprets their contexts. */
export interface ValueEnvironment {
  select(context: unknown): unknown;
  child(context: unknown): unknown;
  export(context: unknown, build: (view: unknown) => unknown): unknown;
  import<T>(context: unknown, build: (view: unknown) => T): T;
  publish<T>(context: unknown, build: (view: unknown) => unknown, publish: (raw: unknown) => Promise<T>): Promise<T>;
}

type CarriesContext<T, Seen = never> = [T] extends [Seen]
  ? false
  : T extends (...args: never[]) => unknown
    ? true
    : T extends readonly (infer Item)[]
      ? CarriesContext<Item, Seen | T>
      : T extends object
        ? true extends { [K in keyof T]: CarriesContext<T[K], Seen | T> }[keyof T]
          ? true
          : false
        : false;

/** Specialization preserves an operation's context requirement without choosing its provider. */
export type ValueContext<T, Context> = true extends CarriesContext<T> ? Context & { valueContext: unknown } : Context;
export type ValueOptions<T, Options> = true extends CarriesContext<T> ? Options & { valueContext?: unknown } : Options;

/** Data validates in both directions without consulting any conversion context. */
export function jsonAdapter<T>(binding: TypeBinding): ValueAdapter<T> {
  return {
    binding,
    needsContext: false,
    export(_context, value) {
      binding.validate(binding.type, value, '$', binding.slots);
      return value;
    },
    import(_context, value) {
      binding.validate(binding.type, value, '$', binding.slots);
      return value as T;
    },
  };
}
