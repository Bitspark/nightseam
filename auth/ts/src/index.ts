/**
 * The optional authority profile of Nightseam: `grant` — the rooted,
 * attenuating grant in an Archon envelope, its chain rules and the pure
 * evaluator (docs/auth/grant.md, held to conformance/tables/auth-grant.json);
 * `connection` — the audience, the possession proof, the challenge/prove
 * exchange, the decision at every call and the bootstrap
 * (docs/auth/connection.md, auth-boot.json); `exposure` — a policy of one
 * treatment per declared member bound whole to a generated surface and
 * decided at dispatch and at the owner's effect (docs/auth/exposure.md,
 * auth-exposure.json).
 *
 * It is a package of its own so that the components stay free of every
 * identity layer: a consumer that adopts no authority profile installs
 * nothing for one. Its identity layer is Archon — `@bitspark/archon` for the
 * Ed25519 floor and the envelope, `@bitspark/archon-sdk` for possession and
 * login — and beside it only the SHA-256 the grant's digest is, which Archon
 * does not export. Every module is pure and loads no transport, in a browser
 * or anywhere; each is also its own entry point, `@nightseam/auth/grant`,
 * `@nightseam/auth/connection`, `@nightseam/auth/exposure`.
 */
export * as grant from './grant.ts';
export * as connection from './connection.ts';
export * as exposure from './exposure.ts';
