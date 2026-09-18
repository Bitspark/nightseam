# Related models — what the neighbouring repositories do that Nightseam does

A cross-repository comparison of type models, generators, wire profiles,
conformance and layering in [aifunc3](repos/aifunc3-models.md),
[bitmachine](repos/bitmachine-models.md) and
[plexis](repos/plexis-models.md), against Nightseam's own contract model
in [internal/contract/api.schema.json](../internal/contract/api.schema.json).
The per-repo pages carry the paths, snippets and line references; this
page carries the shape of the whole and the list of what is worth taking.
Surveyed 2026-09-18 against aifunc3 `e6edab7f9`, bitmachine `f64b23c992`,
plexis `5eb8b254f`.

## Lineage

One stack, three places. The **Glyph / Rune / Mark** stack — a
declaration DSL, a generator with per-language backends, and a
capability-carrying value model with its own session protocol — was
built in aifunc3 (`units/data/glyph`, `units/lib/rune`, `units/lib/mark`)
and ported into bitmachine's kernel as `kernel/glyph` and `kernel/eidos`.
The `plexis-main` branch of bitmachine predates the port: its `SYMMETRY.md`
references a `rune/gen` that is not in the checkout, and Plexis was built
entirely by hand. bitmachine's `kernel/graphe` is a separate, smaller,
JSON-declared attempt at the same problem; `kernel/telos`, `kernel/sema`
and the Plexis / Energeia / Leukos surfaces are hand-written wire
profiles.

Nightseam sits beside all of this as the only one of the four that is
schema-first (a JSON contract validated by a published JSON Schema) and
that emits Go.

## The matrix

| concern | Nightseam | aifunc3 / bitmachine `kernel/{glyph, eidos}` | bitmachine `kernel/graphe` | plexis (+ energeia, leukos, sandbox) |
|---|---|---|---|---|
| **declaration form** | JSON, three layer files (`dto`, `rpc`, `sess`) merged by the loader | `.glyph` surface DSL, one flat file of `type` decls with `role` / `theory` blocks | `frames.json`, `routes.json` — plain JSON, required-field names only | Rust structs + TS interfaces, by hand, twice |
| **schema for the schema** | `urn:nightseam:contract:1`, JSON Schema 2020-12 | none; parser + 31 golden pairs + semantic vectors are the spec | none | none (`LAYERS.md` gives YAML shapes in prose) |
| **type algebra** | six primitives; `record` (`extends`, `open`), `enum`, `alias`; `array`, `map`; `nullable` | `atom` (open registry) · `prod` · `sum` · `seq` · `arr` · `loc` · `union` · `intersect` · `mu` · `var` · `app` | named payload types, unresolved | whatever Rust / TS express |
| **generics** | parameters of one role (`session`); slots `connection` / `envelope` / any record or enum; `apply` / `with` | kinded (`pure \| wire \| arr \| …`) and trait-constrained (`identifiable \| ref \| fact \| …`) params; `<:` subtyping | — | Rust generics only |
| **operations** | `methods` and `events`, each with a hand-declared `direction` | an `arr` type; a service is a `prod` of arrows; topology *derived* from arrow nesting (E⁰ / E¹ / E²) | route ids and frame types listed | trait methods; wire ops as `type`-tagged union arms |
| **value / wire model** | none separate from the type model | Mark: `Nil \| Compound \| Bytes \| Location \| Arrow`, `isWire` / `isPure` | — | eidos2 `KernelWireValue` + canonical JSON |
| **semantics beside the type** | `sess` layer: `decides`, `asks`, `conversation`, `options`, `platforms` | `theory { identity, scope, ref }` per declaration | — | sema `authority × scope × durability × resultMode`; `ownership × persistence` |
| **identity** | family name + schema version | `SchemeHash` (structural) vs `ContractHash` (semantic), plus `TypeHash`, `TheoryHash`, `LoweredShapeHash` | — | leukos `unit.json` address key; `idCorrelationDomains` prefix registry |
| **generator** | `cmd/nightseam`: kernel → `internal/spi` → `internal/languages/{golang, typescript}` | Rune: nine stages, two-stage tagged AST, plugin × target matrix, applicability predicates, external plugins over stdio, incremental manifest | `generate-runtime-model-ts.mjs` regex-scrapes Rust into TS | **none** |
| **targets** | Go, TypeScript | TypeScript, Rust | TypeScript from Rust | — |
| **transports** | one profile, `nightseam.duplex/1`, WebSocket | Mark `{c,h,p}` over WS / HTTP+SSE / CLI, gated by `CapabilityProfile`; a second generated `{type,id,value}` WS framing | WS frames with cursor + resume; HTTP routes | per-family hand-written WS unions; HTTP headers between planes |
| **cancellation** | a cancel frame in the profile | missing `p` = release; `cancellationBackpressure` as a transport capability bit | — | absent (management); `cancel_run` / `interrupt_run` as ops (energeia); `taskCancelled` lifecycle (telos) |
| **backpressure / credit** | per-channel credit in `tunnel/` | `Location` handles, leak-asserted | cursor dedupe | server-internal `Lagged` → reset frame |
| **presence / resume** | presence in `runtime/` | `persistentHandleNamespace` bit | `resume { cursor, dedupe }` clause | telos `hello` / `welcome` / `resume` + `ResyncReason`; energeia journal cursor + `AttachmentLease` |
| **state replication** | — | — | — | plexis `SyncFrame { prev, seq, ops, reset, epoch }` + `SubscriptionHandle` |
| **parity between languages** | by construction (one contract, two renderers) | goldens per language, shared vectors, `symmetry.*` lint with reasoned waivers, `contract-descriptor` manifest | substring checks against hardcoded id arrays; negative fixtures; `parity-report.json` | grep lint on eight tags; measured drift 116 vs 99 |
| **generator tests** | goldens, `duplextest` conformance | `rune-tester` cases (`add-consumer` → `typecheck`, `check-mode`, `incremental`); `protocol-interop` manifest with leak asserts; six-language hand stubs | fixture matrix | — |
| **layering law** | one directory per component, one subdirectory per language | `units/{layer}/{pack}/{name}/{lang}`; `layering.yaml` + `validator.yaml` + `paths.yaml` | `GRID_CELL_PATTERN.md`: `bind_P` / `stub_P` with `stub(bind(f)) ≈ f` | `LAYERS.md`: same algebra plus the beta law; eidos2 `cell-graph.yaml` default-deny |

