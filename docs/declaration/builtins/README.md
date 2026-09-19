# Built-in family references

These specifications are rendered from the declarations embedded in the
generator by the same `spec` target that documents a consumer's family:

- [duplex](duplex/README.md): the profile's envelope and channel handle.
- [tunnel](tunnel/README.md): opening channels, credit, and their wire types.

Their source addresses begin with `nightseam:`, so a declaration or
diagnostic referring to one cannot be mistaken for a file in the consumer's
`api/contracts/`. A tier carries its built-in family implicitly; [a tier is
a built-in family](../../decisions/a-tier-is-a-built-in-family.md) explains
the relationship.

The fast Go test tier checks these documents against the embedded
declarations. Regenerate them after changing a built-in or the spec target:

```sh
go test ./cmd/nightseam -short -run TestBuiltinSpecificationsGolden -update
```

The repository's regular golden update command includes this test too.
