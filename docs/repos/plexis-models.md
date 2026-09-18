# plexis — unit model, protocol and (the absence of) generation, compared to Nightseam

A survey of everything in [plexis](plexis.md) that does what Nightseam
does. Paths are relative to `C:\Development\bitspark\systems\plexis`, on
`plexis-main` at commit `5eb8b254f` (2026-05-25); most are under
`apps/plexis/`. The yardstick is Nightseam's own model in
[internal/contract/api.schema.json](../../internal/contract/api.schema.json);
see the [aifunc3 page](aifunc3-models.md) for it spelled out.

Related pages: [aifunc3](aifunc3-models.md) · [bitmachine](bitmachine-models.md)
· [cross-repo comparison](../RELATED_MODELS.md).

---

## Headline

Plexis has an unusually well-developed *conceptual* model of exactly what
Nightseam does — a formal algebra for bind/stub pairs with a written
transparency law, a role-first unit taxonomy with machine-derived facts,
and a per-transport `decl / bind / stub` triple — and **no generator
anywhere**. Every contract, wire type, dispatch table and client stub is
hand-written twice, in Rust and in TypeScript, and parity is policed by a
string-grep lint that checks only what someone remembered to list. The
measured result: the transport-neutral `ManagementService` has 116
operations in Rust and 99 in TypeScript.

It is the strongest argument for Nightseam's existence in any of the
three repositories, and it holds several wire-profile ideas Nightseam
does not yet have.

---

## 1. The role-first unit model

### 1a. The tree as it actually is

`apps/plexis/README.md` describes `units/surface-*/*` and `units/storage-*/*`;
the real tree is `units/<role>/<axis>/<domain>/<lang>/`:

```
units/api/surface/{control, management, observability, scheduling, traffic}/{rs, ts}
units/api/storage/{accounts, artifacts, common, events, execution, machines, observations, profiles, runs, sandbox, scheduler}/rs
units/contr/{surface, storage}/<domain>/unit.yaml            code-less identity anchors
units/proto/surface/websocket/{decl, bind, stub}/{control, management}/{rs, ts}
units/proto/surface/http/bind/control/rs
units/impl/surface/<domain>/<lang>/default
units/impl/storage/<domain>/<lang>/{postgres, fs}
units/data/plexis/{execution, foundation, identity, observability, runtime, tenancy, traffic}/{rs, ts}
units/lib/plexis/{auth, context, crypto, db, execution, http-capture, otel, sync}/<lang>
units/app/{control, runtime, surface}/{server, cli, browser}/<service>/<lang>
units/ops/{deploy, smoke}/cli/<tool>/ts
units/dev/analysis/{rs, ts}
units/test/boundary/rs
```

Kind census from the `kind:` field of every `unit.yaml`: `api` 14, `contr`
10, `app` 9, `lib` 7, `data` 5, `impl-storage-postgres` 10,
`impl-storage-fs` 1, `impl` 2, `proto_decl` 3, `proto_bind` 3, `proto_stub`
2.

### 1b. The vocabulary

Normative sources: `apps/plexis/docs/spec/LAYERS.md` (the metamodel) and
`apps/plexis/docs/spec/PROTOCOLS_SPEC.md`.

| term | what it is |
|---|---|
| **surface** | A *direction*, not a thing. `surface-*` is inbound (what the core exposes); `storage-*` is outbound (what the core needs). The same `api / contr / proto / impl` roles apply to both. |
| **api** | A language-specific service interface — a Rust `trait` or a TS `interface` — plus its request and response structs: `ManagementService`, `CoreControlService`, `SandboxLifecycleService`. |
| **contr** | A code-less, language-less identity anchor. `units/contr/surface/management/unit.yaml` is six lines of YAML and nothing else; `api` and `impl` units reference it. |
| **proto_decl** | Wire types only: `ClientMessage` / `ServerMessage` enums, envelope shapes, protocol-version constants, encode/decode helpers. |
| **proto_bind** | The server adapter: takes `Arc<dyn Service>`, exposes it on a transport. `bind_P : Func<LANG, API> -> Address<PROT, API>` |
| **proto_stub** | The client adapter: takes a URL, implements the same service interface. `stub_P : Address<PROT, API> -> Func<LANG, API>` |
| **impl** | The application core, or a storage adapter (`impl-storage-postgres`, `impl-storage-fs`). |
| **app** | The composition root; the only layer allowed to see both `impl` and `proto`. |

