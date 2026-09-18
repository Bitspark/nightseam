# aifunc3 — type models, generation and protocol, compared to Nightseam

A survey of everything in [aifunc3](aifunc3.md) that does what Nightseam
does: declare types and operations, generate language packages from the
declaration, and carry them over a duplex wire. Paths are relative to
`C:\Users\julia\Development\aifunc3`. Line numbers are as of commit
`e6edab7f9` (2026-04-13).

The yardstick is Nightseam's own model in
[internal/contract/api.schema.json](../../internal/contract/api.schema.json):
primitives `string | boolean | number | integer | timestamp | json`;
`record` (with `extends`, `open`), `enum`, `alias`; `array`, `map`;
imports of named families; parameters of role `session` with `connection`
and `envelope` slots; `apply`/`with` generics; methods and events with a
direction; a `sess` layer (`decides`, `asks`, `conversation`, `options`,
`platforms`); declared `errors`. Two languages, one wire profile,
`nightseam.duplex/1`.

Related pages: [bitmachine](bitmachine-models.md) · [plexis](plexis-models.md)
· [cross-repo comparison](../RELATED_MODELS.md).

---

## 1. Type models

### 1a. Glyph — the declaration language

The closest analogue to Nightseam's contract layers, and the root of
aifunc3's whole type stack.

| | |
|---|---|
| Parser (TS) | `units/lib/glyph/parser/ts/src/` — `lexer.ts`, `parser.ts`, `file.ts`, `role.ts`, `theory.ts`, `raw-statement.ts` |
| Parser (Rust mirror) | `units/lib/glyph/parser/rust/` |
| Type algebra | `units/data/glyph/types/ts/src/index.ts` (927 lines; the whole model in one file) |
| Atom registry | `units/data/glyph/types/atoms.yaml` → generated `atoms-gen.ts` |
| Language reference | `rune-codegen/docs/GLYPH_LANGUAGE.md`; the codegen subset at `RUNE_CODEGEN.md:1079-1170` |
| Golden corpus | `units/data/glyph/testdata/file/*.glyph` + `*.golden.json` (31 pairs); `units/data/glyph/testdata/core/*.vectors.json` |

Glyph is a surface DSL (`.glyph` files), not JSON. A file is a flat list of
`type Name<params> <: Supers = <type-expr>` declarations, each optionally
followed by a `role { }` block and a `theory { }` block. It is explicitly
*not* an RPC IDL — `GLYPH_GROVE_DELVE_RUNE_STACK.md:52-64`: "Glyph should
be understood as a declaration language for programming entities, not as
an RPC IDL." A service is just a product whose fields are arrows.

**The algebra** (`index.ts:32-75`). Two parallel forms: `Type` (resolved,
de Bruijn `var(index)`, `app(hash, args)`) and `NamedType` (source-level,
`param(name)`, `schemeRef{name, args}`):

```ts
export type NamedType =
  | { kind: 'atom';      name: string }
  | { kind: 'prod';      fields: Record<string, NamedType> }
  | { kind: 'sum';       variants: Record<string, NamedType> }
  | { kind: 'seq';       element: NamedType }
  | { kind: 'arr';       input: NamedType; output: NamedType }
  | { kind: 'loc';       target: NamedType }
  | { kind: 'union';     members: NamedType[] }
  | { kind: 'intersect'; members: NamedType[] }
  | { kind: 'mu';        body: NamedType; display: string }
  | { kind: 'param';     name: string }
  | { kind: 'schemeRef'; ref: SchemeRef };
```

Mapping onto Nightseam's vocabulary:

| Nightseam | Glyph |
|---|---|
| primitives (fixed six) | `atom` — an open, registry-driven set (~200 entries in `atoms.yaml`, in families `ast::decl::*`, `ast::glyph::*`, `ast::lit::*`, `ast::rust::*`, `ast::ts::*`, each with `display`/`description`/`sort`); language escape hatches such as `port: rust::u16` (`units/data/glyph/testdata/file/callable-descriptor.glyph`) |
| `record` | `prod` |
| `enum` | `sum` (a tagged union, so variants carry payloads) |
| `array` | `seq` |
| `nullable` | `union[T, atom void]` — `optionType()` at `index.ts:80` |
| method / event | `arr` (an arrow is a value of the type algebra, see §3) |
| parameter + slot | `param` + `schemeRef` application |
| `extends` | `<:` supers |
| — | `loc` (a handle to a value), `intersect`, `mu` (recursion) |