## What Nightseam does better

Worth stating first, because the surveys make it concrete:

- **Schema-first.** Nightseam is the only one with a machine-readable
  schema for its declaration. Glyph's spec is a parser; Graphe's and
  Plexis's declarations are unvalidated.
- **One wire profile.** aifunc3 carries two unrelated WS framings, one
  with base64 payloads inside JSON; Plexis duplicates its profile per
  family. Nightseam's single `nightseam.duplex/1` is the discipline the
  others lack.
- **Parity as an identity, not a lint.** Plexis's grep lint next to its
  116-vs-99 method count, and Graphe's regex scraper with hardcoded
  expected-id arrays, are the failure modes a generator exists to remove.
- **A layered declaration.** `dto` / `rpc` / `sess` as separate files
  with a direction rule between them is a real design choice the Glyph
  stack made the other way (one algebra, annotations); both are
  defensible, but Nightseam's is easier for a non-author to read.

## What Nightseam could take

Grouped by the part of Nightseam it would touch. Each item names where
it comes from; the per-repo pages have the paths.

### The contract model (`internal/contract`)

1. **Two-level identity.** A structural hash of a type's shape (stable across renames and description changes) beside a semantic hash that includes the `sess` layer and descriptions — Glyph's `SchemeHash` / `ContractHash`. Routing and discovery key on the first; correctness on the second. *aifunc3 §1a, bitmachine §1c.*
2. **Kinded parameters.** Nightseam's parameters have one role, `session`. Glyph's carry a kind (what shape may fill them) and optionally a trait (what the filler must declare). The `of: session` field is already the seed of this. *aifunc3 §1a.*
3. **A theory layer.** `identity key(field(id))`, `scope field(tenantId)`, `ref field(authorId) target(User)` — facts beside a record that a renderer can turn into keyed maps, scoping checks and typed references. *aifunc3 §1a.*
4. **A reference that is not a dependency.** Metatype's `ref(kind)` with `dependency: false`. Nightseam's `imports` conflates "I name your types" with "I depend on your package". *aifunc3 §1b.*
5. **Session governance as orthogonal enums.** Sema's `authority × scope × durability × resultMode(sync | ack | task)` for commands and `ownership × persistence` for data is a cleaner factoring than `decides` / `asks` lists; `resultMode: task` in particular is the honest answer to "which methods can be cancelled". *bitmachine §3d.*
6. **A wire-value model with `isWire`.** A computable "may this cross the wire" predicate, separate from the type model. Nightseam's `json` primitive is where this would bite. *bitmachine §1b, plexis §2c.*

### The generator (`cmd/nightseam`, `internal/kernel`, `internal/spi`)