The algebra, from `docs/spec/LAYERS.md`:

```
| API   | Abstract API identity          | engine, storage, auth        |
| LANG  | Implementation language        | ts, rust, go                 |
| PROT  | Wire/interaction protocol      | cli, http, grpc, websocket   |

bind_P : Func<LANG, API> -> Address<PROT, API>
stub_P : Address<PROT, API> -> Func<LANG, API>

Transparency Law:  stub_P(bind_P(f)) ~= f
```

`docs/spec/PROTOCOLS._DESIGNmd` (the filename typo is in the repo) adds
the beta law `bind_P(stub_P(a)) ~_P a` and the quotient reading
`Address<PROT, API> / ~_P ≅ Func<LANG, API> / ~`, with a four-point
argument for why proto can never import impl: the `Func` is a type
parameter injected at runtime by `app/`.

> **Against Nightseam.** Same idea, stated more formally than Nightseam
> states it. Nightseam's kernel/SPI split is the machine realisation of
> this algebra — the Go server package is `bind_ws`, the TS client is
> `stub_ws` — but Nightseam has no written transparency law. Adopting one
> would give the generator a testable acceptance criterion: generated
> stub ∘ generated bind ≡ direct call, per family.

### 1c. Four parallel, unreconciled declaration formats

1. **`unit.yaml`** — the `LAYERS.md` schema. Flat, hand-written, no validator in the repo:
   ```yaml
   # units/proto/surface/websocket/decl/management/rs/unit.yaml
   kind: proto_decl
   id: plexis-proto-surface-websocket-decl-management-rs
   name: Plexis Management Surface WebSocket Protocol
   pack: plexis
   protocol: websocket
   api: management
   language: rs
   version: 0.1.0
   ```
2. **`.bitmachine.unit.yaml`** — prose description and tags, `$schema=https://bitspark.dev/schemas/bitmachine/unit/v1.json` (remote, not in tree).
3. **`.bitmachine.yaml`** / `apps/plexis/.bitmachine.yaml` — build identities, source-input sets, and an ID-correlation-domain registry (prefix → field names → runtime surfaces), the closest thing here to a typed cross-service vocabulary:
   ```yaml
   idCorrelationDomains:
     - domainId: sandbox.execution
       kind: prefixed-id
       prefix: sexec_
       fields: [sandboxExecutionId, sandbox_execution_id]
       runtimeSurfaces: [plexis/control-plane, plexis/management-api, ...]
   ```
4. **`unit.json`** (leukos only) — a typed *address* form Plexis consumes, `kernel/leukos/packages/leukos-surface-protocol-ws/unit.json`:
   ```json
   { "stage": "source", "slice": "leukos.foundation", "role": "surface-protocol-ws",
     "form": "package", "qualifiers": { "language": "ts", "name": "leukos-surface-protocol-ws" },
     "dependencies": [{ "type": "source",
       "key": "leukos.foundation/source/surface-api/language=ts/name=leukos-data" }] }
   ```

> **Against Nightseam.** Four competing formats, none validated locally;
> Nightseam's one schema-validated triple is strictly better. The leukos
> `slice / stage / role / qualifiers` address key is the idea to keep — a
> structured URN for units that composes into dependency keys.

### 1d. Rust ↔ TypeScript: nothing shared, everything copied

`Cargo.toml` (55 workspace members) and `pnpm-workspace.yaml`
(`units/**/ts` plus 30 cross-workspace paths) are disjoint. There is no
IDL, no shared schema, no build step between them. The two sides share
JSON field names only, held together by hand through serde attributes:

```rust
// units/proto/surface/websocket/decl/management/rs/src/lib.rs
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "camelCase")]
pub enum ClientMessage {
    #[serde(rename = "subscribe")]
    Subscribe {
        #[serde(rename = "subId")] sub_id: String,
        view: ViewSpec, since: Option<u64>,
        #[serde(rename = "_meta", skip_serializing_if = "Option::is_none")] meta: Option<TraceMeta>,
    },
```
```ts
// units/proto/surface/websocket/decl/management/ts/src/index.ts   (48 lines against 268 in Rust)
export type ClientMessage =
  | { type: "subscribe"; subId: string; view: ViewSpec; since: number | null; _meta?: TraceMeta }
```