**Generics are kinded and trait-constrained.** A parameter carries a *kind*
(`Kind` at `index.ts:118-127` — `pure | wire | arr | seq | loc | prod | sum |
union | intersect`, mirroring the algebra) and may carry a *semantic trait*
(`SemanticTrait` at `index.ts:164-169` — `identifiable | ref{target} |
fact{factKind} | authorityScoped | causal`), checked at expansion against
the argument's theory. Syntax `type Name<a: pure, b: wire> = ...`.
Subtyping from `units/data/glyph/testdata/file/supers.glyph`:

```glyph
type Email <: Textual = :string
type Multi <: Bar + Baz = :number
type Wrapped <: Container<:string> = { items: [:string] }
type Result<t, e> <: Fallible = sum { Ok: t, Err: e }
```

Nightseam's parameters have exactly one role, `session`, and its slots
draw `connection`/`envelope`/any record or enum from the bound family.
Glyph's kinds generalise that: a parameter's kind says what shape of type
may fill it.

**Three projections of one algebra** (`RUNE_CODEGEN_STATUS.md:41-60`):
`NamedType` (full) → `LoweredShape` (nine kinds, code-facing,
`units/data/platform/lowered/ts/src/index.ts:44-52`, with an explicit
`opaque{reason}` marker where lowering loses information) → `RuntimeShape`
(nine kinds, for runtime traversal, `glyph/types/index.ts:105-114`, erasing
atom names, params and schemeRefs). Each projection has its own hash
family.

**Identity and hashing** — `units/data/contract/identity/ts/src/index.ts`:
branded `TypeHash`, `SchemeHash` (`s-`), `EntryHash`, `TheoryHash`,
`ContractHash` (`k-`), `LoweredShapeHash` (`ls-`). The load-bearing split
(`glyph/types/index.ts:350-357`): `SchemeHash` is purely structural;
role, boundary and theory feed `TheoryHash` and thence `ContractHash`.
Two types with the same shape share a `SchemeHash` even if they mean
different things.

**Theory — declarative semantics per declaration.**
`units/data/glyph/testdata/file/theory-combined.glyph`:

```glyph
type Post = { id: :string, tenantId: :string, authorId: :string, content: :string }
theory {
  identity key(field(id))
  scope field(field(tenantId))
  ref field(authorId) target(User) via key(field(id))
}
```

Identity, scoping and cross-type references are facts beside the type,
not encoded in it. Nightseam has no equivalent layer; its nearest thing is
the `sess` layer, which is about a session, not a type.

**No schema for the schema.** There is no JSON Schema for `.glyph`; the
parser plus the golden corpus is the specification. The 31 `.glyph` /
`.golden.json` pairs pin the canonical JSON encoding of every construct,
and the `*.vectors.json` files pin `expand`/`hash`/`kind`/`subtype`/
`substitute`/`pure` semantics as language-neutral vectors replayed by
both the TS and the Rust implementation (`units/test/glyph/core/interop/`,
`units/test/glyph/parser/interop/`).

> **Against Nightseam.** Same idea, more ambitious. Nightseam lacks kinded
> type parameters, trait constraints on generics, the structural-vs-semantic
> hash split, the `opaque{reason}` lowering marker, and a theory layer.
> Glyph does worse on one count that matters: with no machine-readable
> grammar, every consumer must link a parser, whereas
> `urn:nightseam:contract:1` can be validated by anything that reads JSON
> Schema.

### 1b. Metatype — a YAML repository metamodel with a real schema-of-the-schema

| | |
|---|---|
| Manifest | `units/meta/_schema.yaml` |
| Kind schemas | `units/meta/kinds/*.yaml` (19) |
| Path rules | `units/meta/paths.yaml`; layering rules `units/meta/layering.yaml` |
| Model types | `metatype/api/data/metatype-model/src/schema.ts` |
| Loader | `metatype/impl/bound/node/metatype/src/schema-loader.ts` |
| Instances | ~200 `units/api/*/*/metatype.yaml` |

