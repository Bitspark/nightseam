# Documentation

The declaration language, the generator and how to use them are in the
[README](../README.md). The runtime components each have a page:

| page | what |
| --- | --- |
| [profile.md](profile.md) | `nightseam.duplex/1`: the envelope, ids and correlation, requests, events, limits and backpressure, trace context, close codes |
| [tunnel.md](tunnel.md) | channels over one peer: the four operations, ids by parity, credit, limits and closes |
| [session.md](session.md) | a session over a tunnel's channels: the relay's rules, the log, the surface, what a consumer builds on it |

Around the repository:

| page | what |
| --- | --- |
| [COLLABORATION.md](../COLLABORATION.md) | how work is organized: the boundary rule, parity, the two tiers, goldens, lanes, one tree |
| [RELEASING.md](../RELEASING.md) | what is published and how a release is cut |
| [CHANGELOG.md](../CHANGELOG.md) | what landed, by version |
| [SECURITY.md](../SECURITY.md) | reporting a vulnerability, and what counts as one |
| [V2_MIGRATION.md](V2_MIGRATION.md) | how the declaration language was redesigned into tiers and the generator rebuilt beneath it |