Measured drift in the transport-neutral contract alone:
`units/api/surface/management/rs/src/lib.rs` `trait ManagementService` has
**116** `async fn`; `units/api/surface/management/ts/src/index.ts`
`interface ManagementService` has **99**. Seventeen operations exist only
in Rust.

> **Against Nightseam.** The single biggest thing Nightseam does better:
> one family renders both packages, and drift is impossible by
> construction.

---

## 2. Declarative schemas and type models

**There is no declarative type model for the API.** Every record, enum
and generic is a Rust struct or a TS interface. A search for `$schema`,
JSON Schema files, zod, or any IDL under `apps/plexis/units/*/{api, data,
proto}` finds nothing. Three near-misses:

**(a) Deployment driver config —**
`apps/plexis/units/ops/deploy/cli/deploy/ts/src/drivers/local-compose/schemas.ts`
and `core/schema.ts`, the only schema-driven typing in the app. JSON
Schema 2020-12 as a `const` literal, types derived with `json-schema-to-ts`,
validated with Ajv:

```ts
export const LOCAL_COMPOSE_GLOBAL_SCHEMA = {
  type: "object", additionalProperties: false, required: ["profile"],
  properties: { profile: { enum: ["postgres-only", "full-stack"] } },
} as const;
export type LocalComposeGlobalConfig = FromSchema<typeof LOCAL_COMPOSE_GLOBAL_SCHEMA>;
```

behind a driver SPI in `core/types.ts` — `DriverSchemas { global, stages:
{build, inspect, plan, apply} }` and `DeploymentDriver<TGlobal, TBuild,
TInspect, TPlan, TApply, …>`. Same idea as Nightseam (the schema is the
source of truth, types are derived), applied to deployment config rather
than the API. The generic-parameterised driver SPI, where each driver
declares its own config schema and the kernel validates and passes typed
config through, is a clean pattern for Nightseam's per-language renderer
SPI.

**(b) `apps/eidos2/cell-graph.yaml`** (323 lines; a sibling app Plexis
does not depend on, but the machine-checkable version of Plexis's prose
architecture doc). A declarative, default-deny layering contract:

```yaml
layout:
  unitsRoot: units
  allowedKinds: [api, app, cap, data, deploy, impl, pack, proto_decl, proto_bind, proto_stub, test]
  leaves: { language: [ts, rs], representation: [data] }
  manifests: { packageJson: { allowedLeaf: ts }, cargoToml: { allowedLeaf: rs } }
dependencies:
  workspace:
    default: deny
    allowed:
      - { name: proto-bind-depends-on-proto-decl, fromKinds: [proto_bind], toKinds: [proto_decl] }
    requirements:
      - { name: proto-stub-requires-api, from: ["proto_stub/**"], toKinds: [api], min: 1 }
sourceImports:
  forbidden:
    - { name: kernel-must-not-import-protocol, from: ["api/kernel/**"], patterns: ["@eidos2/api-protocol-"] }
```

Nightseam has nothing like it. It could *emit* one for the packages it
generates — a dependency-rule file asserting `stub → decl → data` and
forbidding `stub → impl` — turning architectural intent into a checkable
artifact.

**(c) `apps/eidos2/units/api/kernel/wire-value/ts/src/index.ts`** — a
minimal JSON value model with validation and canonicalisation:

```ts
export type KernelWireValue = null | boolean | number | string
  | readonly KernelWireValue[] | { readonly [key: string]: KernelWireValue };
export function canonicalKernelWireJson(value: unknown): string {
  return JSON.stringify(sortKernelWireValue(detachKernelWireValue(value)));
}
```

with round-trip diagnostics (`kernel.wire_value.round_trip_mismatch`).
Canonical JSON plus a round-trip-mismatch diagnostic is a conformance
primitive Nightseam could generate per family.

---

## 3. Code generation — none

Across `apps/plexis`, the only "generated" marker is
`units/ops/deploy/cli/deploy/ts/src/drivers/ssh-compose/public-edge.ts:40`,
`"# Generated by plexis-deploy. Do not edit manually."` — an nginx
config. Stubs are written by hand:

