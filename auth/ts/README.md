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

```ts
import { grant, connection, exposure } from '@nightseam/auth';
// or, without the others: '@nightseam/auth/grant', '/connection', '/exposure'

const root = { key: rootKey, domain: 'example.test' };
const held = grant.verify(root, chain, { domain: 'example.test', action: 'read', scope: 'projects/7' }, { present: true, now });
```

`grant` — `encode`, `decode`, `seal`, `open`, `digest`, `inspect`, `verify`,
`issue`; `connection` — `audience`, `binding`, `prove`, `verify`,
`Connection` (the exchange), `call`, `parseTerms`, `renderTerms`, `Service`
over a `Store` (a `MemoryStore` is the reference shape); `exposure` —
`bind`, `render`, and a `Binding`'s `routes`, `decide`, `effect`, `export`,
`invoke`, `emit`. Bytes are `Uint8Array`, time is `bigint` seconds since the
Unix epoch, and every refusal is a code — the grant's with a hop, the
connection's and the exposure's with the grant's beneath where a chain
refused.

The tests load the three tables and reproduce every case; the count of
passing cases is the count of cases in the file. Archon is pinned at one
release in both languages — `v0.7.0`, from the public registries; the
workspace's `.npmrc` keeps a developer's own scoped configuration from
steering the lockfile anywhere else.
