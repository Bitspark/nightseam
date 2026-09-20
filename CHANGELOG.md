# Changelog

Versions move in lockstep: the published TypeScript packages, the version the
generator writes into a generated client's manifest, and the Go module's tag
are one number. Entries are in the words of the commits that landed them.

## Unreleased

### Clarified

- Live lifetime documentation distinguishes calls, bindings, aliases, records
  and connections; release invalidates aliases without counting owners or
  acknowledging remote receipt, forwarding retains its origin dependency, and
  application resume data supplies no automatic binding revival.
- Generated-role coverage distinguishes Go server bindings from TypeScript
  clients, accounts for every TypeScript generator skip, and records the
  unresolved tier-1 release-policy gap without changing the gate.

### Added

- Live owners give generated plain functions a caller-selected lifetime in Go
  and TypeScript. Owners nest, own fresh exports and import attachments, borrow
  existing aliases, and release idempotently without closing the connection.
  Generated operations accept an owner and handlers receive a child they may
  retain; import and export batches unwind only their own fresh allocations.
  Acquisition moves from scopes to owners, and intrinsically live generic
  converters take the active owner on both imports and exports.

- A shared generated forwarding proof carries higher-order functions through
  Go and TypeScript intermediaries over two socket connections, checks retained
  results and four registry counts, and distinguishes typed conversion from
  raw forwarding and independent binding release.

- A read-only issue-plan audit reports inconsistent lane fields, Epic parents
  and prerequisite links from GitHub or recorded fixtures, with a manual CI
  report. Issue forms require the lane metadata and retain paired-runtime work.

- The declaration proof lists the selected wire and native API commitments,
  their executable evidence and the limits of bounded example synthesis.
- Generic data containers can carry live values when applied in the live tier.
  Both generators pass parameter converters through records, unions, aliases
  and nested imported applications; the data container needs no live runtime.
  Generic callables remain refused and callable contract identities stay nominal.
- The generated-surface reference documents generic boundary converters and
  validation bindings in both languages. Generic-callable and family-draw
  diagnostics identify the distinct support gaps that still prevent them.
- Document examples carry visible concrete type and family bindings, or an
  explicit synthesis-limit reason. A derived table accounts for every corpus
  value, union arm and operation frame; both runtime validators check the
  same bytes and bindings. Markdown and the atlas show unavailable examples
  without inventing JSON, and synthesis bounds recursive and large values.

- The **live tier**, `live.json`, and the `callable` kind: a declaration whose
  values are not data but implementations the other side of a connection can
  invoke. A callable declares a `request`, a `result` and the `errors` it may
  return, each optional; it is named and referred to by name, never written
  inline, since a reference carries the identity of the declaration it
  implements. An interface is a record of callable members, and no service,
  stream, cell or topic kind is introduced. `live.json` carries `server` and
  `client` sides that add operations to the surface the protocol tier
  produces.
- Liveness is **derived, not declared**: the least fixed point of "contains a
  callable" over the declarations, through containers, imports and inline
  shapes. An entity key reference is never live, and a draw through a family
  parameter is decided where it is written. The direction rule every tier
  already had then keeps data and RPC independent — a checkout with no
  `live.json` renders exactly what it rendered before and imports no live
  package — and a declaration or operation in `live.json` that carries no
  callable is refused, so the tier cannot become the place things drift to.
- A callable's contract identity is **nominal**, `family/Type`, computed once
  in rendering and stamped into the wire description both runtimes read. Each
  validator refuses a reference carrying another contract where one is
  expected, even of the same shape, and checks nothing else: it resolves no
  binding, registers nothing and reaches no network. Native function assignment
  remains structural; the identity contains no signature or revision and does
  not prove compatibility after a declaration changes.
- Generated Go and TypeScript render a callable as a **plain function value**,
  so a record of callables is a record whose members are functions and each
  member is its own binding. Go's `ExportX`/`ImportX` and TypeScript's
  `exportX`/`importX` convert at the boundary against `live/go` and
  `@nightseam/live`. Generated Go clients and bindings and TypeScript clients
  install the connection's scope. A Go live type's own `MarshalJSON` refuses
  because converting native functions requires that scope.
- A consumer operation under a layer's reserved prefix is refused, read off
  the built-in families that speak on the wire rather than written out: it
  covers `channel.` and `live.` by one rule.
- The packed-install and registry smokes import every published package from
  outside the workspace, at every entry point its `publishConfig.exports` declares:
  type-checked with library checking on, then loaded by Node. A package the
  getting-started example does not itself import is no longer packed,
  installed and never opened.
- `scripts/first-publish.mjs` asks the public registry which of the published
  names it has never served. The release workflow runs it before the install,
  on a rehearsal as well as on a tag, and on a tag refuses the run when a new
  name has no credential to make its first publish — npm attaches a trusted
  publisher to a package that already exists, and `pnpm -r publish` reaches a
  new name after the tag is pushed.
- `live/go` and `@nightseam/live`: callable values across one connection. A
  scope over a peer, bindings exported from it, references that name them
  inside an ordinary payload, imports that share one dispatch per binding,
  release, forwarding and bounds. The layer speaks `live.invoke` and
  `live.release` as ordinary frames of the profile under a reserved `live.`
  prefix, so it adds nothing to the envelope and needs no tunnel. Native
  references belong to a scope; serialized bytes naming a live binding can be
  decoded and imported again on its original connection. After reconnection,
  lookup refuses stale binding IDs; fresh scope nonces prevent them from
  naming an unrelated binding. Release invalidates all aliases locally and
  sends an unacknowledged event; already dispatched work may finish. Scope
  closure settles calls without undoing their effects, and invocation
  cancellation releases no binding.