- `units/proto/surface/websocket/stub/management/ts/src/client.ts` — 766 lines; `DefaultManagementWebSocketClient implements ManagementService` and dispatches each of ~99 methods as `this.command("profiles.list", req)`.
- `units/proto/surface/websocket/bind/management/rs/src/lib.rs` — 2,389 lines with a **129-arm string match**: `"auth.signup" => {…}`, `"providerAccounts.authStart" => {…}`, …; plus `observability.rs` (2,006 lines) and `energeia_ws/` (4,333 lines) of adapter glue.
- `units/proto/surface/websocket/decl/control/rs/src/lib.rs` is a near-literal copy of the management decl — same `ViewSpec`, `TraceMeta`, `ClientMessage`, `ServerMessage`, same eight wire tags. The shared wire profile is duplicated per family, not factored.

### Parity checking: a grep lint

`units/dev/analysis/rs/src/analyses/trace_context_propagation.rs`, section
"C) TS/Rust decl parity":

```rust
if !contents.contains("interface TraceMeta") { /* warn ts-decl-missing-trace-meta */ }
for field in ["traceparent", "tracestate", "baggage", "traceId"] {
    if !contents.contains(field) { /* warn ts-decl-trace-meta-missing-field */ }
}
let expected_variants = [("command","Command"),("subscribe","Subscribe"),("unsubscribe","Unsubscribe"),
    ("command-ack","CommandAck"),("command-error","CommandError"),("subscribed","Subscribed"),
    ("sync","Sync"),("error","Error")];
for (wire, display) in expected_variants {
    if !variant_line_has_meta(&contents, wire) { /* warn ts-decl-variant-missing-meta */ }
}
```

A hardcoded list of eight wire tags and four field names, checked by
substring search on the TS file. It caught nothing about the seventeen
missing `ManagementService` methods, because it only covers trace
metadata.

> **Against Nightseam.** *The* differentiator. Nightseam generates both
> sides; parity is not a lint, it is an identity. This file beside the
> 116-vs-99 count is the one-slide case for the generator.

### What is impressive: the analysis engine as an SPI model

`units/dev/analysis/rs/src/` (7,480 lines) is a fact-store pipeline
architecturally close to a Nightseam kernel/SPI:

- A fixed stage pipeline `workspace → units → ast → symbols → semantics → references` (`kernel/stage.rs`, `kernel/engine.rs`).
- Typed plugin descriptors declaring `consumes` / `produces` fact families, `depends_on_plugins`, `stage`, `languages`, and `contract: FactContract::core(ContractStage::Units)` (`derivations/unit_structure.rs`).
- Every fact carries `FactProvenance { source_id, source_kind, language, confidence, engine, contract }` (`model/facts.rs`, 948 lines).
- Cross-language external providers over a JSON protocol: the Rust kernel shells out to `plexis-analysis providers run --plugin <id> --stage <stage> --input -` for TS semantics (`providers/external_command.rs`, `dev/analysis/ts/src/protocol.ts`).
- The path grammar is derived, not declared — `derivations/common.rs`:
  ```rust
  pub fn valid_role_bucket(role: &str) -> bool {
      matches!(role, "app"|"api"|"cli"|"client"|"data"|"design"|"dev"|"impl"|"lib"|"contr"|"proto"|"ui"|"worker")
  }
  // conforms_units_shape = segments[0]=="units" && valid_role_bucket(role) && lang_slot ∈ {rs, ts}
  ```

A versioned fact contract with provenance, and a subprocess plugin
protocol for language-specific work, are both things Nightseam lacks; if
its SPI ever hosts a third-party renderer out of process, this is the
reference design. Also `units/test/boundary/rs/src/lib.rs` — boundary
rules as `#[test]`s that grep every `Cargo.toml` / `package.json` for
denied dependency markers. Crude, but it runs in CI.

---

## 4. Protocol and wire

### 4a. Management and control surfaces — one duplicated, untyped-payload profile

`units/proto/surface/websocket/decl/{management, control}/rs/src/lib.rs`
and `decl/management/ts/src/index.ts`:

