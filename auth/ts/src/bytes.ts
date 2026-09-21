/**
 * Byte helpers the profile's three modules share: hex both ways, UTF-8, the
 * big-endian integers the bodies are laid out in, constant-shape equality and
 * the byte order the grant's lists are held in. Nothing here is
 * cryptographic; the cryptography is Archon's.
 */

const encoder = new TextEncoder();
const decoder = new TextDecoder('utf-8', { fatal: true });

export function utf8(text: string): Uint8Array {
  return encoder.encode(text);
}

/** Decodes strict UTF-8, or returns undefined for bytes that are not it. */
export function fromUtf8(bytes: Uint8Array): string | undefined {
  try {
    return decoder.decode(bytes);
  } catch {
    return undefined;
  }
}

export function toHex(bytes: Uint8Array): string {
  let out = '';
  for (const b of bytes) out += b.toString(16).padStart(2, '0');
  return out;
}

/** Lowercase or uppercase hex of even length, or undefined. */
export function fromHex(text: string): Uint8Array | undefined {
  if (text.length % 2 !== 0 || !/^[0-9a-fA-F]*$/.test(text)) return undefined;
  const out = new Uint8Array(text.length / 2);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(text.slice(2 * i, 2 * i + 2), 16);
  return out;
}

export function concat(...parts: Uint8Array[]): Uint8Array {
  let size = 0;
  for (const p of parts) size += p.length;
  const out = new Uint8Array(size);
  let at = 0;
  for (const p of parts) {
    out.set(p, at);
    at += p.length;
  }
  return out;
}

export function equal(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i++) diff |= a[i]! ^ b[i]!;
  return diff === 0;
}

/** Lexicographic byte order: negative, zero or positive. */
export function compare(a: Uint8Array, b: Uint8Array): number {
  const n = Math.min(a.length, b.length);
  for (let i = 0; i < n; i++) {
    if (a[i] !== b[i]) return a[i]! - b[i]!;
  }
  return a.length - b.length;
}

export function u16(n: number): Uint8Array {
  return new Uint8Array([(n >>> 8) & 0xff, n & 0xff]);
}

export function u64(n: bigint): Uint8Array {
  const out = new Uint8Array(8);
  let v = n;
  for (let i = 7; i >= 0; i--) {
    out[i] = Number(v & 0xffn);
    v >>= 8n;
  }
  return out;
}

/** A control character in U+0000–U+001F or U+007F, which no entry may carry. */
export function hasControl(text: string): boolean {
  for (let i = 0; i < text.length; i++) {
    const c = text.charCodeAt(i);
    if (c < 0x20 || c === 0x7f) return true;
  }
  return false;
}

/** Whether every UTF-16 code unit pairs up: a lone surrogate is not UTF-8. */
export function wellFormed(text: string): boolean {
  for (let i = 0; i < text.length; i++) {
    const c = text.charCodeAt(i);
    if (c >= 0xd800 && c <= 0xdbff) {
      const d = text.charCodeAt(i + 1);
      if (!(d >= 0xdc00 && d <= 0xdfff)) return false;
      i++;
    } else if (c >= 0xdc00 && c <= 0xdfff) {
      return false;
    }
  }
  return true;
}

/** A cursor over bytes that answers undefined past its end, never throws. */
export class Reader {
  at = 0;
  readonly bytes: Uint8Array;
  constructor(bytes: Uint8Array) {
    this.bytes = bytes;
  }
  get remaining(): number {
    return this.bytes.length - this.at;
  }
  u8(): number | undefined {
    if (this.remaining < 1) return undefined;
    return this.bytes[this.at++];
  }
  u16(): number | undefined {
    if (this.remaining < 2) return undefined;
    const v = (this.bytes[this.at]! << 8) | this.bytes[this.at + 1]!;
    this.at += 2;
    return v;
  }
  u64(): bigint | undefined {
    if (this.remaining < 8) return undefined;
    let v = 0n;
    for (let i = 0; i < 8; i++) v = (v << 8n) | BigInt(this.bytes[this.at + i]!);
    this.at += 8;
    return v;
  }
  take(n: number): Uint8Array | undefined {
    if (this.remaining < n) return undefined;
    const out = this.bytes.slice(this.at, this.at + n);
    this.at += n;
    return out;
  }
}
