# Decisions

The record of what was decided and why — one page per decision, in a form
a review can argue with. The state pages under `wire/`, `runtime/` and
`declaration/` say what the tree *is*; a decision page says what it was
chosen *over*, and what the alternative cost, so that the next person to
propose the alternative finds the reason rather than repeats the afternoon.
A goal review (`goals/`) reads this record first: a proposal that reopens a
decision says so and argues against the reason recorded here, and a verdict
that changes one rewrites the page — the record is of what holds, not of
what was once said.

Each page has five parts: **the question**, **decided**, **why** (what the
alternative cost), **serves** (the goal it is a step toward), **since** (the
version, issue or event it dates from). A decision is recorded when a page
of the state gives a reason for it; a reason with no page here is a page
missing, and a decision whose reason nobody can state is a design issue,
not a record.

| decision | serves | since |
| --- | --- | --- |
| [Envelope members are what the peer acts on](envelope-members-are-what-the-peer-acts-on.md) | layering | v2 |
| [`meta` is a header, not a member](meta-is-a-header-not-a-member.md) | layering, boundary | 0.3.0, #49 |
| [No subprotocol by default](no-subprotocol-by-default.md) | agnosticism | 0.3.0 |
| [A deadline is not a cancel](a-deadline-is-not-a-cancel.md) | observability | 0.3.0 |
| [Queues are paced for one deadline](queues-are-paced-for-one-deadline.md) | configurability | 0.3.0 |
| [`busy` is a refusal, not a failure](busy-is-a-refusal-not-a-failure.md) | observability | 0.3.0 |
| [Close codes are the WebSocket registry's](close-codes-are-the-websocket-registrys.md) | composability | 0.2.0 |
| [Credit is per channel](credit-is-per-channel.md) | composability | 0.2.0 |
| [The relay mints its own ids](the-relay-mints-its-own-ids.md) | layering | 0.2.0 |
| [The session's vocabulary is not logged](the-sessions-vocabulary-is-not-logged.md) | layering | 0.3.0 |
| [The session runs over any connection of the seam](the-session-runs-over-any-connection-of-the-seam.md) | composability | #48 |
| [Refusals are codes, not prose](refusals-are-codes-not-prose.md) | agnosticism | 0.3.0 |
| [The log is bound at its head](the-log-is-bound-at-its-head.md) | composability | 0.3.0 |
| [The observer is told at the write](the-observer-is-told-at-the-write.md) | observability | 0.3.0 |
| [Layers share the peer's observer](layers-share-the-peers-observer.md) | observability, composability | 0.2.0 |
| [An observer never sees a payload](an-observer-never-sees-a-payload.md) | observability, boundary | 0.2.0 |
| [OpenTelemetry is its own package](opentelemetry-is-its-own-package.md) | agnosticism | 0.3.0 |
| [Generated files carry no version](generated-files-carry-no-version.md) | declarative | 0.3.0 |
| [A specification is rendered, not written](a-specification-is-rendered-not-written.md) | declarative | 0.2.0 |
| [One reference form](one-reference-form.md) | declarative | v2 |
| [Generic families render once and commute](generic-families-render-once-and-commute.md) | agnosticism | v2 |
| [Tiers are promises, not rankings](tiers-are-promises-not-rankings.md) | agnosticism | 0.3.0 |