```ts
export type ClientMessage =
  | { type: "subscribe"; subId: string; view: ViewSpec; since: number | null; _meta?: TraceMeta }
  | { type: "unsubscribe"; subId: string; _meta?: TraceMeta }
  | { type: "command"; id: string; command: string; data?: unknown; _meta?: TraceMeta }
  | { type: "ping"; nonce: number };
export type ServerMessage =
  | { type: "subscribed"; subId: string; _meta?: TraceMeta }
  | ({ type: "sync"; subId: string; _meta?: TraceMeta } & SyncFrame)
  | { type: "unsubscribed"; subId: string }
  | { type: "command-ack"; id: string; data?: unknown; _meta?: TraceMeta }
  | { type: "command-error"; id: string; message: string; _meta?: TraceMeta }
  | { type: "error"; subId: string; code: string; message: string; _meta?: TraceMeta }
  | { type: "pong"; nonce: number };
```

Against the concerns of `nightseam.duplex/1`:

| concern | plexis management/control |
|---|---|
| request / response | `command` / `command-ack` / `command-error` correlated by `id`. Payloads are `unknown` / `serde_json::Value`; the operation name is a *runtime string*, so nothing connects `command: "profiles.list"` to `ProfilesListResponse` except the 129-arm match in the bind. |
| events | Not modelled as events; replaced by subscriptions plus state sync (§4b). |
| cancellation | Absent from the wire. Client-side only — `ManagementTimeoutError` with `connectTimeoutMs` / `commandTimeoutMs` in `stub/management/ts/src/client.ts`; the server keeps running the command. |
| backpressure | Absent from the wire. Nine sites in `impl/surface/control/rs/default/src/sync/*.rs` handle `Err(broadcast::error::RecvError::Lagged(_))` by emitting a fresh reset frame. |
| presence | None. |
| correlation | `_meta` on every operational frame. Rust implements OTel `Injector` / `Extractor` directly on `TraceMeta`; TS degraded it to `Record<string, string>` with a comment disowning W3C as "a carrier's concern". |

### 4b. `SyncFrame` — a fourth frame class

`units/lib/plexis/sync/rs/src/lib.rs`:

```rust
pub enum Patch { Replace{value}, Set{path: Vec<String>, value}, Remove{path}, Batch{ops: Vec<Patch>} }

pub struct SyncFrame {
    pub prev: Option<u64>,     // frame this builds on; None ⇒ reset
    pub seq: u64,              // strictly monotonic per view
    pub ops: Vec<Patch>,
    pub reset: bool,           // complete baseline snapshot
    pub epoch: Option<String>, // set on reset; drops late frames from a prior epoch
}
```

Client side, `stub/management/ts/src/types.ts`: `SubscriptionHandle<T>`
with `status: "subscribing" | "live" | "stale" | "closed"`, a pluggable
`reduce(state, frame)`, and automatic resubscribe on reconnect.

> **Against Nightseam.** Something Nightseam lacks: a first-class
> *state-replication* frame beside request / response / event, with
> sequence, epoch and reset semantics that survive a reconnect.
> Nightseam's events are a log; `sync` is a materialised view. Worth
> considering as a fourth frame class in the wire profile.

### 4c. Energeia session protocol — the best duplex example in the tree

`kernel/energeia/units/proto/default/ws/decl/session/{rs, ts}` (541 Rust
lines, 137 TS), with full `decl / bind / stub` in both languages:

```ts
export const PROTOCOL_VERSION = "energeia.session.ws.v1" as const;
export type ClientMessage =
  | { type: "start_session"; id: string; _meta?: TraceMeta; request: AgentSessionStartRequest }
  | { type: "submit_input";  id: string; request: SessionInputSubmitRequest }
  | { type: "interrupt_run"; id: string; request: SessionInterruptRequest }
  | { type: "cancel_run";    id: string; request: SessionCancelRequest }
  | { type: "subscribe_session"; id: string; subscriptionId: string; sessionId: string;
      cursor?: SessionJournalCursor | null; sinceJournalPosition?: number | null }
  | { type: "unsubscribe_session"; id: string; subscriptionId: string };
export type ServerMessage =
  | { type: "connected"; connectionId: string; protocolVersion: typeof PROTOCOL_VERSION }
  | { type: "session_event"; subscriptionId: string; event: SessionEvent }
  | { type: "attachment_lease_updated"; subscriptionId: string; lease: AttachmentLease }
  | { type: "request_error"; id: string; code: string; message: string };
```

