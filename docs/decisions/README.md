# Decisions

The record of what was decided and why — one page per decision, in a form
a review can argue with. The state pages under `wire/`, `runtime/` and
`declaration/` say what the tree *is*; a decision page says what it was
chosen *over*, and what the alternative cost, so that the next person to
propose the alternative finds the reason rather than repeats the afternoon.
A goal review ([goals](../goals/README.md)) reads this record first: a
proposal that reopens a decision says so and argues against the reason
recorded here, and a verdict that changes one rewrites the page — the
record is of what holds, not of what was once said. A decision the tree has
since left behind is kept and **marked superseded** at its head, with what
replaced it and why: the reasoning is what the next proposal argues against,
and deleting the page would leave the afternoon to be repeated.

Each page has five parts: **the question**, **decided**, **why** (what the
alternative cost), **serves** (the goal it is a step toward), **since** (the
version, issue or event it dates from). A decision is recorded when a page
of the state gives a reason for it; a reason with no page here is a page
missing, and a decision whose reason nobody can state is a design issue,
not a record. One page may record two verdicts where one reason covers
both, and says so at the top.

| decision | serves | since |
| --- | --- | --- |
| [A concept is admitted by composition](a-concept-is-admitted-by-composition.md) | [boundary](../goals/boundary.md), [composability](../goals/composability.md) | 0.5.0, #199 |
| [Envelope members are what the peer acts on](envelope-members-are-what-the-peer-acts-on.md) | [layering](../goals/layering.md) | v2 |
| [`meta` is a header, not a member](meta-is-a-header-not-a-member.md) | [layering](../goals/layering.md), [boundary](../goals/boundary.md) | 0.3.0, #49 |
| [No subprotocol by default](no-subprotocol-by-default.md) | [agnosticism](../goals/agnosticism.md) | 0.3.0 |
| [A deadline is not a cancel](a-deadline-is-not-a-cancel.md) | [observability](../goals/observability.md) | 0.3.0 |
| [Queues are paced for one deadline](queues-are-paced-for-one-deadline.md) | [configurability](../goals/configurability.md) | 0.3.0 |
| [`busy` is a refusal, not a failure](busy-is-a-refusal-not-a-failure.md) | [observability](../goals/observability.md) | 0.3.0 |
| [Close codes are the WebSocket registry's](close-codes-are-the-websocket-registrys.md) | [composability](../goals/composability.md) | 0.2.0 |
| [Credit is per channel](credit-is-per-channel.md) | [composability](../goals/composability.md) | 0.2.0 |
| [The relay mints its own ids](the-relay-mints-its-own-ids.md) *(superseded, 0.5.0)* | [layering](../goals/layering.md) | 0.2.0 |
| [The session's vocabulary is not logged](the-sessions-vocabulary-is-not-logged.md) *(superseded, 0.5.0)* | [layering](../goals/layering.md) | 0.3.0 |
| [The session runs over any connection of the seam](the-session-runs-over-any-connection-of-the-seam.md) *(superseded in what it names, 0.5.0)* | [composability](../goals/composability.md) | #48 |
| [Refusals are codes, not prose](refusals-are-codes-not-prose.md) | [agnosticism](../goals/agnosticism.md) | 0.3.0 |
| [The log is bound at its head](the-log-is-bound-at-its-head.md) *(superseded, 0.5.0)* | [composability](../goals/composability.md) | 0.3.0 |
| [The observer is told at the write](the-observer-is-told-at-the-write.md) | [observability](../goals/observability.md) | 0.3.0 |
| [Layers share the peer's observer](layers-share-the-peers-observer.md) | [observability](../goals/observability.md), [composability](../goals/composability.md) | 0.2.0 |
| [An observer never sees a payload](an-observer-never-sees-a-payload.md) | [observability](../goals/observability.md), [boundary](../goals/boundary.md) | 0.2.0 |
| [OpenTelemetry is its own package](opentelemetry-is-its-own-package.md) | [agnosticism](../goals/agnosticism.md) | 0.3.0 |
| [Generated files carry no version](generated-files-carry-no-version.md) | [declarative](../goals/declarative.md) | 0.3.0 |
| [A specification is rendered, not written](a-specification-is-rendered-not-written.md) | [declarative](../goals/declarative.md) | 0.2.0 |
| [A specification has a document, and renderers of it](a-specification-has-a-document-and-renderers-of-it.md) | [declarative](../goals/declarative.md), [extensibility](../goals/extensibility.md), [configurability](../goals/configurability.md), [agnosticism](../goals/agnosticism.md) | #154, #155, #156, #157 |
| [One reference form](one-reference-form.md) | [declarative](../goals/declarative.md) | v2 |
| [A callable is a declared kind, and its identity is its declaration](a-callable-is-a-declared-kind.md) | [declarative](../goals/declarative.md), [agnosticism](../goals/agnosticism.md) | 0.5.0, #201 |
| [An owner is a lifetime the caller supplies](an-owner-is-a-lifetime-the-caller-supplies.md) | [boundary](../goals/boundary.md), [composability](../goals/composability.md) | 0.5.0, #257 |
| [A tier is a built-in family](a-tier-is-a-built-in-family.md) | [declarative](../goals/declarative.md), [boundary](../goals/boundary.md) | 0.4.0, #62 |
| [A side may extend another family's](a-side-may-extend-another-familys.md) | [composability](../goals/composability.md) | 0.4.0, #61 |
| [One parameter mechanism, of two sorts](one-parameter-mechanism-of-two-sorts.md) | [declarative](../goals/declarative.md), [agnosticism](../goals/agnosticism.md) | 0.4.0, #57 and #59 |
| [A union is adjacently tagged](a-union-is-adjacently-tagged.md) | [agnosticism](../goals/agnosticism.md) | 0.4.0, #56, amended by #146 |
| [Nullness is a fact of a value](nullness-is-a-fact-of-a-value.md) | [declarative](../goals/declarative.md) | 0.4.0, #58 |
| [A shape without a name is named by where it sits](a-shape-is-named-by-where-it-sits.md) | [declarative](../goals/declarative.md) | 0.4.0, #60 |
| [A pattern has one dialect](a-pattern-has-one-dialect.md) | [agnosticism](../goals/agnosticism.md) | 0.4.0, #71 |
| [Strings are Unicode scalar values](strings-are-unicode-scalars.md) | [agnosticism](../goals/agnosticism.md) | 0.5.0, #170 |
| [Generic families render once and commute](generic-families-render-once-and-commute.md) | [agnosticism](../goals/agnosticism.md) | v2 |
| [Tiers are promises, not rankings](tiers-are-promises-not-rankings.md) | [agnosticism](../goals/agnosticism.md) | 0.3.0 |
