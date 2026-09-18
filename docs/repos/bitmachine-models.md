# bitmachine — type models, generation and protocol, compared to Nightseam

A survey of everything in [bitmachine](bitmachine.md) that does what
Nightseam does. Paths are relative to `C:\Development\bitspark\bitmachine`,
on `main` at commit `f64b23c992` (2026-05-17). The yardstick is
Nightseam's own model in
[internal/load/schemas/](../../internal/load/schemas/);
see the [aifunc3 page](aifunc3-models.md) for it spelled out.

Related pages: [aifunc3](aifunc3-models.md) · [plexis](plexis-models.md)
· [cross-repo comparison](../RELATED_MODELS.md).

---

## Headline

Three kernel subsystems carry the whole of Nightseam's thesis, each in a
different way:

- **`kernel/eidos` + `kernel/glyph`** — the Glyph / Rune / Mark stack, ported from aifunc3: a declaration DSL, a JSON-Schema-validated generator config, one generator emitting TypeScript + Rust over one wire model with WS / HTTP / CLI transports. The stack itself is documented on the [aifunc3 page](aifunc3-models.md); this page records only where bitmachine's copy lives and what it adds.
- **`kernel/graphe`** — a smaller, newer, JSON-declared version of the same idea, and what the last commit ("Tighten Graphe runtime contract parity and browser stubs") touched. Read it as a cautionary tale with two good ideas in it.
- **`kernel/telos`** — a hand-written duplex wire profile with resume, presence, subscriptions, commands and task lifecycle: the profile Nightseam's generator should be able to emit.

Around them: `kernel/sema/units/contracts` (session governance as four
enums), `GRID_CELL_PATTERN.md` (the generator stated as an adjoint pair
with a law), and BitDev's declaration-drift checker.

---

## 1. Type models

### 1a. Glyph — the port

| | |
|---|---|
| Language reference | `kernel/eidos/docs/GLYPH_LANGUAGE.md` |
| Type algebra | `kernel/glyph/units/data/glyph/types/ts/src/index.ts` |
| Parser, AST, theory, kernel | `kernel/glyph/units/lib/glyph/{parser, ast, theory}`, `kernel/glyph/units/impl/glyph/kernel` |
| Worked example | `kernel/eidos/examples/full-stack/service.glyph` |

The same eleven-kind algebra as aifunc3 (`atom | prod | sum | seq | arr |
loc | union | intersect | mu | var | app`, with the source-level
`NamedType` / `SchemeRef { name, args }` / `SchemeParam { name, constraint?,
kind? }` mirror). The structural difference from Nightseam that the port
makes plain: **operations are not a separate layer.** An operation is an
arrow type; a service is a record whose every field is an arrow (`role
service`). Imports, slots and generics all fall out of the one algebra
where Nightseam has three layers (`dto`, `rpc`, `sess`) and a separate
`methods` / `events` vocabulary.

### 1b. Mark — the value model

`kernel/glyph/units/data/mark/types/ts/src/index.ts` and `rust/src/lib.rs`.
Five sorts, three opaque, mapped one-to-one onto Glyph's opacity classes:

```ts
export type Value = Nil | Compound | Bytes | Location | Arrow;
// atom → Bytes (opaque content), loc → Location (opaque location), arr → Arrow (opaque capability)
// isWire(v): no Arrow anywhere → wire-safe
// isPure(v): no Arrow and no Location → fully self-contained
```

`isWire` / `isPure` are the load-bearing predicates — how the system
decides mechanically what may cross a wire. Nightseam has a type model but
no separate value model, and so no computable "is this transportable".

### 1c. Content-addressed contract identity

`kernel/glyph/units/data/contract/identity/ts/src/index.ts` — domain-
separated, prefixed SHA-256 branded strings: `t-` TypeHash, `s-`
SchemeHash, `r-` EntryHash, `th-` TheoryHash, `k-` ContractHash, `ls-`
LoweredShapeHash. The comment on `SchemeHash` states the intent:

```
H(arity + params + bodyHash) — does NOT include the scheme name or namespace path.
Stable across: renames, re-registrations with the same structure, and theory changes.
Changes when: arity, param constraints/bounds/sorts, or structural body changes.
This is the route/discovery key in Fiber capability maps and Spire manifests.
Use ContractHash when correctness depends on full semantics (type + theory locked).
```