Here each operation *is* typed — `type` tag ↔ `request` payload ↔ paired
`ServerMessage` variant. `cancel_run` and `interrupt_run` are real wire
operations; `subscribe_session` carries a journal cursor for replay;
`AttachmentLease` is a presence / ownership primitive.
`kernel/energeia/docs/CONTRACT-V1.md` restates the transparency law for
it and adds "replay must not re-execute side effects."

> **Against Nightseam.** The closest thing here to a Nightseam family,
> hand-built. Cancellation as an operation, cursor-based replay and
> attachment leases are all things Nightseam's `sess` layer should have a
> first-class story for; its `conversation` clause is a narrow special
> case of the journal-cursor idea.

### 4d. Leukos surface protocol — per-action union, chunked upload, a correlation carrier

`kernel/leukos/packages/leukos-surface-protocol-ws/src/index.ts` (227
lines, TS only):

```ts
export const SURFACE_WS_PROTOCOL = "leukos.surface.v1";
export type SurfaceRequestMessage =
  | { kind:"request"; id:string; action:"session.start"; payload: StartSessionInput; _meta?: SurfaceCorrelationMeta }
  | … 21 more arms …
export type UploadStartMessage = { kind:"upload-start"; id:string; action:"artifact.put";
    payload: Omit<PutArtifactInput,"bytes"> & { totalBytes:number } };
export type UploadChunkMessage = { kind:"upload-chunk"; id:string; chunk:string; final:boolean };
export type SurfaceSuccessMessage = | { kind:"response"; id; action:"session.start"; ok:true; payload: LeukosSession } | …;
export type SurfaceEventMessage = { kind:"event"; protocol: typeof SURFACE_WS_PROTOCOL; event: LeukosSurfaceEvent };
```

Plus a `MessageCorrelationCarrier` SPI (`inject` / `runWithExtracted` /
optional `wrapHandler`) so the wire protocol stays carrier-agnostic —
Plexis's TS stub imports this carrier for its own `_meta`.

> **Against Nightseam.** The request / response arm pairing is exactly
> what a generator should emit, written by hand 22 times. Two things to
> copy: the `MessageCorrelationCarrier` SPI, which keeps trace context out
> of the wire schema and injects it through a pluggable carrier; and the
> `upload-start` / `upload-chunk` framing with `totalBytes` and `final`,
> a chunked-transfer shape Nightseam's profile lacks.

### 4e. Control plane ↔ data plane: HTTP headers as the contract

`units/proto/surface/http/bind/control/rs` (the edge proxy; its
`unit.yaml` says `protocol: edge-proxy`) and
`units/lib/plexis/context/rs/src/lib.rs`:

```rust
pub const HEADER_SANDBOX_EXECUTION_ID: &str = "x-plexis-sandbox-execution-id";
pub const HEADER_AGENT_SESSION_ID: &str = "x-plexis-agent-session-id";
pub const HEADER_RUNTIME_BINDING_ID: &str = "x-plexis-runtime-binding-id";
// 20+ more
```

`docs/RUNTIME_TOPOLOGY.md` is normative: the edge proxy strips
client-supplied `x-plexis-*` headers and re-derives them from the
server-side token subject binding; the control plane is never in the byte
path.

> **Against Nightseam.** The internal service-to-service seam is untyped
> string headers — no contract at all. Nightseam lacks a notion of a
> *metadata profile* — trusted vs untrusted fields, strip-and-rederive
> rules — that would cover such a seam.

### 4f. The Sandbox kernel contract — the cleanest `api / decl / bind / stub` in the tree

`kernel/sandbox/` (10 crates, Rust only):

```
units/data/default/model/rs                          SandboxPhase, SandboxStartPolicy, SandboxSensitivity, requests/responses
units/api/default/{lifecycle, catalog, diagnostics, io, artifacts, kernel}/rs   #[async_trait] traits
units/proto/default/ws/decl/lifecycle/rs             PROTOCOL_VERSION = "sandbox.lifecycle.ws.v1"
units/proto/default/ws/bind/lifecycle/rs             axum router over Arc<dyn SandboxLifecycleWsService>
units/proto/default/ws/stub/lifecycle/rs             SandboxLifecycleWsClient implements the same traits
```

