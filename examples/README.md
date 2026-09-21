# Getting started

[probe/](probe/) is a working consumer of Nightseam: one family declared in
three JSON files, its generated packages checked in beside them, a Go server
and a TypeScript client that speak it over a WebSocket. It is the repository
README's three code blocks, made to run.

It is a **consumer checkout**, not part of this repository's workspace: it
depends on `@nightseam/duplex`, `@nightseam/runtime` and `@nightseam/live`
at the released version and on `github.com/Bitspark/nightseam` at the same
one, with no `workspace:*` link and no `replace` standing in for either. So
it resolves what is published and nothing else — which is the point of it,
and why `scripts/smoke-packed.mjs` installs it from the packed tarballs
before every release and `scripts/smoke-registry.mjs` installs it from npm
and the module proxy after every tag. The commands below are the ones those
two run.

## Take it

Copy it out of this repository — it is meant to be the first commit of
something of yours, not a directory to work in here:

```
cp -r examples/probe ~/probe && cd ~/probe
```

## Run it

```
pnpm install                # duplex, runtime and live, from npm
go mod download all         # the runtime, and the generator this module names as a tool
go run ./server             # ws://127.0.0.1:8080/probe
```

and, in another shell:

```
pnpm start
```

```
echo    -> olleh
changed -> hello
notice  -> demo
stopped -> demo done
```

Four lines, and between them the three levels. The first two are the
profile: `pnpm start` dialled the server and called `echo`; the server
emitted `changed` and then — inside the request it was still serving —
called the client's `reverse` back, which is what `olleh` is. The last two
are the live tier: the client called `watch` with a `notice` function it
wrote as an ordinary function, and was handed back a `stop` function the
same way; the server called `notice` inside `watch`, and called it again
from inside `stop`, after the request that carried it had returned. Neither
side wrote an envelope, correlated a response to its request, exported a
reference, or checked a frame against the declaration: that is the
generated packages and the runtimes beneath them. Both installation smokes
hold these four lines, whole.

`pnpm check` type-checks the client and the generated package against the
declarations the published packages ship.

Use the example from a published release tag: the development branch may
already require a version that is not on the registries. From a clone of
this repository, `node scripts/smoke-packed.mjs` runs every command above
against what a release *would* publish.

## What is where

| | |
| --- | --- |
| `api/contracts/probe/` | the family: `model.json` for its types and `protocol.json` for its two sides |
| `api/go/`, `api/ts/`, `api/spec/` | what the generator renders, committed so that a reader sees it without running anything and `nightseam check` holds it |
| `api/impl/probe/handler.go` | the server's behavior, where `nightseam init probe` wrote it once and will never write again |
| `server/main.go` | an HTTP host that prepares a peer and scope, constructs `binding.ToWire`, and forwards the peer's Wire to it |
| `client/src/main.ts` | `prepareFromWire` before dialing, then an identity check and a model bound with reverse methods and events |

Generated code is never edited by hand. Behavior goes in files of your own,
against the interfaces the generated packages declare — `api/impl` here,
because that is where `nightseam init` puts it. `init` writes a TypeScript
stub for the client's side of the family too; this example supplies those
methods and events inline to the factory returned by the preparation's
`complete()` after dialing. Preparation holds early delivery until that factory
is bound. The model
uses the generated methods; host code owns the physical peer and closes it.
Live value interpretation is supplied explicitly with `valueEnvironment(scope)`.

There is no `go.sum`: this example ships inside the repository that publishes
the module it requires, and the hash of a version nobody has tagged yet is
not knowable. `go mod download all` writes one in your copy, and it belongs
in your first commit.

## Change it

Add a field, a method or an event to `api/contracts/probe/`, and render it:

```
go tool nightseam validate      # every diagnostic of the family
go tool nightseam generate      # rewrite what is stale
go tool nightseam check         # in your CI: fail if the checked-in output is stale
```

`generate` rewrites `api/go`, `api/ts` and `api/spec` and touches nothing
else; the compiler then says where your own code has to follow, in both
languages. [docs/declaration/families.md](../docs/declaration/families.md)
is what may be declared, [docs/declaration/generator.md](../docs/declaration/generator.md)
what is rendered from it, and [docs/declaration/generated.md](../docs/declaration/generated.md)
what comes out.
