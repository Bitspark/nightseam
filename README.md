# Nightseam

[![ci](https://github.com/Bitspark/nightseam/actions/workflows/ci.yml/badge.svg)](https://github.com/Bitspark/nightseam/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/Bitspark/nightseam.svg)](https://pkg.go.dev/github.com/Bitspark/nightseam)
[![npm](https://img.shields.io/npm/v/@nightseam/runtime.svg)](https://www.npmjs.com/package/@nightseam/runtime)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Declare a duplex API once, in tiers of JSON. Get a typed client and a typed
server in Go and in TypeScript, both speaking one wire profile —
`nightseam.duplex/1`, JSON frames carrying requests, responses, events and
cancellation over a WebSocket, a tunnel channel or an in-memory pipe.

Duplex means both ends call. The server calls the client with the machinery
the client calls the server with, declared in the same file and typed the
same way — which is what a browser session, an agent and a relay need, and
what a request-and-response contract has no way to state.

### Declare it

`api/contracts/probe/protocol.json`

```json
{
  "profile": "nightseam.duplex/1",
  "server": {
    "methods": {"echo": {"request": "Payload", "result": "Payload"}},
    "events": {"changed": {"type": "Payload"}}
  },
  "client": {
    "methods": {"reverse": {"request": "Payload", "result": "Payload"}}
  },
  "errors": {"denied": "The caller is denied."}
}
```

### Write the behavior

Go, the server side. Generated code is never edited by hand; behavior goes
in a file of your own, which `nightseam init probe` writes once:

```go
type Probe struct{}

func (Probe) Echo(ctx context.Context, remote *binding.Remote, p protocol.Payload) (protocol.Payload, error) {
	return remote.Reverse(ctx, p) // the server calls the client, typed, inside the request
}

// wherever you serve:
handler, err := binding.NewHandler(Probe{}, runtime.ServerOptions{})
http.Handle("/probe", handler)
```

### Call it

TypeScript, the client side:

```ts
const client = await Client.dial('wss://example.test/probe', {}, {
  reverse: ({ text, count }) => ({ text: [...text].reverse().join(''), count }),
});

const payload = await client.echo({ text: 'hello', count: 1 });
const stop = client.onChanged(p => console.log('changed', p.text));
```

What you never write: the envelope, the correlation of a response to its
request, cancellation, backpressure, the validator that holds every frame to
the declaration — or the second language's copy of any of it.

## Status

Pre-1.0. The declaration language, the generated surface and the profile
move with minor versions; `CHANGELOG.md` says what each version holds. What
is already held fixed is the agreement between the two languages: both
runtimes' wire validators are held to one conformance table, and the Go and
TypeScript components to one suite over a real socket, so a peer of either
language is held to its twin before either is released.

## Install

```
npm install @nightseam/runtime @nightseam/tunnel           # what a generated TypeScript client needs
go get github.com/Bitspark/nightseam                       # the Go runtime packages
go get -tool github.com/Bitspark/nightseam/cmd/nightseam   # the generator, as a Go tool
```

Nightseam is developer tooling and never a runtime dependency of its own
generator: a generated package depends on the protocol types and the runtime,
and on nothing else.

## The packages

| npm | Go | what it is |
| --- | --- | --- |
| [`@nightseam/duplex`](duplex/ts) | [`duplex/go`](duplex/go) | the seam: ordered frames both ways, an explicit close with a code and a reason, a WebSocket adapter and an in-memory pipe |
| [`@nightseam/runtime`](runtime/ts) | [`runtime/go`](runtime/go) | the peer of the profile: correlation, cancellation, backpressure, presence, trace context, the wire validator, the observer and its console and slog adapters |
| [`@nightseam/tunnel`](tunnel/ts) | [`tunnel/go`](tunnel/go) | channels multiplexed over one peer, each one a connection of the seam, with per-channel credit |
| [`@nightseam/session`](session/ts) | [`session/go`](session/go) | a session over a tunnel's channels: the relay, the registry, the holder of control, the log |
| — | [`cmd/nightseam`](cmd/nightseam) | the generator |

Every component exists in both languages and both are held to one suite. A
third language is `<component>/<lang>` and nothing else moves.

## Using it

A family is a directory of tier files, `api/contracts/<family>/` — the
types, the protocol over them, how a session of them is governed, and what
each target names otherwise than the convention does. The generator renders
it into packages it owns wholesale: `api/go/<f>-protocol`, `-binding` and
`-client`, `api/ts/<f>-client`, and the family's specification as Markdown at
`api/spec/<f>/README.md`.

```
go tool nightseam validate            # every diagnostic of every family
go tool nightseam generate            # render what is stale
go tool nightseam init probe          # the handlers you implement, written once
go tool nightseam check               # in CI: fail if the checked-in output is stale
```

## Documentation

| page | what |
| --- | --- |
| [docs/language.md](docs/language.md) | the declaration language: the tiers, the types, the two sides, a session's governance, per-target names, and a family generic in others |
| [docs/generator.md](docs/generator.md) | the commands and their flags, the pipeline, and what the generated packages own |
| [docs/profile.md](docs/profile.md) | `nightseam.duplex/1`: the envelope, ids and correlation, limits and backpressure, trace context, close codes |
| [docs/tunnel.md](docs/tunnel.md) | channels over one peer: the four operations, ids by parity, credit |
| [docs/session.md](docs/session.md) | a session over a tunnel's channels: the relay's rules, the log, what a consumer builds on it |

## Working on Nightseam

```
go test -short ./...                              # the fast tier: Go alone, seconds
go test ./...                                     # the full tier: both languages, the cross-language gates
pnpm install && pnpm -r check && pnpm -r test
```

The full tier needs Go, Node 22.12 or later and the TypeScript compiler
`pnpm install` brings, and fails rather than skips when one is missing.

[COLLABORATION.md](COLLABORATION.md) says how work is organized here — the
boundary rule, parity between the languages, the two tiers, the golden
discipline, lanes, and working in one tree. [SECURITY.md](SECURITY.md) says
how to report a vulnerability, [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) what
is expected of everyone here, and [RELEASING.md](RELEASING.md) what is
published and how a release is cut.

## License

Apache License, Version 2.0: [LICENSE](LICENSE), with [NOTICE](NOTICE)
beside it.