- A `live` conformance profile runs Go and TypeScript in both peer roles over
  real sockets. Registry-count assertions hold resource lifetime alongside
  invocation results, closure, release, cancellation and reference checks.

### Changed

- Callable documentation separates named declarations from nominal wire
  identity and states the limits of native function assignment and contract
  evolution, held by compiled Go and TypeScript boundary examples.

- The full type-language proof joins the shared family corpus, with compiled
  generated cross-wire scenarios, a mixed family/type binding diagram, and
  measured inline-name churn in Go and TypeScript.

- Composition guidance ties its stream, cell and topic claims to the behavior
  the examples actually exercise, and separates that behavior from shared peer
  scheduling, authority policy and guarantees a reusable composition must add.

- Language targets supply their own names, declarations and invocation
  snippets to the document through `spi.Speller`. Markdown shows the
  enabled languages beside types and operations; declarations and handler
  signatures come from generated sources, and every call snippet is held
  to the language's compiler.
- The generator writes a self-contained HTML atlas at `api/spec/index.html`,
  with an exchange board, wire frames, annotated types, search and family
  comparisons. Checkout config sets its title, design tokens and optional
  webfonts; the default page uses local fonts and makes no network requests.
- The specification is a document the generator builds once from the
  declaration, `internal/doc`, and writers render: every type with an
  example value of it and where it is used, every operation as the frames
  of the profile carry it — the request, the response, a refusal per
  declared error, an event's frame — and how much each side says, all
  synthesized deterministically. A writer implements `doc.Writer` and is a
  target by `doc.Target`, declaring its unit, pages of a family or of the
  checkout as a whole; the `spec` target became `markdown`, the first
  writer, at the layout it had and rendering every settled form as it did,
  its pages gaining the examples and the frames. The checkout is a unit a
  target may render, family `""`, rendered whole or not at all on every
  run and never a leftover. The checkout has a config of its own,
  `api/contracts/nightseam.json`: `disabled`, which the kernel reads, and
  under `targets` a section per target, carried raw and decoded and
  validated by the target, refused where it names no composed target, a
  member the config lacks, or what the tool's flags set; a problem of it
  is the checkout's own diagnostic, which `validate` now prints first and
  which refuses `generate`, `check` and `init`.
- Both runtime validators read literals, nullable and inline shapes, scoped
  type and family applications, explicit generic inheritance and adjacent
  union payloads. Family descriptors retain parameter and import ownership;
  generated Go packages expose `WireSchema()`, and `TypeArgument[T]()` binds
  instantiated Go types without losing named constraints. Both runtimes use
  the ruled Unicode pattern semantics, held by shared values and Node
  differential checks, including large counts and surrogate escapes.
- The Go target renders the settled type language: concrete unions with
  adjacent payloads and widening/narrowing helpers, literals, nullable and
  inline shapes, mixed type and family parameters, and inherited sides.
  Generated codecs retain the supplied types' validation constraints;
  model-only families generate their types without protocol helpers.

- The TypeScript target renders the settled type language: adjacent unions,
  nullable and literal expressions, inline shapes, type and family parameters,
  nested applications and explicitly bound inheritance. Generated clients
  retain imported operation names and validate both parameter kinds through
  scoped runtime bindings. Model-only families emit types and a validator;
  the proof family and built-ins compile, and executable fixtures hold
  inherited calls, events, reverse calls and lossless union payloads.

- Wire and declaration strings contain Unicode scalar values. Both runtimes
  reject malformed Unicode before decoding or publishing can replace it,
  including nested JSON, names, descriptors and custom encodings. Generated
  Go codecs and peers over raw connections use the same guards. Valid
  surrogate pairs and ordinary U+FFFD stay unchanged.

### Removed

- The governed session layer, whole: the `session` tier and its `decides`,
  `asks` and `conversation` declarations; controller, participant and
  observer roles, holder selection, attention and question handover; the
  fixed upstream/consumer topology and the lifecycle it imposed; the
  `session/go` and `@nightseam/session` packages with their registry,
  attachments, log and replay; the built-in `session` family, the reserved
  `session.` namespace and the generated `Decides`, `Asks`, `Conversation`,
  cursor and control helpers; the session observer events and their OTel
  spans; and the `session` conformance profile with its scenarios, driver
  ops and testee halves. Nightseam has no released consumer, so this is a
  clean break with no shim and no migration; the replacement live layer is
  described in [the live runtime guide](docs/runtime/live.md). The decision
  records of the removed layer are kept and marked superseded.
- The `holder` corpus family, which drew a declared type through an unbound
  family parameter. The rule it exercised — every family that may bind a
  parameter declares what is drawn through it — is unchanged and still held by
  `internal/check`. The replacement `relay` family exercises a live-tier
  family parameter by drawing the carried `Envelope` and `Handle` data types.

- `after`, the session's resume cursor: from `Tunnel.Open` and
  `Channel.After`, from `channel.open` and the `channel.opened` and
  `channel.accepted` observer events, in both languages, and from the
  built-in `tunnel` declaration, the driver and the tables. It existed only
  so an attaching consumer could resume a governed log, and the tunnel never
  read it — it forwarded it to whatever served the channel. Nothing in data,
  RPC or live reads it; a layer that needs to resume states its own cursor
  when it attaches rather than taking one off the channel it attached over.

### Fixed

- Live reference documentation and paired tests distinguish native scope
  association from caller-supplied serialized bytes: valid bytes can be reused
  on their original connection, while stale bindings fail lookup after reconnect.
