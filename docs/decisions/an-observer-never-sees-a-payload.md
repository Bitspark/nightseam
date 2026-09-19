# An observer never sees a payload

**The question.** An observer is told what a peer did. How much of what
the peer carried does it see?

**Decided.** Names, ids, sizes, durations, outcomes, close codes and the
frame's trace — and never a payload, a `meta` key or value, or what a
handler threw beyond the value itself. It emits and never aggregates, and
chooses no backend; an absent one costs nothing; one that gives up gives
up alone. The suites hold this by putting a sentinel in every payload a
peer carries and finding it in nothing any event renders. [The
observer](../runtime/observer.md).

**Why.** What a method was called, how big its frame was and how it ended
are observable; what it said is not — a payload reaching an observer is a
payload reaching whatever backend the observer writes to, and the profile
has no way to know which of a family's fields is a credential. Drawing the
line at the payload, and at `meta` with it, is what lets a consumer attach
any observer to any peer without auditing the family first, and what lets
the OpenTelemetry adapter render an event of a layer it has never heard of
without a payload reaching a span: a field that is a name, a count, a
flag, a duration or a trace becomes an attribute and a field of any other
kind is dropped. Aggregating, sampling and choosing a backend are the
observer's because a peer that did them would be a peer that decided a
policy.

**Serves.** Observability, within the boundary rule — the mechanism tells,
the consumer decides what to do with it.

**Since.** 0.2.0, with the observer.