7. **Emit a tagged AST, never strings.** Rune's two-stage AST — language-agnostic tagged values from the kernel, language-specific tags from the renderer, formatting last — and the per-stage knows / does-not-know table in `RUNE_PIPELINE.md`. The strictest statement of the kernel/SPI boundary Nightseam already has. *aifunc3 §2a.*
8. **Applicability predicates.** `isData`, `isService`, `hasOperationBoundaries`, … with `any` / `all`, so a renderer says which declarations it handles instead of receiving all of them. *aifunc3 §2a, bitmachine §2a.*
9. **A fingerprinted output manifest and `--check`.** Per-file content hashes and per-unit fingerprints so CI can fail on stale output without regenerating into the tree. BitDev's `docker-templates check` is the same contract. *aifunc3 §2a, bitmachine §2a, §4.*
10. **A language-neutral contract descriptor.** One JSON per family with each type's names and hashes, emitted alongside the packages, for CI gates and cross-language checks. *aifunc3 §2a.*
11. **A third language out of process.** Versioned JSON-over-stdio plugins (Rune `externalPlugins`; the Plexis analysis engine's `providers run`). Nightseam's `internal/spi` is in-process Go; this is how it would host a renderer written in something else. *aifunc3 §2a, plexis §3.*
12. **Four-way artifact identity.** What an artifact is, which output unit it lands in, what name it is imported by, where the file goes — separated, so the linker needs no domain knowledge. *aifunc3 §2a.*
13. **A renderer config schema per renderer.** Plexis's deployment drivers declare a JSON Schema for their own config and the kernel validates and passes typed config through. *plexis §2a.*

### The wire profile (`runtime/`, `duplex/`, `tunnel/`)

14. **Derive topology from the signature.** Glyph's elevation model: an operation whose result contains an arrow is a stream, one whose request contains an arrow takes a callback, and the generator refuses a transport that cannot carry it. Nightseam declares `direction` by hand and has one transport, so this is a design option more than a gap — but `CapabilityProfile` with a `cancellationBackpressure` bit is the axis Nightseam's `tunnel/` implements and its contract does not name. *aifunc3 §3b, bitmachine §3a.*
15. **Resume.** `hello` → `welcome { sessionId, resumeToken }`, `resume { checkpoints, subscriptions, pendingCommandIds }` → `resumeOk` | `resyncRequired { reason }` with an enumerated `ResyncReason` — Telos. Energeia's journal cursor on `subscribe_session` and its rule "replay must not re-execute side effects" are the same concern from the other side. *bitmachine §3c, plexis §4c.*
16. **Cancellation as a lifecycle, not a flag.** Telos's `taskSpawned` / `taskProgress` / `taskCompleted` / `taskCancelled` / `taskFailed`; Energeia's `cancel_run` and `interrupt_run` as typed operations. *bitmachine §3c, plexis §4c.*
17. **`causationId` beside `correlationId`.** Telos's `SequencedStreamEnvelope`. *bitmachine §3c.*
18. **A state-replication frame.** Plexis's `SyncFrame { prev, seq, ops, reset, epoch }` with a client `SubscriptionHandle` that reduces frames and resubscribes on reconnect — a materialised view where Nightseam's events are a log. A candidate fourth frame class. *plexis §4b.*
19. **Presence as a lease.** Energeia's `attachment_lease_updated`. *plexis §4c.*
20. **A correlation carrier SPI.** Leukos's `MessageCorrelationCarrier` (`inject` / `runWithExtracted` / `wrapHandler`) keeps trace context out of the wire schema. *plexis §4d.*
21. **Chunked transfer.** Leukos's `upload-start { totalBytes }` / `upload-chunk { chunk, final }`. *plexis §4d.*
22. **Callbacks and live handles as values.** Mark's `Arrow` and `Location` sorts with `register` / `resolve`. If Nightseam wants callbacks over its channel this is the mechanism; if not, it is the complexity being declined, and worth saying so in the README. *aifunc3 §3a.*
23. **A metadata profile.** Plexis's `x-plexis-*` headers, stripped and re-derived at the edge, are an untyped seam; a contract could name trusted vs untrusted metadata and strip-and-rederive rules. *plexis §4e.*

### Conformance and parity

24. **A written transparency law.** `stub_P(bind_P(f)) ≈ f` and `bind_P(stub_P(a)) ≈ a` — `GRID_CELL_PATTERN.md`, Plexis `LAYERS.md`. Nightseam's Go server and TS client are exactly this pair without the name. The law is testable: the generator could emit a round-trip test per family. *bitmachine §5, plexis §1b.*
25. **A declarative interop manifest.** `interop.manifest.json` scenarios with `validTransports`, a `serverScript`, `runtimeAsserts` including a `liveHandlesAfter: 0` leak check, and expected transcripts; `CONFORMANCE_LAYERS.md` saying which layer owns a failure. `duplex/go/duplextest` is the seed. *aifunc3 §3e.*
26. **Negative fixtures.** Graphe's parity matrix requires that malformed frames are *rejected*, not only that well-formed ones pass. *bitmachine §2b.*
27. **An evidence artifact.** Graphe's `parity-report.json`; BitDev's coded drift findings. A machine-readable report from `nightseam check`. *bitmachine §2b, §4.*
28. **Consumer type-check.** `rune-tester`'s `add-consumer:` → `typecheck:` step proves generated packages are usable, not only that they match a golden; `check-mode` and `incremental` cases beside it. *aifunc3 §2b.*
29. **Hand-written stubs as falsification.** aifunc3's Grid WS protocol has hand-written Python, Go, Haskell and C# stubs to prove the wire format is implementable without the generator. *aifunc3 §3e.*
30. **Live conformance.** BitDev's `websocket-protocol-hello { expectedJsonType }` probe checks a running service against its declaration. *bitmachine §4.*
31. **Waivers with reasons.** `exceptions.yaml` — per-unit, per-check, each with a `reason:` — instead of narrowing a check globally. *aifunc3 §2a.*

### Layout and packaging

32. **A generated layering contract.** eidos2's `cell-graph.yaml` (default-deny, `fromKinds` → `toKinds`, `requirements` with `min`, `sourceImports.forbidden`) could be *emitted* by Nightseam for the packages it generates, asserting `stub → decl → data` and forbidding `stub → impl`. *plexis §2b.*
33. **`decl` / `bind` / `stub` as a layering rule.** aifunc3's `layering.yaml` forbids `proto_bind → impl` with a message; Plexis's `PROTOCOLS_SPEC.md` states the same. Nightseam's package split already respects it; naming it lets a check enforce it. *aifunc3 §4, plexis §5.*
34. **Manifests are projected, not sourced.** Forge's invariant that `package.json`, `go.mod` and friends are generated from the resolved graph. Relevant to how Nightseam's emitted packages get their module files. *aifunc3 §4.*
35. **A structured unit address.** Leukos's `slice / stage / role / qualifiers` key that composes into dependency keys — the nearest cousin of `urn:nightseam:contract:1`. *plexis §1c.*
36. **A dependency map from manifests.** `heraldry.md` derives its architecture diagram from runtime manifests only, each edge annotated with why. Nightseam could render the same from the import graph between families. *bitmachine §5.*

## What not to take

- A DSL without a machine-readable grammar (Glyph). Keep the JSON Schema.
- Two wire framings in one system, or payloads base64-wrapped inside JSON frames (aifunc3 Mark vs generated WS).
- Parity by substring search against hand-maintained expected lists (Graphe, Plexis).
- A generator whose source of truth is regex-scraped source code (Graphe's `generate-runtime-model-ts.mjs`).
- Four unreconciled declaration formats for the same unit (Plexis).
- A prose "schema for the schema" with no validator (Plexis `LAYERS.md`, Graphe `cap/*/README.md`).

## The documents, in one list

Read in this order for the most transfer per page:

1. `aifunc3/RUNE_PIPELINE.md` — the generator's stage table.
2. `bitmachine/kernel/eidos/docs/ELEVATION_MODEL.md` (same as `aifunc3/rune-codegen/docs/ELEVATION_MODEL.md`) — topology from signature; `CapabilityProfile`.
3. `bitmachine/GRID_CELL_PATTERN.md` and `plexis/apps/plexis/docs/spec/LAYERS.md` — the bind / stub algebra and its laws.
4. `bitmachine/kernel/telos/units/surface-protocol/ts/src/index.ts` — a complete duplex profile in one file.
5. `aifunc3/units/test/rune/protocol-interop/` with `CONFORMANCE_LAYERS.md` — conformance done declaratively.
6. `aifunc3/units/data/glyph/types/ts/src/index.ts` (or the bitmachine copy) with `units/data/mark/types/ts/src/index.ts` — the type algebra and value model together.
7. `bitmachine/kernel/sema/units/contracts/ts/src/` — governance as enums, ~200 lines.
8. `plexis/kernel/energeia/units/proto/default/ws/decl/session/ts/src/index.ts` with `kernel/energeia/docs/CONTRACT-V1.md` — the best hand-written family, and what it costs to write one by hand.
9. `aifunc3/rune-codegen/schemas/rune.gen.schema.json` — the generator config schema.
10. `aifunc3/rune-tester/` — generator end-to-end cases.
11. `bitmachine/kernel/graphe/tools/verify-contract-parity.mjs` — the cautionary tale.
12. `bitmachine/declarative-machine-semantics.md` and `irreducible-kernel-and-gateways.md` — the separations (`capability != authority`, `identity != identifier`) a contract language must not conflate.
