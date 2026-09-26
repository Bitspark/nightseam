# Repository layout

Use **component first, language second**, following the family
[layout policy](https://github.com/Bitspark/bitwire/blob/main/LAYOUT.md).
This rule applies to new source and to deliberate source-layout migrations.

```text
<repo>/<component>/<lang>/...
<repo>/cmd/<command>/<lang>/...
```

A component can have a semantic hierarchy, such as `core/node/go/` or
`providers/tree/memory/go/`. Choose that hierarchy before the language
directory. Do not organize implementations as repository-root `go/` or `ts/`,
`go/<component>/`, or a language-first `packages/` tree.

Use exactly two lowercase letters: `go`, `ts`, `py`, `rs`, `hs`, `cc`,
`jv`, `sw`; other assigned codes include `rb`, `kt`, `cs` and `sh`.
Bitwire already uses `hs` for Haskell. Service SDKs and their source manifests
also follow their own language registry; register a code there before using it.

Source, native tests and language-specific package metadata belong with the
language implementation. Shared specifications, schemas, vectors, documentation,
assets and deployment configuration stay language-neutral. Repository-root
workspace/build manifests and maintenance scripts may remain at their normal
tooling locations. A source directory does not automatically require its own
module, package publication or repository.

Create a language directory only when it contains a real implementation or
contract presentation. Keep module boundaries, dependencies and release rules
consistent with the repository's charter. When moving existing source, update
imports, manifests, generators, tests, CI and documentation together; never
rewrite an immutable published release or bypass a frozen-foundation policy.

## Adoption in this repository

Existing runtime components demonstrate the pattern: `duplex/{go,ts}`,
`runtime/{go,ts}`, `live/{go,ts}` and `tunnel/{go,ts}`. Existing `cpp`, `java` and
`swift` directories and commands without a language directory are historical
paths, not the spelling for new successor components. Their two-letter codes
are `cc`, `jv` and `sw`.

[Bitwire decision 0010](https://github.com/Bitspark/bitwire/blob/main/docs/decisions/0010-bitwire-holds-the-contract-and-bitruntime-implements-it.md)
places successor work in the new family repositories. Preserve Nightseam's
released source and provenance; this layout policy neither moves its code nor
creates compatibility aliases.