> **Against Nightseam.** Two-level identity — structural for routing,
> semantic for correctness — where Nightseam has a single
> `urn:nightseam:contract:1` version string. Nightseam lacks this
> entirely.

### 1d. Graphe protocol declarations — JSON, no schema

| | |
|---|---|
| WS frames | `kernel/graphe/units/proto_decl/runtime/ws/default/data/frames.json` |
| HTTP routes | `kernel/graphe/units/proto_decl/runtime/http/default/data/routes.json` |
| Capabilities | `kernel/graphe/units/cap/runtime/control/data/README.md` (prose) |

Closest in *form* to Nightseam's JSON layers. `frames.json` declares a
protocol id, an endpoint, a frame list with required-field sets and a
named payload type, and a resume clause:

```json
{ "type": "receipt.appended", "required": ["type","cursor","receipts"], "payload": "ReceiptAppendedFrame" },
...
"resume": {
  "cursor": "opaque monotonically increasing sequence emitted by the bind",
  "dedupe": "clients discard frames whose cursor was already observed"
}
```

`cap/*/data/README.md` is the transport-neutral identity anchor ("narrow
waist … without choosing HTTP, WebSocket, MCP, CLI, Docker, or browser
transport") but is prose, not data.

> **Against Nightseam.** Same idea, done worse: only required field
> *names*, not types; no schema validating the declaration; capabilities
> in prose. Nightseam's schema-validated layers are stronger.

### 1e. Other type-model-adjacent subsystems

| path | what | note |
|---|---|---|
| `kernel/logos/spec/log/schema/*.schema.json` (9) + `spec/log/{fragments, engines, logical-profiles, physical-models, plan-kinds, selectors, conformance}/*.toml` | Spec-as-data: each logic fragment, engine, plan kind and conformance suite is a TOML document validated by its own JSON Schema | The engine's capability matrix is itself declarative data. Nightseam could declare wire profiles this way. |
| `kernel/syntaxis/units`, `syntaxis.yaml`, `kernel/syntaxis/minimal-rust.stx` | Universal AST packing: `syntaxis.yaml` maps extensions → node kinds (`ast:rs:source_file`); `.stx` is a binary interned-tree container | Source trees, not APIs. |
| `schemas/bitmachine/package/v4.json` | The only file under `schemas/`: JSON Schema 2020-12 for package manifests | See §4. |
| `kernel/sema/units/model-types/ts/src/index.ts` | Reactive UI model contracts: `ModelMeta {kind, version}`, `StateConstraint<S>`, middleware | Client state, not wire. |
| `kernel/ontos/data/{contract/identity, mark/types, platform/error}` | Ancestor copies of glyph's identity and Mark types | The *archē* layer per `heraldry.md`. |

---

## 2. Code generation

### 2a. Rune — the port

Read in order: `kernel/eidos/README.md`, `docs/OVERVIEW.md`,
`docs/ELEVATION_MODEL.md`, `docs/CONFIG.md`, `docs/PLUGINS.md`,
`schemas/rune.gen.schema.json`. Pipeline: `.glyph` + `rune.yaml` →
`rune-codegen` → TS + Rust.

Units under `kernel/eidos/units/`:

| unit | role |
|---|---|
| `lib/rune/core`, `lib/rune/gen-decl` | parse; type-declaration emit |
| `lib/rune/gen-batch` | workspace planning — `plan.ts`, `resolve.ts`, `expand.ts`, `compatibility.ts`, `lowering.ts` |
| `lib/rune/gen-proto-{mark, ws, http, cli}` | the four protocol plugins |
| `lib/rune/gen-rust`, `lib/rune/gen-ts`, `lib/rune/surface-rust`, `lib/rune/surface-ts` | the per-language renderers — the kernel/SPI split, matching Nightseam's `internal/spi` + `internal/languages` |
| `api/rune/gen-plugin`, `api/rune/gen-registry`, `lib/rune/gen-plugin` | the external-plugin SPI |
| `lib/rune/gen-contract-descriptor` | emits `ContractDescriptorManifest` of `{fqName, schemeHash, contractHash, boundary, modulePath}` |
| `api/transport/{ts, rust}`, `impl/transport/ts/{ws, in-process}` | transport SPI |
| `app/rune/codegen` | the CLI |

`kernel/eidos/schemas/rune.gen.schema.json` (384 lines) is the
generator-config schema described on the aifunc3 page: `generators` each
naming a `plugin` (`decl | proto:mark | proto:ws | proto:http | proto:cli`),
`target`, `preset`, `projection`, and an `applicability` predicate with
`any` / `all` over `isData | isService | isResource | isSerializable |
isFirstOrder | hasOperationBoundaries | hasIdentity | isCausal |
hasProtocolSafeOperations`; `matrix`; `layout.routes` (glob → output path
templated on `{space.path}` / `{role}` / `{lang}`); `importOverrides`
keyed `space:role:target`; `suppress`. Example config at
`examples/full-stack/rune.gen.yaml`.

Staleness is a lockfile-like manifest, `examples/*/.rune-manifest.json`:

```json
"typescript:examples/minimal:mark": {
  "unitId": "typescript:examples/minimal:mark", "role": "mark", "target": "typescript",
  "plugin": "mark",
  "fingerprint": "e801349932083b91ddc63cad14e151a0de9b6831cb5eb97027248df4e7076502",
  "files": [{ "path": "examples\\minimal\\mark\\index.ts", "contentHash": "8782a25181…" }]
}
```

driving `rune-codegen generate --check` in CI. Golden corpus at
`units/data/rune/testdata/codegen/golden/{rust, typescript}/` — about 17
fixtures, each emitted twice (`X.rs` and `X.bind-stub.rs`), covering
`generics`, `recursive`, `arrows`, `callable-error`, `target-overrides`,
`boundary-parity`, `endpoint`.

### 2b. Graphe — "runtime contract parity" and "browser stubs"

What commit `f64b23c992` means. Four tools in `kernel/graphe/tools/`, wired
into the package's `test` and `build`:

```json
"verify:contract-parity": "node tools/generate-runtime-model-ts.mjs --check && node tools/verify-contract-parity.mjs",
"build": "tsc -p tsconfig.json --noEmit && node tools/generate-runtime-model-ts.mjs --check && node tools/verify-contract-parity.mjs && node tools/build-workbench.mjs --check && node tools/verify-workbench.mjs"
```

**The generator, `generate-runtime-model-ts.mjs`** (199 lines). Its source
of truth is Rust source, regex-scraped — not a schema:

```js
const rustPath = join(root, "units/data/runtime/model/rs/src/lib.rs");
const tsPath   = join(root, "units/data/runtime/model/ts/src/generated.ts");
// parseAliases: /^pub type ([A-Za-z0-9_]+) = ([^;]+);/gm
// parseStructs: /pub struct ([A-Za-z0-9_]+)\s*\{([\s\S]*?)\n\}/g
// mapType: String→string, u64|usize|i64→number, Option<T>→`T | null`,
//          Vec<T>→T[], BTreeMap<K,V>→Record<string,V>, serde_json::Value→JsonValue
```

with hand-coded special cases (`if (alias.name === "LedgerVector")`, a
hardcoded union for `VectorEntry`). `--check` fails if the committed
`generated.ts` differs from a fresh render.

**"Runtime contract parity" = `verify-contract-parity.mjs`** (360 lines):
not schema validation but ~80 assertions written as code, emitting
`.run/evidence/cross-language-runtime-contract-parity/parity-report.json`.
Five kinds of check:

1. The generated file is current and every Rust type has a TS counterpart.
2. An anti-`unknown` lint — named interfaces must not contain broad JSON:
   ```js
   const strictContractInterfaces = ["RuntimeTopologyResponse","RuntimeProjectionsResponse",
     "RuntimeAgentsResponse","InteractionStateResponse","RuntimeEvidenceResponse",
     "ResolvedAffordance","ActionReceipt","AgentCard","McpToolMetadata","ObservatorySurface"];
   .filter(([, body]) => /JsonValue|unknown|\[key: string\]/.test(body))
   ```
3. Declaration completeness — `routes.json` route ids must equal a hardcoded 31-element `expectedRouteIds`; `frames.json` types a hardcoded 11-element list.
4. Cross-language substring presence — for each frame type, `wsBind.includes('"type": "${frameType}"')` (Rust bind emits it) and `wsStub.includes(frameType)` (TS stub recognises it); for each of 23 HTTP method names, `httpStub.includes(marker)`.
5. A fixture matrix — `units/test/runtime/contract-parity/rs/fixtures/{valid/rust-generated, valid/ts-generated, negative}/`, validated by hand-written `validateFact` / `validateEventEnvelope` / `validateProductSurface` / `validateLiveFrame`. Positives must pass; **negatives must fail** (`missing-fact-scope.json`, `ws-frame-missing-cursor.json`, `product-surface-missing-receipts.json`, `obsolete-feature-action-op-only.json`).

**"Browser stubs"** = `units/proto_stub/runtime/ws/default/ts`,
`.../http/default/ts`, and the `units/app/workbench/browser/ts` page
consuming them. `verify-workbench.mjs` greps `src/` and built `dist/` for
markers (`RuntimeLiveClient`, `receipt.appended`, `/api/runtime/live`) to
prove the shipped bundle still speaks the declared protocol. The WS stub
does cursor dedupe and reconnect and re-declares the required-field table
by hand — the parity checker exists to notice when that copy drifts:

```ts
const liveRequiredFields: Record<string, string[]> = {
  connected: ["protocol"],
  "runtime.snapshot": ["nodeId", "tick", "counts"],
  "projection.updated": ["records", "outputs"],
  ...
};
```

> **Against Nightseam.** Same goal — one contract, two language sides,
> checked — by a much worse mechanism: regex scraping and hand-maintained
> expected-id arrays, where Nightseam derives both sides from the
> declaration. Two things to take anyway: the **negative-fixture matrix**
> (a bad frame must be *rejected*, which is as important as a good one
> being accepted) and the machine-readable `parity-report.json` evidence
> artifact.

### 2c. Other generators

- `kernel/eidos/units/lib/mark/gen-rust`, `.../gen-ts` — Mark codec emitters.
- `apps/mimos/runtime/units/data/mimos/manifest/gen`, `apps/mimos/runtime/units/proto/mimos/cli/decl/gen` — app-level consumers of the same pattern.
- `bitdev/src/docker-templates.ts` — Dockerfiles rendered from manifests (§4).

---

## 3. Protocol and wire

### 3a. The elevation model

`kernel/eidos/docs/ELEVATION_MODEL.md` — the same document as aifunc3's.
Elevation is arrow nesting depth, inferred, never annotated:

| signature | topology | elevation |
|---|---|---|
| `A -> B` | `unary` | E⁰ |
| `A -> (… -> …)` | `outputLive` | E¹ |
| `(… -> …) -> B` | `bidirectional` | E² |

```glyph
echo:        EchoRequest -> EchoResponse                  -- E^0
subscribe:   StreamFilter -> (:void -> StreamEvent)       -- E^1 server-returned pull-stream, Nil = EOS
observe:     (ObserveEvent -> ObserveAck) -> Subscription -- E^2 client passes a callback
```

Transports declare a `CapabilityProfile { clientSendsAfterInitial,
serverSendsAfterInitial, serverReturnsLiveHandles, clientSuppliesLiveHandles,
persistentHandleNamespace, cancellationBackpressure }` — `UNARY` (CLI, E⁰),
`PUSH` (HTTP/SSE, E⁰+E¹), `BIDI` (WebSocket, all three) — and the generator
refuses impossible combinations. The refusal is visible in generated code,
`examples/full-stack/my/namespace/ws/index.ts`:

```rust
DocServiceClientMessage::Collaborate { id, .. } => {
    send_message(DocServiceServerMessage::Error { id,
        code: "BIDIRECTIONAL_NOT_DISPATCH".to_string(),
        message: "Bidirectional operation \"collaborate\" must be handled via session lifecycle, not dispatch" });
```

> **Against Nightseam.** The single biggest idea to take. Nightseam
> hand-declares request / response / event / cancellation as frame
> categories; Glyph derives which session protocol an operation needs
> from its type and checks the transport can supply it.
> `cancellationBackpressure` as a declared transport bit is exactly the
> axis Nightseam names in its runtime but does not model in its contract.

### 3b. The Mark session protocol

Symmetric handle-passing over any bidirectional channel — `{c, h, p}`
request frames, `{c, r}` responses, `register` swapping an arrow for a
Location token, `resolve` building the proxy back. `SessionRegistry` for
E¹, `BiSession` for E². Runtime at `kernel/eidos/units/lib/mark/session`
and `.../lib/mark/tunnel`. Detailed on the aifunc3 page.

### 3c. Telos — the session and governance wire profile

`kernel/telos/units/surface-protocol/ts/src/index.ts` (and `rs/`), with
sibling units `surface-protocol-ws`, `surface-bind-ws`, `surface-stub-ws`,
`client`, `server`, `space-auth-ws`, `sema-bridge`.

`TELOS_SYNC_PROTOCOL = 'telos.sync.v2'`: a flat `ProtocolMessage` union
over `ProtocolEnvelope { protocol, kind, timestamp }`, with
`SequencedStreamEnvelope` adding `{ workspaceId, streamId, sequence,
correlationId?, causationId? }`. In one file, everything Nightseam's
`runtime/` calls a wire profile:

| concern | messages |
|---|---|
| handshake, presence | `hello` → `welcome { sessionId, resumeToken, heartbeatIntervalMs }`; `ping` / `pong` |
| resume | `resume { sessionId, resumeToken, checkpoints[], subscriptions[], pendingCommandIds[] }` → `resumeOk` or `resyncRequired { reason }`, `ResyncReason = 'unknown_session' \| 'expired_journal' \| 'version_mismatch' \| 'workspace_reset' \| 'sequence_gap'` |
| subscription | `subscribe` / `unsubscribe` / `subscribeRejected` / `caughtUp` |
| data | `snapshot` / `tx` / `ack` |
| commands | `command` / `commandAccepted` / `commandRejected` |
| long-running work | `taskSpawned` / `taskProgress` / `taskCompleted` / `taskCancelled` / `taskFailed` |

Per `kernel/nomos/docs/SEMANTICS.md`, telos is deliberately pure
transport — a policy enforcement point that "imports NEITHER archon NOR
nomos at source level", taking auth as injected `ConnectionGate` and
`Authorize` callbacks; `nomos` is the decision point owning `check(subject,
action, resource, context)`.

> **Against Nightseam.** Same idea, hand-written rather than generated —
> and that is the gap in both directions. Resume with checkpoints, an
> enumerated `ResyncReason`, `causationId` beside `correlationId`, and
> cancellation as a *task lifecycle* rather than a request flag are all
> richer than Nightseam's profile. Nightseam has the generator Telos
> lacks; Telos has the profile Nightseam should aim at.

### 3d. Sema contracts — governance as four orthogonal enums

`kernel/sema/units/contracts/ts/src/` and `rs/src/`, a hand-maintained
pair of six modules each, mirrored file for file. This is Nightseam's
`sess` layer as a small vocabulary:

```ts
// commands.ts
export const authority   = { local: 'local', remote: 'remote', hybrid: 'hybrid' } as const;
export const scope       = { app: 'app', session: 'session', workspace: 'workspace' } as const;
export const durability  = { ephemeral: 'ephemeral', durable: 'durable' } as const;
export const resultMode  = { sync: 'sync', ack: 'ack', task: 'task' } as const;

export interface CommandContract<TId extends string, TArgs, TResult> {
  readonly kind: 'command';
  readonly id: TId;
  readonly authority: CommandAuthority;
  readonly scope: ContractScope;
  readonly durability: CommandDurability;
  readonly resultMode: CommandResultMode;
  readonly __args?: TArgs;       // phantom
  readonly __result?: TResult;   // phantom
}
```

`models.ts` is the data-side twin — `ownership: local | replicated |
server`, `persistence: ephemeral | durable | projected`, sharing `scope`.
`correlation.ts` brands `RequestId` / `CorrelationId` / `TaskRunId` /
`ActivityRunId`. `session.ts` is `SessionStatus { state, sessionId, error }`
over `ConnectionState = disconnected | connecting | connected | error`.

Rust/TS parity is round-trip serde tests asserting exact JSON including
null-vs-absent:

```rust
assert_eq!(v, json!({"state": "disconnected", "sessionId": null, "error": null}));
```

and the Rust side documents the one deliberate asymmetry: "The TS
phantom-type generics (`TArgs`, `TResult`) exist only at compile time and
are not represented here."

> **Against Nightseam.** The same concern as Nightseam's `sess` layer
> (`decides`, `asks`), factored as `authority × scope × durability ×
> resultMode` for commands and `ownership × persistence` for data.
> `resultMode: sync | ack | task` is a cleaner answer to the
> cancellation / long-running question than a per-operation flag.
> Weaker than Nightseam in one way: contracts are defined in code
> (`defineCommandContract`), not in a validated document.

### 3e. Named documents and a hand-built triad

- `docs/PLATFORM_BOUNDARIES.md` — the app / kernel responsibility matrix: "Apps own product composition. Kernel and platform packages own reusable semantics, runtime models, protocols, policy, diagnostics, and UI primitives." Normative repo-wide.
- `docs/apis/` — only `cloudflare.md`, `docker.md`, `hcloud.md` (vendor notes). `docs/integration/` is language-layer analysis (`syntaxis → deixis → noesis`) plus `glyph-layer-migration-plan.md`.
- `kernel/logos/proto/log/ws/{decl, bind, stub}/store/ts/` — the decl / bind / stub triad applied by hand to a Datalog store, with per-unit `metatype.yaml` (`kind: proto_decl`, `protocol: websocket`, `api: store`). Its `index.ts` states the wire-safety problem in comments: `WireResourceLimits` is "`ResourceLimits` without AbortSignal (not serializable over WS)"; `WireQuerySubscriptionOpts` has "kinds as array (Set is not serializable)". That is `isWire` enforced by hand.

---

## 4. BitDev — declarations, native truth, drift

| | |
|---|---|
| Source | `bitdev/src/` (~16 modules) |
| Capabilities doc | `docs/bitdev/REPO_CAPABILITIES.md` |
| Package schema | `schemas/bitmachine/package/v4.json` (127 lines) |
| Manifests | 1,339 `.bitmachine.yaml` / `*.bitmachine.yaml` files outside the excluded trees |

Three manifest species, discriminated by `apiVersion`:

- `bitmachine/namespace/v1` — the root `.bitmachine.yaml`: `namespaces: [apps, boot, components, kernel, neo-athens]`, `projects: [arkhe]`
- `bitmachine/v4` — a project: `name`, `description`, `purpose`, `tags`, `layer`, `languages`, and an explicit `packages:` list (`kernel/graphe/.bitmachine.yaml` enumerates all 34 unit paths)
- `bitmachine/package/v4` — a package, schema'd by `v4.json`: `apiVersion` / `name` / `language` required, plus `sources`, `dependencies`, `devDependencies`, `dependencyScopes`, `docker.templates`, `targets`

**Declaration drift** = `bitdev/src/manifest-check.ts` (~1,000 lines). The
manifest is the declaration; `package.json` / `Cargo.toml` is the native
truth; the checker diffs them into coded findings:

```
package-name-mismatch · package-version-mismatch · native-package-file-missing
dependency-missing-in-manifest · dependency-missing-in-native
dependency-version-mismatch · dependency-kind-mismatch
manifest-parse-error · manifest-unsupported-language · native-package-parse-error
id-correlation-{domain-duplicate, domain-id-invalid/missing, domain-kind-invalid/missing,
  field-invalid, fields-missing, prefix-duplicate/invalid/missing/not-allowed,
  runtime-surface-unknown, runtime-surfaces-missing}
```

Two things to note. Drift is repairable in four directions — `bitdev
manifest sync <native | bitmachine | intersection | union>` — so the tool
does not presuppose which side is right. And the `id-correlation-*` family
is a repo-wide registry of ID namespaces under
`bitdev.idCorrelationDomains`, each with `kind: "prefixed-id" | "trace-id"
| "span-id" | "opaque-external"`, a `prefix`, `fields` and
`runtimeSurfaces`; uniqueness of domain ids and prefixes is checked
globally across all 1,339 manifests.

`REPO_CAPABILITIES.md` is explicit that `language` is metadata, not
routing: adapters are chosen by native marker files (`package.json` →
pnpm, `Cargo.toml` → Cargo, both → both); public verbs `check | build |
test` map to internal capabilities `static-check | build | test`; "Do not
add new branches based on the Bitmachine `language` field."

**Templates from manifests** = `bitdev/src/docker-templates.ts`. A manifest
declares `docker.templates: [{ template, output }]`; the tool computes the
pnpm dependency closure (`packagePaths`, `copyRoots`), renders the
Dockerfile with a generated header, and supports `check` (fail if stale)
and `render` — the same contract as Rune's `--check`.

**Live conformance** = `bitdev/src/registry.ts` + `types.ts`: `SystemRecipe`s
carry `RuntimeSurfaceRouteExpectation { id, method, path, transport }` and
`RuntimeEdgeProbeRecipe` (`dns | tcp-connect | websocket-upgrade |
websocket-protocol-hello { expectedJsonType }`), so BitDev can assert a
*running* service still serves the routes its `proto_decl` declares.

> **Against Nightseam.** A different problem — build manifests, not API
> contracts — but the pattern transfers whole: a declaration, a native
> truth, coded drift findings, four-way sync. `websocket-protocol-hello`
> with `expectedJsonType` is live conformance checking of a declared
> protocol; Nightseam has nothing like it beyond `duplextest`.

---

## 5. The design documents

**`GRID_CELL_PATTERN.md`** (root, 31 KB) — the most important document
here for Nightseam. Every artifact lives at
`<project>/units/<kind>/<concept>/<name>/<lang>/`, kind ∈ `cap · data ·
api · lib · impl · proto_decl · proto_bind · proto_stub · app · test`
(locked). The point is dependency discipline made visible as paths. It
rests on a stated type model — `Cap<API>`, `Data<LANG>`, `Api<LANG, API>`,
`Func<LANG, API>`, `Address<PROT, API>`, `Lib<LANG>` — and two operations
with a law:

```
bind_P : Func<LANG, API> -> Address<PROT, API>
stub_P : Address<PROT, API> -> Func<LANG, API>
stub_P(bind_P(f)) ~= f
```

The architecture rule falls out of the type: "When `bind_P` takes a
`Func<LANG, API>` as a type parameter, it structurally cannot know which
concrete implementation it receives. The rule 'proto cannot import impl'
is not policy; it falls out of the type signature." `cap/` depends on
nothing and is "the single source of truth from which language-specific
`data/` and `api/` units are derived"; the `data` language leaf is
reserved for declarative content.

> Nightseam's Go server / TS client split is exactly `bind_P` / `stub_P`
> without the name or the law. The law is testable.

**`SYMMETRY.md`** (root, 27 KB) — "Every subsystem is implemented in both
TypeScript and Rust as parallel full-stack runtimes. Neither language is
'primary' — a change in one language is incomplete until the equivalent
exists in the other." Mechanical mirroring: `src/index.ts` ↔ `src/lib.rs`,
`kebab-case.ts` ↔ `snake_case.rs`, `@grove/path` ↔ `grove-path`,
module-for-module tables, a `{TS, Rust} client × {TS, Rust} server` interop
matrix under `units/test/{domain}/interop/`; permitted asymmetries named.
Nightseam reaches the same parity by generation, which is the better
mechanism.

**`CONVENTIONS.md`** (root, 17 KB) — the locked directory contract, scope
"directory layout only": strata `kernel/` (durable nouns and contracts) /
`components/` (reusable, non-deployable) / `apps/` (deployable composition
roots), the five-segment grid, the `default` concept placeholder, the
mandatory language leaf. It declares a *seven*-kind set (`proto/` unsplit)
where `GRID_CELL_PATTERN.md` declares ten — the two are out of sync, and
the tree uses both spellings (`eidos/units/proto/rune/ws` vs
`graphe/units/proto_decl`).

**`declarative-machine-semantics.md`** (root, 5.5 KB) — names the whole
program: `ontology + conceptual model + logic model + operational semantics
= declarative machine semantics`. The loop `desired facts → accepted state
→ canonical meaning → candidate intents → authorized accepted intent →
host attempt → evidence → admitted observations → new meaning`, compressed
to "Meaning proposes; authority selects; substrate attempts; evidence
returns; meaning judges." Its separations list is a checklist of
conflations a contract language can make:

```
fact != command            derived candidate != intent      intent != execution
execution != truth         host evidence != admitted observation
observation != satisfaction    value bytes != capability    capability != authority
```

`capability != authority` and `intent != execution` are the distinctions
Nightseam's `sess` layer (`decides`, `asks`) is trying to encode.

**`irreducible-kernel-and-gateways.md`** (root, 13.8 KB) — refuses to
start from world-things (data, process, service, gateway) and posits `Term
· Claim · Assertion · Transaction · Context · RuleProfile · Evaluator ·
AuthorizedTransition`, "an admitted-assertion fixpoint machine". Its
section on identity is for contract-language authors: `identity !=
identifier`, `location != address`, `representation != represented thing`,
`content hash != object identity`, `addressability != existence`; kernel
primitives get only atom equality, with `same_entity(a, b, scope)` /
`equivalent_under(profile, a, b)` modelled above the floor. The
justification for the `SchemeHash` / `ContractHash` split.

**`heraldry.md`** (root, 8.7 KB) — the dependency map as a feudal
metaphor: *archai* (`graphe, kosmos, ontos, sema, zygon`, no intra-kernel
deps) ← *factions* (`kernel/*`) ← *vassals* (`apps/*`). Edges are derived
from runtime manifests only (`dependencies` in `package.json`,
`[dependencies]` / `[workspace.dependencies]` in `Cargo.toml`), each
annotated with why — "Eidos — Mark codec, tunnel protocol, and type
system", "Telos — WS transport for space-auth and surface-bind flows". A
generated-from-manifests architecture doc is something Nightseam could
produce from the import graph between families.

**`RUNTIME-DEPENDENCIES.md`** (root, 5.9 KB) — three kinds of dependency:
*runtime* ("this process calls that endpoint"), *build-time* ("this package
imports this crate to compile"), *semantic* ("this code understands this
domain contract, regardless of which runtime implementation provides
it"). Rule: a runtime dependency does not imply a build-time dependency on
the other system's implementation; compile against the smallest stable
contract and inject the concrete endpoint via app config. This is the
value proposition of a contract language stated independently of one —
good framing for Nightseam's README.

**`kernel/eidos/units/test/rune/protocol-interop/CONFORMANCE_LAYERS.md`** —
three test layers with a failure-attribution rule: L1 service projection
(codec / bind-stub round trip, wire-type fidelity) → "fix once in
codegen"; L2 symmetry runtime (Session / PushRuntime / BiSession over
in-process channels) → "fix once in the runtime crate"; L3 protocol shim
(real HTTP / WS / CLI end to end) → "fix in the specific shim". Written to
prevent "fix SchemeRef, codec, bind/stub, and EOS bugs three times".

---

## 6. Ranked — what to read first when designing Nightseam

1. **`kernel/eidos/docs/ELEVATION_MODEL.md`** — E⁰ / E¹ / E² from arrow nesting plus `CapabilityProfile` per transport, including a `cancellationBackpressure` bit, checked at generation time. Stop declaring "this is an event"; derive which session protocol an operation needs.
2. **`GRID_CELL_PATTERN.md`** — `bind_P` / `stub_P` as an adjoint pair with the law `stub_P(bind_P(f)) ~= f`, and the `cap → data/api → impl → proto_{decl, bind, stub}` ladder. A formal statement of what Nightseam's generator is, and a law to test.
3. **`kernel/eidos/schemas/rune.gen.schema.json` + `examples/full-stack/rune.gen.yaml`** — the generator-config schema: `applicability`, `matrix`, `layout.routes`, `importOverrides`, `externalPlugins`, `suppress`. 384 lines, top to bottom.
4. **`kernel/glyph/units/data/glyph/types/ts/src/index.ts` + `.../data/mark/types/ts/src/index.ts`** — the type algebra paired with the five-sort value model and its `isWire` / `isPure` predicates. Read together; the pairing is the point.
5. **`kernel/telos/units/surface-protocol/ts/src/index.ts`** — a complete duplex profile in one file: hello / welcome / resume with checkpoints, `ResyncReason`, subscribe / caughtUp, command / accepted / rejected, task spawned / progress / completed / cancelled / failed with `correlationId` + `causationId`. The concrete target Nightseam should be able to emit.
6. **`kernel/graphe/tools/verify-contract-parity.mjs`** (+ `generate-runtime-model-ts.mjs`, `frames.json`) — the cautionary tale with two good ideas: the negative-fixture matrix and the `parity-report.json` evidence artifact.
7. **`kernel/sema/units/contracts/ts/src/{commands, models, session, correlation}.ts`** (+ `rs/`) — session governance as `authority × scope × durability × resultMode(sync | ack | task)` and `ownership × persistence`. ~200 lines; a cleaner factoring than a per-operation flag.
8. **`bitdev/src/manifest-check.ts` + `docs/bitdev/REPO_CAPABILITIES.md`** — declaration drift as a coded finding set with four-way `sync` repair, and the `websocket-protocol-hello { expectedJsonType }` probe checking a live service against its declaration. The model for Nightseam CI beyond `--check`.

Honourable mentions: `CONFORMANCE_LAYERS.md` (pair with 1),
`kernel/glyph/units/data/contract/identity/ts/src/index.ts` (pair with
4), `kernel/logos/spec/log/schema/*.schema.json` (a capability matrix as
schema-validated data).
