import { DuplexError } from './error.ts';
import type { CallOptions } from './peer.ts';
import { scalarString } from './unicode.ts';

/** The ordinary identity request, independent of transport negotiation. */
export const IDENTITY_METHOD = 'identity.check';

/** A declaration path and its generated digest, when the revision is specified. */
export interface DeclarationIdentity {
  path: string;
  digest?: string | undefined;
}

/** Checks identity before the caller exposes its model. The supplied call honors
 * its options; absent an explicit timeout, the exchange has a 30-second limit.
 * Only method_not_found means absent identity. No refusal closes the carrier. */
export async function checkIdentity(
  call: (method: string, params: unknown, options?: CallOptions) => Promise<unknown>,
  expected: DeclarationIdentity,
  options?: CallOptions,
): Promise<void> {
  const local = ownIdentity(expected);
  let answer: unknown;
  try {
    answer = await call(IDENTITY_METHOD, local, {
      ...options,
      timeoutMs: options?.timeoutMs === undefined ? 30_000 : options.timeoutMs,
    });
  } catch (error) {
    if (error instanceof DuplexError && error.code === 'method_not_found') return;
    throw error;
  }
  compareIdentity(local, readIdentity(answer));
}

/** Snapshots a declaration and answers identity requests without running model
 * code. Install this handler before exposing that model. */
export function identityHandler(expected: DeclarationIdentity): (raw: unknown) => DeclarationIdentity {
  const local = ownIdentity(expected);
  return (raw) => {
    compareIdentity(local, readIdentity(raw));
    return { ...local };
  };
}

function invalid(message: string): never {
  throw new DuplexError('contract_invalid', message);
}

function ownIdentity(expected: DeclarationIdentity): DeclarationIdentity {
  // Optional undefined is the native absence spelling; it never becomes a wire member.
  return readIdentity(expected, true);
}

function readIdentity(raw: unknown, native = false): DeclarationIdentity {
  if (typeof raw !== 'object' || raw === null || Array.isArray(raw)) {
    return invalid('a declaration identity is an object with path and optional digest');
  }
  const members = raw as Record<string, unknown>;
  for (const key of Object.keys(members).sort()) {
    if (key !== 'path' && key !== 'digest') return invalid('identity: unknown field ' + key);
  }
  const path = Object.hasOwn(members, 'path') ? members['path'] : undefined;
  if (typeof path !== 'string' || path === '') return invalid('an identity names a nonempty Unicode declaration path');
  try {
    scalarString(path);
  } catch {
    return invalid('an identity names a nonempty Unicode declaration path');
  }
  if (Object.hasOwn(members, 'digest') && !(native && members['digest'] === undefined)) {
    const digest = members['digest'];
    if (typeof digest !== 'string' || !/^[0-9a-f]{64}$/.test(digest))
      return invalid('a declaration digest is lowercase SHA-256 hex');
    return { path, digest };
  }
  return { path };
}

function compareIdentity(expected: DeclarationIdentity, remote: DeclarationIdentity): void {
  if (
    expected.path !== remote.path ||
    (expected.digest !== undefined && remote.digest !== undefined && expected.digest !== remote.digest)
  ) {
    throw new DuplexError('contract_mismatch', 'the declaration identity for ' + expected.path + ' differs');
  }
}
