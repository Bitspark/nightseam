import type { AdapterContext as RuntimeAdapterContext, TypeBinding } from '@nightseam/runtime';
import type { LiveOwner, LiveScope } from './index.ts';

/** Explicit lifetime environment; selection and mounting never infer its scope. */
export interface AdapterContext extends RuntimeAdapterContext {
  readonly scope?: LiveScope;
  readonly owner?: LiveOwner;
}

/** A reusable value conversion; each invocation supplies its active owner. */
export interface ValueAdapter<T> {
  readonly binding: TypeBinding;
  readonly live: boolean;
  readonly export: (owner: LiveOwner | undefined, value: T) => unknown;
  readonly import: (owner: LiveOwner | undefined, value: unknown) => T;
}

type CarriesLive<T, Seen = never> = [T] extends [Seen]
  ? false
  : T extends (...args: never[]) => unknown
    ? true
    : T extends readonly (infer Item)[]
      ? CarriesLive<Item, Seen | T>
      : T extends object
        ? true extends { [K in keyof T]: CarriesLive<T[K], Seen | T> }[keyof T]
          ? true
          : false
        : false;

/** Specializing a generic value preserves the concrete operation's lifetime surface. */
export type ValueContext<T, Context> = true extends CarriesLive<T> ? Context & { owner: LiveOwner } : Context;
export type ValueOptions<T, Options> = true extends CarriesLive<T> ? Options & { owner?: LiveOwner } : Options;

/** Data validation and conversion have no dependency on an owner's lifetime. */
export function jsonAdapter<T>(binding: TypeBinding): ValueAdapter<T> {
  return {
    binding,
    live: false,
    export(_owner, value) {
      binding.validate(binding.type, value, '$', binding.slots);
      return value;
    },
    import(_owner, value) {
      binding.validate(binding.type, value, '$', binding.slots);
      return value as T;
    },
  };
}