- TypeScript live invocations preserve already-aborted caller signals before
  dispatch, returning cancellation without invoking the exported function or
  releasing its binding; a subsequent fresh invocation remains usable.
- Generated live exports reclaim only their own unpublished bindings when
  conversion, validation or serialization fails, including nested generic
  values and callable requests and replies. Both runtimes provide a synchronous
  export construction boundary; completed exports keep their existing lifetime.
- Closing a live scope settles incoming and local self-reference calls as well
  as outgoing calls, even when their implementations ignore cancellation.
  The underlying peer remains usable; late results cannot replace closure.
- Both installation smokes require the complete data/RPC/live example exchange,
  including the registry smoke's callback and returned-function results, and
  reject incorrect values that merely begin with the expected output.
- Generated live conformance waits for the server's attachment before reverse
  calls, within the driver's deadline, instead of racing a successful dial.
- Generated higher-order callables convert their own request and result through
  the live scope in both languages, so a callable may take or return another
  callable without losing its binding during JSON encoding.
- Worktree cleanup removes empty unregistered leftover directories and their
  branches, while preserving nonempty unregistered directories.
- Carried built-ins used as union payloads remain local to their family in
  generated Go and TypeScript, without imports of nonexistent built-in packages.
- Documentation examples honor array lengths and explicit family bindings,
  and synthesize checked witnesses for supported patterned strings.
- Registry smoke consumers have their own pnpm workspace boundary, so an
  install beneath an existing workspace leaves its manifest, settings and
  lockfile unchanged while retaining the release's dependency declarations.
- Release verification waits up to thirty minutes for registry propagation,
  with retry backoff capped at thirty seconds, so cached Go proxy misses
  have time to expire after a tag is published.
- `nightseam init` skips Go handler scaffolds for model-only families,
  which have no generated binding package to implement.

## 0.4.0 - 2026-09-19

