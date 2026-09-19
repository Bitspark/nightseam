# Documentation

The [README](../README.md) is the short path: what Nightseam is, how it is
installed and what a family looks like. The reference is here.

| page | what |
| --- | --- |
| [language.md](language.md) | the declaration language: the tiers, the types, the two sides, a session's governance, per-target names, and a family generic in others |
| [generator.md](generator.md) | the commands and their flags, the pipeline that renders a family, and what the generated packages own |

The runtime components each have a page:

| page | what |
| --- | --- |
| [profile.md](profile.md) | `nightseam.duplex/1`: the envelope, ids and correlation, requests, events, limits and backpressure, trace context, the subprotocol, close codes |
| [tunnel.md](tunnel.md) | channels over one peer: the four operations, ids by parity, credit, limits and closes |
| [session.md](session.md) | a session over a tunnel's channels: the relay's rules, the log, the surface, what a consumer builds on it |
| [observability.md](observability.md) | one observer across the three layers: the rule, the twenty-five events in both languages, the `slog` and console adapters, the OpenTelemetry one beside them, and a layer's own |
| [tiers.md](tiers.md) | languages, profiles and tiers: the four promises a language can make, the profiles the conformance suite holds them to, which tier guarantees what and when, and how a language is onboarded |
| [layers.md](layers.md) | what belongs where: the test that decides whether something new on the wire is the profile's, a layer's own vocabulary, or a header — and how a layer speaks on the wire |

Around the repository:

| page | what |
| --- | --- |
| [CONTRIBUTING.md](../CONTRIBUTING.md) | the short path in: what to run, how a change is cut |
| [COLLABORATION.md](../COLLABORATION.md) | how work is organized: the boundary rule, no legacy, parity, the two tiers, goldens, lanes, one tree |
| [../conformance/DRIVER.md](../conformance/DRIVER.md) | the conformance suite: the protocol a language's testee speaks to the runner, every op, and how a language joins |
| [RELEASING.md](../RELEASING.md) | what is published and how a release is cut |
| [CHANGELOG.md](../CHANGELOG.md) | what landed, by version |
| [SECURITY.md](../SECURITY.md) | reporting a vulnerability, and what counts as one |
| [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md) | what is expected of everyone here, and how a concern is raised |