The bind composes three traits into one through a blanket impl — family
composition:

```rust
pub trait SandboxLifecycleWsService:
    SandboxCatalogService + SandboxLifecycleService + SandboxDiagnosticsService {}
impl<T> SandboxLifecycleWsService for T where
    T: SandboxCatalogService + SandboxLifecycleService + SandboxDiagnosticsService {}
```

Plexis consumes it at one point,
`units/impl/surface/control/rs/default/src/sandbox/lifecycle.rs`,
connecting a `SandboxLifecycleWsClient` per call. The TS sandbox CLI
(`units/app/runtime/cli/sandbox/ts`, `src/lib/api.ts`) bypasses the
contract entirely and hand-builds URLs (`buildTerminalWsUrl`,
`buildLogsWsUrl`) — because there is no TS stub and no generator to make
one.

> **Against Nightseam.** Same architecture, one language. Trait
> composition into a transport service is the analogue of Nightseam's
> imports and slots; the missing TS side is precisely the gap a generator
> closes.

---

## 5. The design documents

| document | in brief |
|---|---|
| `apps/plexis/ARCHITECTURE_MODEL.md`, `UNITS_REORGANIZATION_PROPOSAL.md` | Five-line pointer stubs to the copies under `docs/`. |
| `apps/plexis/docs/ARCHITECTURE_MODEL.md` (1,016 lines) | The conceptual target: an application core between an inbound `surface-*` family and outbound `storage-*` families, `decl / bind / stub` triples per transport, a `bootstrap` composition root as the only layer seeing both core and adapters. Allowed and forbidden dependency directions, six invariants, and "consumers depend on service contracts, not transport stubs" via a `DeploymentClientFactory` returning a `ConnectedService`. Caveat: a rename script collapsed `surface-api` and `storage-api` both to `contr`, leaving a section titled "Why `contr` And `contr` Must Both Exist" and diagrams where the two are indistinguishable. |
| `apps/plexis/docs/UNITS_REORGANIZATION_PROPOSAL.md` (1,011 lines) | The physical layout with one hard rule: the first directory under `units/` is a role, never a domain (`contr/management`, not `management/contr`), and every unit carries an explicit `<lang>` slot even when only one language exists. Full current → target mapping from `claude-gateway2`, a six-slice migration order, six discipline rules. The shipped tree adds an `api/surface/…`, `api/storage/…` axis this doc does not describe. |
| `apps/plexis/docs/spec/LAYERS.md` | The metamodel: `API` / `LANG` / `PROT`, `Func<LANG, API>` / `Address<PROT, API>`, `bind_P` / `stub_P`, the transparency law, a path-pattern table per kind, an allowed-import matrix, and YAML schemas for all ten `unit.yaml` kinds. The schema for the schema — as prose, with no validator. |
| `apps/plexis/docs/spec/PROTOCOLS_SPEC.md` | Governs `units/proto/`. Every protocol (CLI, HTTP, WS, gRPC) is the same `decl / bind / stub` triple with shared per-protocol primitives in `lib/proto/{protocol}/{lang}`. Argues CLI *is* a protocol (argv + stdin = request, exit code + stdout = response, line-buffered stdout = streaming); states `proto/* -> impl/* FORBIDDEN`; ends with WebSocket-portability rules (`WebSocketLike`, injectable `WebSocketFactory`, `addEventListener` not `.on()`). |
| `apps/plexis/docs/spec/PROTOCOLS._DESIGNmd` | The rationale: quotient semantics, both eta and beta laws, and why proto can never import impl. |
| `apps/plexis/docs/RUNTIME_TOPOLOGY.md` | The four-role runtime — `control` / `management` / `ingress` / `egress_worker` — with the control plane never in the byte path; the Energeia agent-session flow; managed HTTP / WS / CONNECT flows; `zone_id` as a locality hint; the gateway-token trust boundary. |
| `apps/plexis/STORAGE.md` | Postgres for all durable control-plane state (~1.3 GiB locally), an `artifacts` filesystem backend, the list of stored concepts; `secrets/ infra/ work/` are environment state, not product state. |
| `apps/plexis/TRACES.md` | DB-backed and OTLP tracing: `PgTelemetryLayer` → `trace_logs`, `PgSpanExporter` → Postgres spans, otel-collector / Jaeger in local compose. Records carry `trace_id`, `traceparent`, `tracestate`, `correlation_refs_json`, execution ids. |
| `apps/plexis/.bitmachine.yaml` | The build and identity manifest: seven build identities (context, Dockerfile, `sourceInputs`, `buildRecipeInputs`) and the `idCorrelationDomains` registry mapping prefixes (`esess_`, `erun_`, `sexec_`, `rbind_`, `rexec_`) to field aliases and the surfaces allowed to carry them. |
| `apps/plexis/docs/PLATFORM_PRIMITIVE_GAP_AUDIT.md` | Audits the Plexis UI against the shared kernel packages (OPSIS / SEMA / TELOS / LEUKOS / NOMOS / ARCHON / SKOPOS) to decide which gaps to fix upstream. A model of "don't let the app privatise a kernel concern". |
| `apps/plexis/docs/ideas/*` | RFC-style specs for agent execution contexts (API, persistence / migration, security / auth, state / sequence). `KERNEL_FIRST_MAPPING_NOTE.md` states the reusable rule: if a sentence explains how Plexis currently achieves a fact, it is an implementation detail; if it explains what must be true across runtimes, it is a kernel contract. |
| `SYMMETRY.md` (system root; identical on `main`) | TS and Rust as co-equal runtimes with mechanical mirroring and a `{TS, Rust} client × server` interop matrix. It references a `rune/gen` + `rune/codegen/cli` generator with `contract-descriptor` and `lowering` modules that is **not present in this checkout** — the generator lives on `main` under `kernel/eidos` (see the [bitmachine page](bitmachine-models.md)), and Plexis was built without it. |