Field types are a mini-DSL parsed from strings (`schema.ts:20-30`):

```ts
export type FieldType =
  | { kind: 'string' } | { kind: 'bool' } | { kind: 'integer' }
  | { kind: 'any' } | { kind: 'object' }
  | { kind: 'enum'; values: string[] }
  | { kind: 'list'; elementType: FieldType }
  | { kind: 'map'; valueType: FieldType }
  | { kind: 'ref'; targetKind: string }        // typed cross-fact reference
  | { kind: 'nested'; typeName: string };      // local TypeDef
```

Written in YAML as `list(surface_decl)`, `map(slot)`, `ref(api)`,
`enum(ts, rust, go, python)`. Each kind declares a `path_pattern` the
filesystem must satisfy, plus local `types:`. Records, enums, lists, maps,
references, nesting — no generics.

> **Against Nightseam.** Different job: this describes what units exist,
> where they live and what they may import, not a wire contract. One
> distinction transfers — `ref(kind)` with `dependency: false`, a reference
> that is not a code dependency, which Nightseam's import model conflates.

---

## 2. Code generation

### 2a. Rune — the generator

About twenty units under `units/lib/rune/gen-*`, each with TS and Rust
mirrors. Reference manual `RUNE_CODEGEN.md` (1,460 lines); what actually
works in `RUNE_CODEGEN_STATUS.md`; the target architecture in
`RUNE_PIPELINE.md` (164 lines, the single best document in the repo).