This release makes the consumer improvements landed since 0.3.0 available
without waiting for the complete declaration-language roadmap. Go and
TypeScript remain the reference runtimes. The declaration checker and
specification renderer understand the new type language; complete code
generation and paired value validation for those forms remain in
[milestone 0.5.0](https://github.com/Bitspark/nightseam/milestone/6).

### Upgrading from 0.3.0

- Go's `session.New` now returns `(*Registry, error)`. Handle the error at
  construction; negative attachment, inflight and send-timeout limits are
  refused with `invalid_options`, while zero still selects the defaults.
- Regenerate clients with the matching generator. Supply typed event
  handlers at construction to observe an immediate replay from its first
  event; later handler registration remains available.
- Generated TypeScript sibling packages use relative `file:` dependencies
  by default. `--ts-sibling` selects `file`, `workspace` or `version`
  consistently for generation and stale-output checks.
- Checkout families cannot use the reserved built-in names `duplex`,
  `tunnel` or `session`. Rename such a family and its references. The
  checker also refuses invalid or incomplete generic bindings that older
  versions accepted.

### Consumer improvements

- Session binding uses an optional log head lookup in both languages,
  avoiding a full read when a durable log already knows its last sequence.
  Memory logs provide it; a failed lookup cannot start a session at zero.
- Attachments expose completion through Go `Done()` and TypeScript `done`,
  including session termination, so callers can forget an attachment
  without observing every registry change.
- Generated Go and TypeScript clients install typed event handlers before
  reading their first frame, preserving events at the start of a replay.
  Go preparation hooks and later registration remain available.
- Replay delivers the machine's events to consumers, without replaying
  other consumers' requests, unrelated replies or pending asks into a live
  peer. A replay ending in skipped or truncated entries sends its final
  log cursor so that resumption does not repeatedly scan those entries.
- Request observers report `request_timeout` for local deadlines and
  `cancelled` for local cancellation in both directions, and observe an
  ending before sending its best-effort cancellation frame. TypeScript
  treats a handler's public `cancelled` refusal as an error, matching Go.
- TypeScript event names that collide with inherited Object members are
  refused before generation; a name override preserves the wire name.
  A send on a closed TypeScript tunnel channel reports `disconnected`.

### Declaration and specification foundations

- The declaration model, schemas and checker support unions, literals,
  nullable expressions, parameters on families and types, applications,
  inline shapes and inherited protocol sides. Inline names are derived
  deterministically, and generic inheritance requires explicit bindings.
- Union declarations use an adjacent tag and complete payload under the
  declared value member, including record payloads. A payload-free arm has
  the tag alone. This replaces the earlier flat-record proposal; payload
  keys cannot collide with the union envelope.
- Shared rendering facts retain declaration identity, lexical ownership,
  applied arguments, inherited union variants, operations and governance.
  Checker fixes cover forwarded family arguments, imported generic bounds,
  captured parameters, conflicting inherited governance and ambiguous tags.
- Profile and tier declarations live as built-in families. Carried profile
  types use ordinary reference resolution, and the envelope table is held
  to the built-in duplex declaration.
- The specification target renders the settled forms and standalone
  references for duplex, tunnel and session, held to their embedded
  declarations. Applied inheritance, substituted expressions and inherited
  references retain their source ownership.
- The Go and TypeScript targets still refuse new forms they do not render,
  with `unrendered_form` diagnostics. Full new-language value validation,
  code generation, cross-wire proof and implicit typed session operations
  are not claimed by this release. Generated session control callbacks,
  retained cursors and subscription operations continue in 0.5.0.

### Validation, documentation and release reliability

- TypeScript checks cover every source, test and conformance helper under
  shared compiler settings. Prettier formatting is enforced, and internal
  helpers no longer widen published surfaces.
- Timing-sensitive tests use observable completion and controlled pacing;
  observer polling preserves history until an expectation matches. The
  two conformance testees report malformed log prefill consistently.
- Documentation is organized into wire, runtime, declaration, language,
  decision and goal pages. The link gate checks tracked Markdown links and
  rejects malformed UTF-8 instead of silently replacing invalid bytes.
- Every change lands through an isolated worktree and squash pull request;
  CI checks the live branch rules and merge settings against their file,
  checks issue scope, and reruns that scope check when a PR body changes.
- Packed-consumer checks require built entry points, declarations, README,
  LICENSE and NOTICE. Rehearsals use distinct Go versions and isolated
  module caches, and published tags cannot be moved to another tree.
- The registry round trip waits for npm and Go module propagation with
  backoff before installing and can file an issue when verification fails.
- The document/writer model, language code examples, HTML atlas and
  validated documentation examples continue in 0.5.0. Pilot languages are
  planned for [0.6.0](https://github.com/Bitspark/nightseam/milestone/5).

## 0.3.0

### Added

- The conformance suite, `conformance/`: one Go runner drives a **testee** per
  language — a peer under remote control, speaking `DRIVER.md`'s JSON lines —
  through scenarios that are data, over a real socket, and holds every
  language to Go's on both sides of the wire. The Go and TypeScript testees
  serve the seam, the peer, the tunnel, the session and the generated
  packages; the six interop gates the runtimes carried became scenarios and
  were retired; the suite's first two catches were a TypeScript observer
  told less than Go's and a TypeScript session log that disagreed with Go's.
- Profiles and tiers, `docs/tiers.md` and `conformance/profiles.json`: the
  four promises a language can make, the five profiles the suite holds a
  language to, and the four tiers that say which a language guarantees and
  which red cell stops a release. The runner places every scenario in a
  profile, holds a testee to its tier, and writes `conformance/matrix.json`;
  the README renders it as the Languages table and CI holds the table to the
  matrix; the release refuses a tag whose matrix the tier table stops; the
  full matrix runs nightly and a failure becomes an issue against the
  scenario. Go and TypeScript are tier 1; Python, Rust, C++ and Haskell are
  the pilot languages, planned for tier 2 and pushed to every profile first
  — C++ and Haskell are in because they are the hardest — and Java and
  Swift follow at 4.
- A dial-only language is held in every role: a scenario no longer says who
  opens the socket but writes `pair.conns`, `pair.peers` or
  `pair.peer_and_conn` on the runner, which expands each from what the
  testees answered `hello` with — the natural side listening when it can,
  otherwise the other side listening a raw connection over which each takes
  a peer of its role. What a scenario needs is held per side, derived from
  each side's steps; `TestDialOnly` runs the star with TypeScript treated as
  unable to listen and allows no skip but that one; every run builds and
  renders under a directory of its own, since two runs may share one
  checkout; and `matrix.json` is written only by a run that held every
  profile for every language.
- A getting-started a consumer can run, and the release's own smoke: one
  family under `examples/probe` — three tier files, the generated Go,
  TypeScript and Markdown packages committed beside them, the server's
  behavior where `nightseam init` writes it, served through
  `binding.NewHandler`, and a TypeScript client that answers the reverse
  call the server makes inside the request it is serving and hears the
  event it emitted — the README's three blocks made to run. It carries no
  `workspace:*` and no `replace`: it names the packages at the released
  version and resolves against nothing in the tree, which is what lets
  `scripts/smoke-packed.mjs` pack every published package, copy the example
  out of the workspace and install it from the tarballs and a `file://`
  module proxy before a tag exists, and `scripts/smoke-registry.mjs` install
  it from npm and the module proxy after one — the release workflow runs
  the first before it publishes and the second after, and the second's
  failure opens an issue naming the tag.
- A consumer of a session is told who holds control and where it stands:
  the relay speaks the session's own vocabulary on the wire as ordinary
  events under the prefix the session tier reserves — `session.control`
  `{"holder": "<origin>"|null}` to every attachment when control is given,
  released or transferred and once to a consumer on attach before its
  replay, and `session.cursor` `{"sequence": N}` to the one attachment a
  frame was delivered to, straight after it, naming the log's own sequence
  so that a cursor never moves backwards. Neither is logged, a session's own
  frames being state and not messages of it; a machine that sends a
  `session.` frame is refused. The attachment exposes `Holder()` /
  `holder`, `OnControl` / `onControl` and `Sequence()` / `sequence` in both
  languages; the typed generated surface follows in 0.4.0.
- Go dials with a deadline: `ConnectTimeout` on `DialOptions`, the twin of
  TypeScript's `connectTimeoutMs`, thirty seconds where zero, refusing a
  handshake that outlasts it with `connect_timeout` as a `*PublicError`.
- `docs/layers.md`, the test for where something new on the wire belongs:
  the profile's only when the peer acts on it; a layer's own vocabulary as
  ordinary frames under a reserved prefix when one layer produces it and
  another reads it; a header when it is a consumer's fact about a call — and
  why a header does not propagate: delivering is the peer's one action on
  it, and a header is a fact about this call, not about the calls a handler
  makes in answering it.
- `meta`, the one header the profile has: a flat string map a request or an
  event may carry about the call rather than in it, delivered to the handler
  beside the payload and read into by nothing, never on a response, never in
  an observer event, `nightseam.`-prefixed keys reserved and refused. Both
  peers, both validators, the generated client and binding of both languages
  pass it through; the session relay forwards it verbatim. The suite holds
  the carriage across the wire and through the relay — both testees take a
  `meta` argument on `peer.call` and `peer.emit` — and the log is held to
  keeping a carriage as it keeps a payload.
- The WebSocket transport negotiates a subprotocol: `Subprotocols` on both
  sides, `SelectSubprotocol` on the server for a browser's ticket, what was
  selected readable on the peer, none offered or selected by default.
- A session binds and attaches over any connection of the seam — `Bind` and
  `Attach` take `duplex.Conn` / `FrameConnection`, a tunnel channel being one
  — so an in-process machine binds over a pipe; a registry-level observer
  serves an up side that runs over no peer.
- A durable log is bound at its head: `Bind` seats the relay's cursor by
  reading the log before the pump starts, so a consumer attaching to a
  session bound over a log that already holds frames is replayed them.
- The OpenTelemetry adapter, the one component a consumer opts into:
  `@nightseam/otel` at `otel/ts` and `github.com/Bitspark/nightseam/otel/go`
  at `otel/go`, a package and a Go module of their own so that the four
  components keep the dependency-freedom they publish and a consumer who
  chooses no backend installs nothing for one. Each binds the two hooks the
  runtime leaves open: a propagator over an OpenTelemetry text-map
  propagator — the W3C trace context one by default — whose every injection
  is a traceparent a peer accepts, minting one where OpenTelemetry has
  nothing to say; and an observer that opens a server span where a request
  came in and a client span where one went out, makes the connection a span
  and each event emitted or delivered a span of no duration, and records
  everything else the three layers tell as a span event, carrying only the
  fields that are a name, a count, a flag, a duration or a trace — so that no
  payload reaches a backend by this path either.
  `docs/observability.md` says what a trace of one
  call through a relay looks like.
- The Go module at `otel/go` is released by a second tag, `otel/go/vX.Y.Z`,
  cut after the root tag it requires; `RELEASING.md` says in what order, and
  `.github/dependabot.yml` names it, a `gomod` entry covering one directory
  and never a module nested in it.
- `nightseam version` prints the module version of the running tool — what a
  consumer's `go get -tool` resolved, or `(devel)` from a checkout, a build of
  a tree being exactly what that answer should say — and `nightseam --version`
  prints the same line. A tool built inside a consumer's own module is not
  that module's main package, so what is reported is the version the
  consumer's requirement resolved to rather than the consumer's own, and a
  `replace` pointing at a checkout reports `(devel)`. The generated files name
  no version and keep the header they have: one in it would rewrite every
  generated file of every consumer on every release, which the golden
  discipline would feel immediately, and would fail a `check` over output that
  is otherwise byte-identical — the failure this command exists to explain
  rather than one to add. `docs/generator.md` states the
  split.

### Changed

- No legacy: with no released consumer, a change is made directly and whole
  — both peers and the validators and the generator in one lane, emitting
  what they accept in the same commit, every caller changed in the same
  commit — and a design is chosen for being right, never for being cheap to
  roll out; `COLLABORATION.md` says so. It also gains the design issue — a
  question whose answer is the operator's to give, in a Design issue form
  under the `design` label, holding what exists today, the options lettered
  with what each costs, a recommendation marked as whose it is, and the
  verdict copied onto it verbatim before a lane is cut from it — and says
  which of the two ways to commit only one's own work applies when.
- One backpressure rule and one bound on outstanding calls, where the two
  peers had disagreed and `docs/profile.md` had described one of them as
  both: every queue is paced for one write deadline before its consumer is
  called stalled, in both runtimes and on both sides of the connection,
  because a burst that would drain in a second should not end a connection
  — the TypeScript peer paces its outgoing queue where it failed at once
  with `busy`, the Go peer paces its inbound event queue where it yielded
  once and gave up. Pacing an inbound queue is pacing the remote, so
  responses and cancellations on that connection wait behind a full event
  queue, which is what the deadline bounds. Go gains `MaxPendingRequests`,
  default 128, refusing the call past it with `busy` where it stands — no
  frame, no request an observer is told of — as TypeScript always has. The
  *Limits and backpressure* section is rewritten to the one rule, and three
  scenarios under `conformance/scenarios/peer` hold it with either language
  in either seat.
- The composition root is a package: the targets, their configs and their
  order live in `internal/compose`, which `cmd/nightseam` and the
  conformance runner both import, so that the suite renders with exactly the
  tool's targets — it had composed a kernel of its own and drifted to two
  targets where the tool has three, and would have gone on validating output
  the tool no longer produces with nothing saying so. `TestImportDirection`
  refuses a target named anywhere else.
- Nothing in the tree is named for a version that was never published: the
  schema identifiers are `urn:nightseam:v1:*`, the first public version of
  the declaration language, where they said `v2` of a first shape the tier
  files replaced before anyone consumed it; `cmd/nightseam`'s composition
  file and its fixtures are named for the tool rather than for a count.
- One spelling per limit across the two runtimes: TypeScript's
  `maxIncomingRequests` is `maxConcurrentHandlers` and `maxQueuedMessages`
  is `queueCapacity`, the Go names being the reading since they say what is
  bounded; defaults unchanged at 64 and 128; `docs/profile.md` tables the
  limits by name in both languages.
- `runtime/go`'s `TypeExpression` is `MustTypeExpression`, named for the
  panic it answers an expression it cannot read with, as `MustSchema` beside
  it is; every generated protocol package re-exports it under that name,
  its only caller being generated code passing a constant the generator
  wrote.
- What npm shows of the five packages is written for the reader who has npm
  open: each README opens with its install line and links into the
  repository where it named bare paths; each manifest carries an author and
  exports `./package.json`; declaration maps are no longer emitted, every
  one having pointed at a `src` the tarballs do not hold; each build empties
  `dist` first. `otel/go` has the README its `go get` was without;
  `.github/CODEOWNERS` routes review to the maintainer.
- The release workflow runs in the `release` environment, so that every run
  — a tag's or a rehearsal's — waits for its required reviewer; the first
  publish of each package name is made with a one-day granular token on the
  `@nightseam` scope, and once the five exist each names the workflow in
  that environment as its trusted publisher and the token is deleted.
- Every exported name a consumer meets first carries its doc comment, in
  `runtime/go` and in the published TypeScript packages — what pkg.go.dev
  and a `.d.ts` show a reader before anything else.
- The root module's Go directive and the nested module's move together;
  `otel/go` declares 1.26.0 as its dependencies require.
- The npm publish attaches provenance: the release workflow mints a
  short-lived OIDC token, so each package's npm page names the workflow run,
  the commit and the repository its tarball was built from. It refuses to
  publish without it, and the same workflow runs on demand without publishing
  anything — a rehearsal, so that a misconfiguration fails a run somebody
  asked for rather than one that follows a tag which cannot be taken back.

### Fixed

- A handler installed on an accepted peer cannot miss the other side's first
  request: `runtime.Options.Prepare` runs on the peer after its handlers and
  events are installed and before it reads a frame, so `Handle`, `HandleEvent`
  and a `tunnel.New` over the peer are there before anything can arrive. The
  Go peer started reading inside `newPeer` and `Accept` called `OnConnect`
  after that, so a client that opened a channel the moment it saw the `101` —
  the first act of a consumer that came for a session, and the pattern
  `docs/tunnel.md` taught — could be refused `channel_refused` with
  `method_not_found` beneath it; a server whose install takes a millisecond
  lost 868 of 1000 opens that way and loses none now. `ServerOptions` and
  `DialOptions` reach the hook through their embedded `Options`, so both
  constructors have it and neither grew a member; a `Prepare` that fails
  fails the construction, and a socket whose handshake was already answered
  is closed with 1008 rather than left open in silence. `OnConnect` is
  unchanged and means what it meant — the peer is live — and the documents
  say which is for what: install in one, use in the other. TypeScript had the
  order already, a peer being made before it is attached, and its README says
  that this is the rule.

- The seam has a conformance suite in TypeScript too, and three transports run
  it: `duplex/ts/src/conformance.ts`, the twin of `duplex/go/duplextest`, run
  by the in-memory pipe and the WebSocket adapter in `duplex/ts` and by a
  channel in `tunnel/ts`, as the Go suite is run by `duplex/go`,
  `duplex/go/ws` and `tunnel/go`. `@nightseam/duplex` had no test file and no
  test script at all and so published untested, while `pnpm -r test` passed
  over it in silence — a package with no script looking exactly like one whose
  tests pass. The suite holds what a `FrameConnection` promises and nothing of
  the transport keeping it: frames in order and whole either way, every
  listener given every one of them and detaching one leaving the others, a
  close carrying its code and its reason to both sides and refusing what is
  sent after it, and `buffered` counting what a send left behind and nothing
  once the transport took it. Two promises of the Go suite are not the seam's
  in this language and the suite says so rather than asking for them: a
  receive limit, which is the peer's option and the tunnel's here and refused
  with 1009 by each, and an abort, a close being how a connection ends. The
  WebSocket adapter is held to its own mapping besides — the three shapes a
  socket delivers a message in, a Blob read as bytes with what followed it
  waiting behind it, `binaryType`, a close event with no code as the
  registry's 1005, and the detach that follows a close.
  `TestEveryPublishedPackageIsTested` now holds every published package to
  having a test script and to running every test file it has, so that the
  parity rule of COLLABORATION.md is run and not only read.

- An observer that gives up gives up alone in Go, as it already did in
  TypeScript: `Observe` is called on whichever goroutine the traffic ran on —
  the reader, a handler, a caller — and a panic there ended the connection and
  the process with it, where the TypeScript peer had always caught a throw and
  carried on. The Go peer now calls its observer from one place and recovers
  there: that event is lost and nothing else is. The rule is written down for
  both languages, in the `Observer` of each runtime and in
  `docs/observability.md`.
- A receiver's own deadline answers `cancelled` on the wire in both runtimes:
  when a handler's deadline passed, the Go peer answered the caller
  `cancelled` and the TypeScript peer `request_timeout`, so the code a caller
  saw for one fact depended on which language served it. TypeScript now
  answers what the profile's error table has always said; `request_timeout`
  stays a caller's own error for its own deadline, never a frame anyone
  receives, and the *Requests* section of `docs/profile.md` says so.
- An observer's events are one order per peer, and the writer is the one
  place a send is told: the Go peer observed a frame sent after handing it
  to the writer, so a reply's `frame.received` could precede its request's
  `frame.sent` in an array the suite holds to one order; the send is now
  told by the goroutine that writes, immediately before the bytes leave, and
  the TypeScript peer moves the same way — it told the observer where it
  queued the frame, so a queue holding two had told both before either
  reached the transport. `docs/observability.md` gains an *Order* section
  stating the promise, its cost — one serialization point per direction —
  and what it does not promise; and its trace-and-family rule, wrong about
  two of the three layers where both runtimes had agreed all along, now says
  that every event names the thing its layer is about while an event
  concerning a frame carries that frame's trace.
- The TypeScript tunnel holds the receive window its Go twin holds: a
  channel kept what nobody had listened for in an array with no bound, so a
  sender that ignored the credit it was granted was served rather than
  refused and what a channel could take was the remote's to choose. A
  TypeScript channel now holds one window, the frame beyond it ends the
  channel with 1002, the two languages' reasons match word for word, and a
  channel refused while nobody was listening keeps the close for its first
  listener. Held by a case in each language and by a tunnel scenario that
  writes `channel.frame` on the peer the tunnel runs over, in both pairings.
- A consumer's server can tell a session's refusals apart in Go as it could
  in TypeScript: `session/go` returned twenty-three English sentences where
  `session/ts` throws a `DuplexError` with one of ten codes; the ten are
  constants of `session/go` too, name for name — `invalid_options`,
  `no_session`, `not_attached`, `not_controlling`, `origin_invalid`,
  `role_invalid`, `sequence_invalid`, `session_exists`, `session_invalid`,
  `too_many_attachments`, with `busy` beside them — and every refusal
  `Bind`, `Attach`, `Control` and the log return is a `*session.Error` that
  `errors.As` and `errors.Is` reach; `docs/session.md`'s table names the
  code on every row; a scenario holds the codes across the wire.
- A binary frame on a session's connection ends it as unsupported data,
  1003, in both languages, where TypeScript closed with 1008 and a reason
  about the message rather than the frame; `docs/profile.md` lists 1003
  among the close codes, and a session scenario reaches the case in both
  pairings.
- Text that is no message of the profile ends a session's connection with
  1002, a protocol error, in both languages and under the same reason, where
  TypeScript closed with 1008 and one sentence for every case. The reason
  names the fault as Go's decoder does — `a session frame must be a JSON
  object`, `duplicate session frame member "id"`, `invalid trailing session
  frame content` — and those three are the whole of the set: TypeScript now
  reads the member named twice and the content after the object that
  `JSON.parse` passes over, and Go gives one of the three where it gave
  `encoding/json`'s own words for a frame malformed inside, which no other
  runtime could say. `docs/session.md` states as the rule what it recorded
  as a divergence; both session suites send such text from a consumer, which
  ends that consumer alone, and from the machine, which ends the session,
  and a session scenario holds the code and the reason on both sides in both
  pairings.
- One refusal reads one way in both validators: the pointer names the
  member and the text states the fact — `$.count: required field missing`,
  `$.note: null is not permitted`, `$.zzz: unknown field` — where the
  TypeScript validator pointed at the parent and spelled each differently;
  `conformance/tables/validator.json` carries the message on its invalid
  rows and both runtimes are held to it. The two also read an absent
  `required` flag opposite ways, Go as optional and TypeScript as required,
  latent only because the generator always writes it; TypeScript reads it
  as Go does, held by rows with a field that carries no flag.
- The TypeScript pipe carries the backpressure that is the point of a pipe:
  the bound Go's has, eight frames a direction, and the `buffered` a peer
  paces on, where `pipe()` declared `buffered = 0` and delivered every send
  at once; held by the seam suite in TypeScript and a seam scenario that
  exhausts the bound in both pairings.
- A peer that refuses a frame closes with the code the profile names, in Go
  too, and its observer is told what the wire carried: `fail` aborted for a
  frame the profile does not admit exactly as it aborted for a transport that
  was already gone, so a malformed frame left the far side reading 1006 where
  TypeScript closed 4011, and the observer was told 4011 all the same — and a
  close a consumer chose was told 1000 where the wire had carried nothing.
  A protocol failure now does the close handshake with `duplex.CodeDuplex` and
  a reason, cut to what a close frame admits; `Close` closes with 1000; and a
  write that failed, a context that ended or a consumer that stalled still
  aborts, which is the one case `ConnectionClosed` reports as 1006.
  `peer.await_close` reports the code beside `clean`, and the malformed-frame
  scenario holds 4011 on the peer that refused and on the raw connection
  reading it, for every invalid row of the table and in both pairings. A
  machine that ends its connection by choice thereby tells a session's
  consumers the normal close it chose, where the relay used to pass on the
  1006 an abort had left behind.
- An error frame with an empty `message` is malformed in TypeScript as it is
  in Go, where `{"code":"denied","message":""}` ended the connection against a
  Go peer and resolved a call against a TypeScript one; `frames.json` gains a
  row for an empty message and one for an empty code, and each runtime's
  table-driven test now judges every row of the table the way its peer judges
  a frame it is handed — the envelope decoded and the id held to its prefix —
  where both read only the rows naming `meta`.
- A cancel is answered when the handler returns in TypeScript, as the profile
  says and the Go peer does: the receiver answered `cancelled` the moment the
  cancel arrived and dropped whatever the handler went on to return, so an
  observer's `request.ended`, a relay counting open requests and a handler
  slot's occupancy all moved with the asking rather than with the work. The
  cancel now aborts the handler's signal and answers nothing itself, and the
  handler's return is the one response — `cancelled` whatever it returned.
  Held by a scenario in which a handler that ignores its signal is still open
  while a call sent after the cancel is served and answered, and ends only
  after it, with the `hold` behaviour `DRIVER.md` now names.
- A TypeScript `emit` resolves when the frame was accepted for sending, which
  is queued, as the profile says and `Emit` returns in Go, where it waited for
  the transport to report its buffer drained: a sender over a connection that
  was not draining never reached the frame that meets a full queue, which is
  why the outgoing queue had no scenario and the profile's one backpressure
  rule was held on that side by each runtime's own tests alone. The drain and
  the write deadline continue behind the caller and still end a connection
  nothing drains; `runtime/ts`'s README no longer promises the drain. Held by
  the outgoing-queue scenario, paced over an in-process pair nothing reads and
  mirrored so each language is held over its own, and by a `peer.test.ts` case
  for the resolve point itself.
- Three things a release would have tripped on or told wrong: a test in the
  TypeScript target asserted the literal `0.2.0` that `scripts/version.mjs`
  never rewrites, which would have failed the tag workflow's own `go test`
  after the tag existed; `cmd/nightseam`'s package doc named the runtime's
  import path without its `/go`; and the README and RELEASING.md said the
  four runtime packages depend on nothing, true on npm and not in Go, where
  the WebSocket transport is a third-party module.
- A frame of no kind is refused with one sentinel, `duplex.ErrNoKind`, where
  three transports spelled it three ways; the registry's 1005 is
  `CodeNoStatus` in Go and an exported `NO_STATUS` in `@nightseam/duplex`;
  the three codes `channel.open` is refused with are constants of
  `@nightseam/tunnel` as they are of `tunnel/go`; the two runtimes' limit
  refusals say *must not be negative*, which is what they check.
- The documents say what the code does where an audit found them overtaken:
  `docs/layers.md` illustrated the session layer with an operation that
  never existed; `docs/language.md` numbered the tiers wrong and called the
  override files a tier; `docs/tiers.md` described `profiles.json` as globs
  and lists; CONTRIBUTING.md's first bullet had lost its dash; COLLABORATION.md
  sent a report to a form the templates do not have. `gofmt -l` is a gate in
  CI's fast job.

## 0.2.0

The first version to be published; nothing has been tagged yet. Everything below is in it.

### The declaration language and the generator

- A family is a directory of tier files under `api/contracts/<family>/`:
  `model.json`, `protocol.json`, `session.json`, and `go.json` and
  `typescript.json` for names the convention would spell otherwise. A
  declaration refers to its own tier or a lower one, never a higher one.
- A family declares the parameters it is generic in and a type draws on one
  — `S.Envelope`, `S.Handle`, `S.Payload` — and is rendered once,
  generically, in both languages; a parameter is bound where the generated
  code is instantiated, to any family that declares its role. A generic type
  of an imported family is applied explicitly, `{"apply": …, "with": …}`.
  The two ways to a concrete package — binding into the declaration, or
  rendering generically and instantiating — are held to agree by the
  fixtures, in Go by reflection and in TypeScript under `tsc`.
- `nightseam generate`, `check`, `validate` and `init`; the
  generated Go packages `-protocol`, `-binding` and `-client` and the
  TypeScript client; a `spec` target rendering each family's specification
  as Markdown; the public errors a family declares reaching both languages
  by name; the wire validator living in each runtime, held to one
  conformance table.
- A corpus of families with every target's output held as goldens, the
  exported Go surface held file for file, and one checkout per refused rule
  with its diagnostics.

### The runtimes

- `duplex`: the seam beneath every protocol — a frames duplex connection with
  an explicit close carrying a code and a reason — with the WebSocket
  transport, an in-memory pipe, and the conformance suite every transport is
  held to.
- `runtime`: the peer of the `nightseam.duplex/1` profile — requests,
  responses, events and cancellation, bounded queues and backpressure,
  presence types, the HTTP upgrade in Go, the wire validator — and W3C trace
  context carried on every kind of envelope and propagated: an incoming
  frame's trace reaches its handler's context, what the handler sends carries
  a child of it, a response its request's, with a propagator hook and a
  default that mints ids without a tracing library.
- `tunnel`: channels multiplexed over one peer, each a connection of the
  seam held to the same suite as the WebSocket and the pipe; per-channel
  credit; ids chosen by the opener, odd for the client and even for the
  server; the Go and TypeScript tunnels held to each other over a socket.
- `session`: a session as a thing that outlives connections — one up channel,
  any number of down channels in a role each, one holder of control, an ask
  routed to the holder and following a transfer, ids minted per session, a
  log replayed from a sequence before anything live — held to one suite in
  both languages.
- The generated code runs over any connection of the seam: `Attach` and
  `Serve` beside `Dial` and `NewHandler`, `Open` resolving a handle on a
  tunnel, `Client.attach` and `Client.open` in TypeScript.

### Observability

- One observer sees every layer. The peer takes an `Observer` of one method
  — `Options.Observer` in Go, `PeerOptions.observer` in TypeScript — and is
  told ten things about the traffic it carries: a connection opened and
  closed, a frame sent and received, a request started and ended with its
  duration and its outcome, an event emitted and delivered, backpressure, and
  a handler that threw. The tunnel adds five of its channels and the session
  ten of its conversation, emitted through the peer they run over rather than
  through an observer of their own, so a consumer chooses one and sees all
  three layers. A peer emits and never aggregates and chooses no backend, an
  absent observer costs nothing, and no event carries a payload — held by a
  sentinel in every payload a peer carries appearing in nothing any event
  renders.
- Every event that concerns a frame carries that frame's trace and the family
  of the name it concerns; the generated install labels each of a family's
  methods and events with the family, and a name nobody labelled has no
  family rather than a guessed one.
- Two adapters, one per language: `runtime/go/slogobserver` writes one `slog`
  record per event at the event's own instant, and `consoleObserver()` in
  `@nightseam/runtime` writes one line per event through four console methods
  a test can capture. Both write an event of a type they do not know.
- A layer of your own joins the same stream — in Go by implementing
  `ObserverEvent()`, in TypeScript by declaring into `ObserverEvents` so that
  a `switch (event.type)` stays exhaustive — and reaches the observer through
  `Channel.Peer()` in Go or `channel.observe(event)` in TypeScript.
- The session's registry reports the same ten facts as domain changes to a
  consumer that asks: `Registry.OnChange(fn)` in Go and `registry.onChange(fn)`
  in TypeScript, each handing back the stop that ends that registration alone,
  each change told before the frame it concerns is handed on.
