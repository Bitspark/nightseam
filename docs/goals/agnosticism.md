# Agnosticism

## At the limit

Nightseam is of no host, no transport, no backend and no language. A peer
assumes nothing about what executes it, what carries its frames, or what
consumes what it reports, beyond the profile itself; a consumer brings the
host, the transport and the backend, and Nightseam brings none. The thing
itself — the wire, the promises a language makes, the scenarios that hold
it — is defined outside every language, and each language is one
realization of that definition, in its own idiom: nothing exists in one
and not another, nothing lands in one before another, and none is the
definition's home.

## The dimensions

- **Host.** What a peer assumes about where it runs: threads, a clock,
  sockets, a file system, a way to wait. The limit is what the seam states
  and nothing more; a peer that runs in a browser, on a server and in a
  process with neither is the same peer.
- **Transport.** Whether anything that carries ordered frames both ways
  and closes with a code will do, or whether one kind of carrier is
  assumed — in a name, a default, a code path, a test that only runs over
  one.
- **Backend.** What a consumer installs by taking a component: nothing it
  did not choose. What watches, traces or stores is the consumer's choice,
  and the component that binds to one is a component of its own that the
  others do not depend on.
- **Language — existence.** Whether every component, every operation,
  every event, every refusal exists in every language, under the same
  meaning.
- **Language — timing.** Whether a thing lands in every language before it
  is released, or in one first and the others within some allowance; the
  allowance is what a language's promise names, and the limit is none.
- **Language — idiom.** Whether each realization is written as that
  language would write it, or transliterated from another: the same thing,
  not the same spelling. A shape forced on one language because another
  has it is a failure of this dimension in the other direction.
- **Language — definition.** Whether the definition is one, written once,
  as data every language reads and is held to — or whether one language's
  code is, in practice, the specification the others copy.

## What it yields to

A reference: where the definition is silent, some realization must decide,
and the one that decides is a tool for settling disagreement, not the
definition's home — its decisions are written back into the definition, so
that the silence closes. Idiom: parity is of meaning, and a language's own
unit, own error shape, own way of waiting are not departures from it.
Platform facts: a dependency that *is* the platform — its own socket, its
own random source — is not a choice made for the consumer.

## What it is not

Not identical code or identical surfaces across languages. Not portability
of the tool that renders: the generator is one program, and this goal is
about what it renders and what a peer is. Not "no dependencies" as dogma:
the goal is that a consumer takes on nothing it did not choose, and a
component whose purpose is to bind to a backend carries that backend by
design. Not a ranking of languages: a language that lags is behind on a
dimension, not lesser.
