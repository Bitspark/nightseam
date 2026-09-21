# github.com/Bitspark/nightseam/auth/go

```sh
go get github.com/Bitspark/nightseam/auth/go@latest
```

The optional authority profile for Nightseam's Go runtime: package `grant`,
the rooted and attenuating grant in an Archon envelope with its chain rules
and pure evaluator — `Inspect`, `Verify`, `Issue`; and package `auth`, the
audience a connection binds to, the possession proof, the `auth.challenge` /
`auth.prove` exchange that establishes one immutable context per
connection, the decision at every protected call, the bootstrap that turns
a login into a grant, and the exposure that binds a policy of one treatment
per declared member whole to a generated surface and decides each call at
dispatch and again at the owner's effect. Each is specified in
[`docs/auth/`](../../docs/auth/) and held, byte for byte, to the tables under
[`conformance/tables/`](../../conformance/tables/) — `auth-grant.json`,
`auth-boot.json`, `auth-exposure.json` — that both languages' verifiers
reproduce.

It is a module of its own so that `github.com/Bitspark/nightseam` pulls in
no identity layer: a consumer that adopts no authority profile installs
nothing for one. It requires the root module at its own version and is
released by a tag of its own, `auth/go/vX.Y.Z`, cut beside `vX.Y.Z`. Its
identity layer is [Archon](https://github.com/Bitspark/archon) —
`github.com/Bitspark/archon/core/go` for the Ed25519 floor and the envelope,
`github.com/Bitspark/archon/sdk/go` for possession and login, Apache-2.0,
pinned at one release — and it depends on nothing else beside the runtime
it composes onto. `TestImportDirection` holds the direction: the root module
names nothing of Archon and nothing of this module.

The module is released under Nightseam's lockstep versions. Each package is
held to its table by one test that runs every case — 72, 52 and 35 — and
beyond the tables by fuzz targets that never panic, by the bounds refused
before the work they would cost, and by a check that no package reads a
clock or touches the network: every nonce, id and decision time is an
argument, and the only state is the store a consumer supplies.
