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
| [profile.md](profile.md) | `nightseam.duplex/1`: the envelope, ids and correlation, requests, events, limits and backpressure, trace context, close codes |
| [tunnel.md](tunnel.md) | channels over one peer: the four operations, ids by parity, credit, limits and closes |
| [session.md](session.md) | a session over a tunnel's channels: the relay's rules, the log, the surface, what a consumer builds on it |
| [observability.md](observability.md) | one observer across the three layers: the rule, the twenty-five events in both languages, the two adapters, and a layer's own |

Around the repository:

| page | what |
| --- | --- |
| [COLLABORATION.md](../COLLABORATION.md) | how work is organized: the boundary rule, parity, the two tiers, goldens, lanes, one tree |
| [RELEASING.md](../RELEASING.md) | what is published and how a release is cut |
| [CHANGELOG.md](../CHANGELOG.md) | what landed, by version |
| [SECURITY.md](../SECURITY.md) | reporting a vulnerability, and what counts as one |
| [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md) | what is expected of everyone here, and how a concern is raised |