**Pipeline** — nine stages, each with a stated knows / does-not-know
(`RUNE_PIPELINE.md:153-163`, e.g. "Print: knows target syntax, doesn't know
semantics"):

```
parse → resolve → lower → route → generate → link → render → print → emit
```

Inputs: `.glyph` files, `rune.yaml`, `.space.yaml`, `.rune.<lang>.yaml`
sidecars. Everything converges on one `LoweredDeclaration[]` (`Product |
Sum | Alias | Callable`) that every generator pattern-matches on. The
linker has zero domain knowledge — it resolves imports purely by
output-unit identity — and `module_path` is guaranteed populated at route
time so no later stage derives paths from filenames.

**Emission is a two-stage AST, never strings.** Plugins emit
language-agnostic tagged `Pure` values (`ast::decl::*`, `bs-*`, `enc-*`
families); `render` maps those to language-specific tags (`ast::rust::*`,
`ast::ts::*`); `print` formats. `RUNE_PIPELINE.md:83-94`:

```
{ tag: "bs-callable", name: "RunnerService", methods: [
  { tag: "bs-method", name: "getCell", topology: "unary",
    request: { tag: "enc-delegate", type: "CellQuery" },
    response: { tag: "enc-delegate", type: "CellDetail" } } ]}
```

"Not a string containing `mark_tunnel::callable_adapt::bind_async_unary(...)`."

**Targets.** TypeScript and Rust only — the registry at
`units/lib/rune/gen-registry/ts/src/index.ts` registers exactly
`RUST_TARGET_REGISTRATION` and `TS_TARGET_REGISTRATION`. The backends
`units/lib/rune/gen-ts/ts/src/` and `units/lib/rune/gen-rust/ts/src/` have
near-identical file sets (`target-emit.ts`, `bind-emit.ts`, `adapter-ws.ts`,
`adapter-http.ts`, `adapter-cli.ts`, `mark-gen.ts`, `imports.ts`,
`theory.ts`, `golden.test.ts`) — per-language backends behind a `Target`
SPI, the same split as Nightseam's `internal/spi` and
`internal/languages/{golang,typescript}`. A third language is out of
process: `externalPlugins` speak JSON over stdio with a versioned protocol
(`units/lib/rune/gen-plugin/ts/src/index.ts`, `PROTOCOL_VERSION` /
`MIN_PROTOCOL_VERSION` negotiation).

**Plugins are orthogonal to targets** — `decl`, `contract-descriptor`,
`proto:mark`, `proto:ws`, `proto:http`, `proto:cli`
(`rune-codegen/schemas/rune.gen.schema.json:80`). A `matrix:` entry in
config expands plugin × target into named generators.

**Applicability predicates** choose which declarations a generator sees,
with `any`/`all` combinators (`rune.gen.schema.json:82-145`): `isData`,
`isService`, `isResource`, `isSerializable`, `isFirstOrder`,
`hasOperationBoundaries`, `hasIdentity`, `isCausal`,
`hasProtocolSafeOperations`.

**Generator configuration** — `rune-codegen/schemas/rune.gen.schema.json`:
`generators` (plugin × predicate × target × preset × roles), `targets` with
`runtimeImports` overrides, `layout.routes` matched first-match-wins on
`{space, role, lang}`, `importOverrides` keyed by `space:role:target`,
`matrix` expansion, `suppress` diagnostic rules, `externalPlugins`. Roughly
the shape Nightseam's renderer configuration will grow into.

**Artifact identity** — `units/data/glyph/types/ts/src/index.ts:785-870`:
`ArtifactKey`, `OutputUnitKey`, `OutputUnitPlan` separate four things
generators usually conflate — what an artifact *is*, which output unit it
lands in, what name it is imported by, and where the file goes. This is
what makes the domain-free linker possible.

**Incremental emission** — `.rune-manifest.json` with per-file
`contentHash` and per-unit `fingerprint`; `.space.yaml` is hashed into a
`contextHash` (`units/lib/rune/gen-batch/ts/src/discover.ts:20-60`).

**Parity between the two languages is checked four independent ways:**

1. Golden files per language — `units/data/rune/testdata/codegen/golden/typescript/*.ts` and `*.bind-stub.ts` (Rust siblings alongside), driven by `gen-ts/src/golden.test.ts` and `gen-rust/src/golden.test.ts`, refreshed by `update-golden.ts` in each.
2. Shared JSON vectors — `units/data/glyph/testdata/core/*.vectors.json`, replayed by both interop suites.
3. Structural symmetry lint — `validator.yaml` runs `symmetry.package-existence`, `symmetry.module-structure`, `symmetry.dependency-mirror`, `symmetry.workspace-membership`; per-unit waivers in `exceptions.yaml`, each with a `reason:`.
4. A language-neutral contract manifest — the `contract-descriptor` plugin emits one JSON per space with `fqName` / `schemeHash` / `contractHash` / `shapeClass` / `boundary`, explicitly "for CI gates, cross-language parity checkers" (`RUNE_CODEGEN.md:233-300`).

> **Against Nightseam.** Same shape — kernel plus per-language renderer
> SPI — but stricter about the boundary: the language-agnostic →
> language-specific two-stage AST is worth copying outright. Nightseam
> lacks external plugins as versioned subprocesses, an applicability
> algebra, a contract descriptor as a CI artifact, and incremental
> emission.

### 2b. rune-tester — end-to-end cases for the generator

`rune-tester/ARCHITECTURE.md`; cases in `rune-tester/cases/*/case.yaml`,
each run against a real scratch workspace. `rune-tester/cases/gateway/case.yaml`:

```yaml
name: "gateway generates and type-checks"
skip: [{ target: rust }]
steps:
  - scaffold: scaffold/
  - add-sources: sources/
  - generate: { expect: success, expect-files: [gateway/types/index.ts, gateway/mark/index.ts] }
  - typecheck: { expect: success }
  - add-consumer: consumer/
  - typecheck: { expect: success }
```

Cases: `clean`, `check-mode` (regenerate and diff), `incremental`
(`sources-v1` → `sources-v2`), `gateway`, `multi-service`. The harness
dogfoods — it is itself declared in `rune-tester/api/runner.glyph` and
`events.glyph` and served over generated WS E² bindings.

> **Against Nightseam.** Something Nightseam lacks outright. The
> `add-consumer:` → `typecheck:` pair proves generated packages are
> *usable* by a downstream package, a class of bug goldens never catch.

### 2c. Other generators

- `units/lib/mark/gen-ts`, `units/lib/mark/gen-rust`, `units/svc/mark/gen` — generate typed *value literals* (not types) from Mark `Pure` values. The one dependency cycle in the type stack (`CODE_TYPE_DEPS.md:69`).
- `metatype/app/node/metatype/src/commands/scaffold.ts` — scaffolds units from the kind schemas.

---

## 3. Protocol and wire

### 3a. Mark — the value currency and the duplex session protocol

| | |
|---|---|
| Value model | `units/data/mark/types/ts/src/index.ts:23-52` |
| Codec | `units/lib/mark/codec-json/ts/src/` (`encodeShape` / `decodeShape`, shape-directed) |
| Sessions | `units/lib/mark/session/ts/src/{session.ts, bisession.ts, push-runtime.ts, session-registry.ts}` |
| Tunnel | `units/lib/mark/tunnel/ts/src/{register.ts, resolve.ts, wireify.ts, channel.ts, ws-channel.ts, http-channel.ts, cli-channel.ts, counters.ts}` |

Five value sorts — `Nil | Compound | Bytes | Location | Arrow` — where
`Arrow` is a first-class capability *inside the value tree* and `Location`
is a handle. `isWire(v)` = no Arrow; `isPure(v)` = no Arrow and no
Location. Callbacks across the wire fall out of the type system: an arrow
in a payload is registered and replaced by a Location token on send
(`register`), and rebuilt as a proxy arrow calling back through the
channel on receive (`resolve`).

**Framing** — `session.ts:19-28`, JSON, symmetric in both directions:

```ts
interface RequestFrame  { c: string; h: string; p?: string }   // p absent ⇒ release
interface ResponseFrame { c: string; r: { ok: string } | { error: string } }
```

`c` correlation id, `h` handle id, `p` base64 of Mark codec bytes. A
frame with no `p` is the release / cancellation signal; `Value::Nil` is
the end-of-stream sentinel. Documented at `ELEVATION_MODEL.md:107-126`.
Three runtimes, one per symmetry class: `session.ts` (unary),
`push-runtime.ts` (server push), `bisession.ts` (full duplex; one receive
loop handles correlation-id responses and inbound `step` messages).

### 3b. The elevation model — the signature decides the transport

`rune-codegen/docs/ELEVATION_MODEL.md` (127 lines) and
`RUNE_CODEGEN.md:1311-1337`. Arrow nesting depth *is* the interaction
topology — detected, never annotated:

| signature | topology | elevation | transports |
|---|---|---|---|
| `A -> B` | `unary` | E⁰ | WS, HTTP, CLI |
| `A -> (… -> …)` | `outputLive` | E¹ | WS (events), HTTP (SSE) |
| `(… -> …) -> B` | `bidirectional` | E² | WS only |

Derived by `collectBoundaryArrowSites()` / `deriveBoundaryPlan()`, in both
TS and Rust.

**Capability profiles instead of protocol names** —
`units/data/glyph/types/ts/src/index.ts:710-753`:

```ts
export interface CapabilityProfile {
  clientSendsAfterInitial: boolean;
  serverSendsAfterInitial: boolean;
  serverReturnsLiveHandles: boolean;
  clientSuppliesLiveHandles: boolean;
  persistentHandleNamespace: boolean;
  cancellationBackpressure: boolean;
}
// UNARY_PROFILE (CLI) / PUSH_PROFILE (HTTP+SSE) / BIDI_PROFILE (WebSocket)
```

`profileForTopology()` gives the floor an operation needs; the transport
gives the ceiling; `filterCompatibleOperations()`
(`units/lib/rune/gen-lowering/ts/src/compatibility.ts`) drops incompatible
operations with a generation-time warning rather than emitting code that
cannot work (`units/lib/rune/gen-proto-ws/ts/src/index.ts:50-61`).

Nightseam declares a method's `direction` and an event's `direction` by
hand, and has exactly one profile. Elevation would let it infer "this is
a request", "this returns a stream", "this takes a callback" from the
types alone.

### 3c. The canonical callable IR

`units/data/glyph/types/ts/src/index.ts:646-695` — `CanonicalCallableModel
{ name, operations }`; each `CanonicalOperation` carries `inputType`,
`outputType`, `modeledErrors`, `boundaryPlan`, `boundaryTopology`,
`codecRefs {input, output, error, event}`, `transportHints {http, ws, cli}`,
`streamSite`, `callbackOutputType`, `capabilitySites`. Every protocol
emitter consumes this and never re-derives callable structure; `proto:mark`
owns payload semantics and `proto:ws|http|cli` project from it
(`GLYPH_GROVE_DELVE_RUNE_STACK.md:124-132`).

Transport hints (`index.ts:603-631`) are the per-protocol escape hatch:
HTTP gets `method` / `path` / `memberLocations{path|query|body|header}` /
`errorStatus`; WS gets `messageKind`; CLI gets `command`. Nightseam's
`go_name` / `ts_name` are a language-level cousin of this idea.

### 3d. The generated WebSocket format

`RUNE_CODEGEN.md:846-860`:

```
Client → Server:  { type: "get-user", id: "abc", value: "user-123" }
Server → Client:  { type: "result", id: "abc", data: {...} }
Server → Client:  { type: "event", event: {...} }   (repeating)
Server → Client:  { type: "error", id: "abc", code: "NOT_FOUND", message: "..." }
```

A hand-written comparator lives at
`units/proto/grid/ws/decl/bus/ts/src/index.ts`: `ClientEnvelope
{ request_id, message }` / `ServerEnvelope { request_id?, message }` with a
~25-variant discriminated `ClientMessage` union and an explicit
`unsubscribe`.

Note that this is a *second*, unrelated framing beside Mark's `{c,h,p}`;
see the closing remarks.

### 3e. Conformance across languages and transports

`units/test/rune/protocol-interop/` — `callable.glyph` (one callable, one
operation per elevation), `interop.manifest.json` with its
`interop.manifest.schema.json`, `orchestrator/`, `probe-ts/`, `probe-rust/`,
`wire-interchange/`, `fixtures/transcripts/`, `bench-results.json`,
`ci.sh`. `CONFORMANCE_LAYERS.md` splits it into L1 service projection /
L2 symmetry runtime / L3 protocol shim with a table of "a failure at this
layer means fix it *here*, once".

Scenarios are declarative and assert on capability accounting, not just
payloads:

```json
{ "id": "subscribe/three-events", "op": "subscribe", "kind": "outputLive",
  "validTransports": ["ws", "http"],
  "serverScript": { "streamEvents": ["StreamEvent.first", "..."] },
  "runtimeAsserts": { "minRegisters": 0, "minResolves": 1, "liveHandlesAfter": 0 },
  "bench": { "payloadClass": "small", "warmup": 100, "iterations": 1000 } }
```

`liveHandlesAfter: 0` is a leak assertion. Nightseam's
`duplex/go/duplextest` conformance suite is the counterpart; it does not
yet carry a declarative scenario manifest.

**A six-language wire-format test.** `units/proto/grid/ws/stub/bus/{ts,
rust, python, go, haskell, csharp}` with JSONL bridge drivers at
`units/test/grid/interop/{go/main.go, python/bridge.py, csharp/Program.cs,
haskell/app/Main.hs}`. The four non-generated stubs are hand-written and
exist to prove the wire format is implementable without the generator.

> **Against Nightseam.** Very close in intent — JSON frames over WS with
> correlation ids, requests / responses / events / cancellation — and
> further along in five places: elevation from the signature; capability
> profiles as structured feature sets with a generation-time gate;
> `Arrow` / `Location` as value sorts; the leak-asserting interop
> manifest; hand-written stubs in extra languages as a falsification test.
> Two things not to copy: base64-wrapping the payload inside a JSON frame,
> and two coexisting WS framings — Nightseam's one-profile discipline is
> the better call.

---

## 4. Layering and composition

**Unit addressing** — `units/{layer}/{pack}/{name}/{lang}/`, layers
`data | svc | lib | impl | app | proto | test`, `ts/` and `rust/` as
siblings (`SYMMETRY.md:9-96`). Nightseam's "one directory per component,
one subdirectory per language" is the same rule at smaller scale.

**Layering is declared three times, for three consumers:**

1. `units/meta/layering.yaml` — kind-level rules for the metatype validator:
   ```yaml
     - from: proto_bind
       allowed: [data, svc, lib, proto_decl]
   forbidden:
     - from: [proto_bind, proto_stub]
       to: impl
       message: "Proto layer must not depend on implementation - use injection via app layer"
   ```
2. `validator.yaml` — `arch.layering` for the devtools graph checker, plus the four `symmetry.*` checks.
3. `units/meta/paths.yaml` — the taxonomy as path patterns (`impl/{pack}/{api}/{lang}/{variant}/`), so a layout violation is visible from the filename alone.

**Root manifests, one job each:**

| file | declares |
|---|---|
| `.space.yaml` | `space: [my, namespace]` — the namespace owning the `.glyph` files below it; hashed into `contextHash` |
| `rune.work.yaml` | multi-project generation: project name → its `rune.yaml` |
| `roles.yaml` | path glob → role (`data/** → data`) |
| `symbols.yaml` | language-agnostic symbol kinds (TS `interface.data` / Rust `struct` → `data-shape`); `groups: public-api` |
| `validator.yaml` | which arch and symmetry checks run |
| `exceptions.yaml` | per-unit, per-check, per-language waivers, each with a `reason:` |
| `devtools.yaml` | workspace discovery and the wiring of the four above |
| `metatype.yaml` (per unit) | `kind`, `id`, `pack`, `name`, `version`, `description`, plus kind-specific fields |

**Composition kinds** — `units/meta/kinds/stack.yaml` defines a `stack`
with no code of its own: `provides: list(surface_decl{capability, protocol,
version, labels})`, `requires.deps: list(requirement{capability, protocol,
version, env_var, optional})`, `interface{env, files}`, `wiring{exports,
inject}`. `units/meta/kinds/impl.yaml:24` gives units `slots: map(slot)`
with `slot = { protocol, api: ref(api), optional }` — dependency injection
keyed by protocol × API, and `requirement.use: enum(compile, build, run)`
says in which phase a dependency is needed. (This "slot" is DI, a
different thing from Nightseam's type slots.)

**Forge — the build kernel** — `FORGE_KERNEL.md` (163 lines): four narrow
SPIs, `UnitStore` (resolve / fetch / publish; crates.io, npmjs, PyPI are
one abstraction), `FileProjector` (renders the resolved graph into
ecosystem files on a `Volume`), `Volume` (abstract write target),
`CompilerStrategy` (pure `describe()` declaring tool needs before the
solve; effectful `compile()` through a kernel-mediated host). Invariant
1: "No ecosystem manifest is a source input" — `Cargo.toml`,
`package.json`, lockfiles, `tsconfig.json` are projected, never sealed as
source. Invariant 7, the native-command litmus test: a human can enter
the projected volume and run the native build command.

**Transparent decomposition** — `TRANSPARENT_DECOMPOSITION.md` (~470
lines): every service boundary becomes six packages — `-data` (DTOs),
`-api` (interface), `-default` (impl), `-bind-http` (server adapter),
`-stub-http` (client), `-server` (wiring). The stub satisfies the same
interface as the impl, so a protocol round-trip is invisible to callers,
and the forbidden edges are enforced by the package manager rather than
by review.

> **Against Nightseam.** Different scope — a monorepo and build
> metamodel, not a contract layer. Two things transfer: waivers with
> recorded reasons for parity checks, and the `decl` / `bind` / `stub`
> triad as a *layering rule* (a bind may not import an impl) rather than a
> naming convention.

---

## 5. The design documents

| document | in brief |
|---|---|
| `GLYPH_GROVE_DELVE_RUNE_STACK.md` (255 lines) | One job per subsystem: Glyph declares, Grove parses source into path-local semantics, Delve resolves across units and emits facts, Rune projects declarations across boundaries. Grove/Delve understand source; Rune projects declarations — siblings around one substrate, not a pipeline. Rune must not depend on Delve, so codegen runs offline. `proto:mark` is the one boundary layer; `proto:ws|http|cli` project from it. |
| `RUNE_PIPELINE.md` (164) | The nine-stage target architecture with a knows / does-not-know per stage; one `LoweredDeclaration[]` everything pattern-matches on; tagged AST values, never strings; a linker with zero domain knowledge. |
| `rune-codegen/docs/ELEVATION_MODEL.md` (127) | Elevation = arrow nesting depth, auto-detected, fixing both session protocol and eligible transports (E⁰ / E¹ / E²); each level's wire path spelled out in `bindService` / `register` / `encode` → `decode` / `resolve` / `stubService`; the six-field `CapabilityProfile`. |
| `CODE_TYPE_DEPS.md` (91) | The dependency matrix among Glyph, Mark, Grove and Rune. Glyph is the root, Mark the currency (`Pure` is the universal boundary representation), Grove a leaf, Rune Glyph's heaviest consumer. The single cycle `@mark/codegen ↔ @rune/codegen` is named and confined to one module. |
| `FORGE_KERNEL.md` (163) | Four SPIs replacing one monolithic ecosystem plugin; seven invariants, above all that no ecosystem manifest is a source input. |
| `SYMMETRY.md` (~700) | TS and Rust are both first-class; "a change in one language is incomplete until the equivalent exists in the other." Identical layout, mechanical `kebab-case.ts` ↔ `snake_case.rs` mapping with 1:1 module tables, matching API surfaces modulo naming, a `{TS,Rust} client × {TS,Rust} server` interop matrix per domain — and a list of where asymmetry is *permitted*, so the rule stays enforceable. |
| `TRANSPARENT_DECOMPOSITION.md` (~470) | The six-packages-per-boundary recipe (see §4). |
| `META_FORGE_PARITY.md` (~330) | A file-by-file ledger of the metatype Rust → Forge rewrite: 87 files / 28,273 LOC → 69 / 22,973, each row Re-implemented / Not yet / Not re-implementing / Partial. Data types shrank 65% (typed specs + content-addressed snapshots replaced a fact model + blob store); SPI traits grew 3.5× (a formal kernel/factory boundary metatype never had). |
| `RUNE_CODEGEN.md` (1,460) | The reference manual: terminology guardrail, pipeline, one section per plugin (what it produces, artifact roles, runtime deps, import resolution, transport hints), the canonical callable model, the Glyph subset, the full `rune.yaml` reference, and a generate-vs-hand-write rule. `RUNE_CODEGEN_STATUS.md` is its companion: what works as of 2026-04-05, the registry-driven ownership split, the three-layer type projection. |

---

## 6. Ranked — what to read first when designing Nightseam

1. **`RUNE_PIPELINE.md`** — the stage table and the "tagged AST values, never code strings" rule. What `cmd/nightseam`'s kernel/SPI split should be measured against.
2. **`rune-codegen/docs/ELEVATION_MODEL.md` + `CapabilityProfile` (`units/data/glyph/types/ts/src/index.ts:710-766`)** — topology inferred from the signature; transports gated by a structured feature set with a generation-time check. Directly applicable to Nightseam's request / event / cancellation split.
3. **`units/test/rune/protocol-interop/`** — declarative cross-language × cross-transport scenarios with leak assertions and expected transcripts, and `CONFORMANCE_LAYERS.md` saying which layer owns a failure. What "one wire profile, two languages" should be proven by.
4. **`units/data/glyph/types/ts/src/index.ts`** — the whole contract model in one file. Read `:785-870` (artifact identity vs output unit vs import name vs file placement) even if nothing else.
5. **`rune-tester/`** — `add-consumer:` → `typecheck:`, `check-mode`, `incremental`. Cheap to copy.
6. **`units/lib/mark/session/ts/src/` + `units/data/mark/types/ts/src/index.ts:23-52`** — `Arrow` / `Location` as value sorts and the `{c,h,p}` / `{c,r}` frame pair. If Nightseam wants callbacks or live handles over its channel, this is the mechanism; if not, this is the complexity being declined, made explicit.
7. **`rune-codegen/schemas/rune.gen.schema.json`** — a well-factored generator configuration schema.
8. **`validator.yaml` + `exceptions.yaml` + `SYMMETRY.md`** — parity as a mechanically checked property, with reasoned waivers.

Not to copy: the absence of a machine-readable schema for `.glyph`, and
the two coexisting WS framings with base64 payloads.
