# @nightseam/auth

```sh
npm install @nightseam/auth
```

The optional authority profile for Nightseam: `grant`, the rooted and
attenuating grant in an Archon envelope with its chain rules and pure
evaluator; `connection`, the audience a connection binds to, the possession
proof, the `auth.challenge` / `auth.prove` exchange that establishes one
immutable context per connection, the decision at every protected call, and
the bootstrap that turns a login into a grant; and `exposure`, a policy of
one treatment per declared member bound whole to a generated surface and
decided at dispatch and again at the owner's effect. Each is specified in
[`docs/auth/`](../../docs/auth/) and held, byte for byte, to the tables under
[`conformance/tables/`](../../conformance/tables/) — `auth-grant.json`,
`auth-boot.json`, `auth-exposure.json` — that both languages' verifiers
reproduce.

It is a package of its own so that `@nightseam/duplex`, `@nightseam/runtime`,
`@nightseam/tunnel` and `@nightseam/live` stay free of every identity layer:
a consumer that adopts no authority profile installs nothing for one. Its
identity layer is [Archon](https://github.com/Bitspark/archon) —
`@bitspark/archon` for the Ed25519 floor and the envelope,
`@bitspark/archon-sdk` for possession and login, Apache-2.0, pinned at one
release — and it depends on nothing else beside the runtime it composes
onto. The pure modules load no transport, in a browser or anywhere.

This is the package's coordinate under Nightseam's lockstep releases; the
modules land by their packets.
