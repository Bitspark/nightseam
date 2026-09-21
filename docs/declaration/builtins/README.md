# Built-in family references

These specifications are rendered from the declarations embedded in the
generator by the same `spec` target that documents a consumer's family:

- [auth](auth/README.md): the exchange that gives a connection its one
  context — a challenge, then a proof of possession over it with the
  chain of grants — of the optional authority profile.
- [duplex](duplex/README.md): the profile's envelope and channel handle.
- [identity](identity/README.md): declaration agreement before model interpretation.
- [live](live/README.md): invoking and releasing a binding, and the reference
  a callable value travels as.
- [tunnel](tunnel/README.md): opening channels, credit, and their wire types.

Their source addresses begin with `nightseam:`, so a declaration or
diagnostic referring to one cannot be mistaken for a file in the consumer's
`api/contracts/`. A tier carries its built-in family implicitly; [a tier is
a built-in family](../../decisions/a-tier-is-a-built-in-family.md) explains
the relationship.

`auth` is the vocabulary of [the authenticated
connection](../../auth/connection.md), implemented by the optional
`auth/` packages over a peer and by nothing in the runtime: bare Nightseam
refuses `auth.challenge` and `auth.prove` as any unknown method, and a
consumer's family may not declare an operation under the `auth.` prefix.

`identity` is bootstrap vocabulary implemented by the runtime. Its generated
surface includes types, validation and canonical declaration metadata, together
with this specification; it has no application-model adapters. The runtime owns
the `identity.check` receiver that every other generated family adapter uses
before interpreting a Wire. This avoids recursively constructing a model to
check the declaration needed to construct that model
([#382](https://github.com/Bitspark/nightseam/issues/382)).

The fast Go test tier checks these documents against the embedded
declarations. Regenerate them after changing a built-in or the spec target:

```sh
go test ./cmd/nightseam -short -run TestBuiltinSpecificationsGolden -update
```

The repository's regular golden update command includes this test too.