---

## 6. Ranked — what to read first when designing Nightseam

1. **`apps/plexis/docs/spec/LAYERS.md` + `PROTOCOLS_SPEC.md` + `PROTOCOLS._DESIGNmd`** — the `bind_P` / `stub_P` algebra and the laws `stub(bind(f)) ≈ f`, `bind(stub(a)) ≈ a`. Give Nightseam a written law, then have the generator emit a round-trip conformance test proving it per family. The highest-value borrow.
2. **`units/dev/analysis/rs/src/analyses/trace_context_propagation.rs` §C** beside the 116-vs-99 method count — the empirical case for generation and the exact failure mode to cite.
3. **`kernel/energeia/units/proto/default/ws/decl/session/ts/src/index.ts` + `kernel/energeia/docs/CONTRACT-V1.md`** — typed per-operation pairing, `cancel_run` / `interrupt_run` as wire operations, a journal cursor for replay, `attachment_lease_updated` for presence, and "replay must not re-execute side effects". Nightseam's profile should cover all five.
4. **`units/lib/plexis/sync/rs/src/lib.rs` + `units/proto/surface/websocket/stub/management/ts/src/types.ts`** — `Patch` / `SyncFrame` and `SubscriptionHandle`: sequenced, epoch-tagged state replication with reset on reconnect and a pluggable reducer. A fourth frame class.
5. **`kernel/leukos/packages/leukos-surface-protocol-ws/src/index.ts`** — the `MessageCorrelationCarrier` SPI and the `upload-start` / `upload-chunk` framing.
6. **`apps/eidos2/cell-graph.yaml`** — the declarative default-deny layering contract Nightseam could generate for the packages it emits.
7. **`kernel/sandbox/`** in full — the smallest end-to-end `data → api → decl → bind → stub` family, with blanket-impl trait composition; then `apps/plexis/units/app/runtime/cli/sandbox/ts/src/lib/api.ts` hand-building URLs because no TS stub exists.
8. **`units/ops/deploy/cli/deploy/ts/src/core/{schema.ts, types.ts}` + `drivers/*/schemas.ts`** — schema-as-const, `FromSchema`, Ajv, behind a generic driver SPI; a compact model for a renderer SPI where each renderer declares its own config schema.

Honourable mention: the leukos `unit.json` address key
(`leukos.foundation/source/surface-api/language=ts/name=leukos-data`) — a
composable URN for units, closer in spirit to `urn:nightseam:contract:1`
than anything else here.
