# A concept is admitted by composition

**The question.** What makes a concept Nightseam's — a primitive of the
declaration language or of a runtime — rather than a consumer's, and what
may a proposal assume when it argues for one? The session layer had been
admitted on three arguments that turned out not to count: it existed, it
owned no state of the consumer's, and it was reusable. Against what basis
is a candidate tested, and what does a failed composition have to show?

**Decided.** A concept is admitted only when it is in scope at its level and
cannot be composed from the justified primitives beneath it; the argument
is written down in six steps — scope, basis, the composition attempt, the
obstruction, the smallest missing primitive, the alternatives — and ends in
one of four classes: primitive, composition, domain semantics, unresolved
candidate. The **basis** is one level down and nothing is grandfathered: the
public shipped surface of the level beneath the candidate plus ordinary
application facilities, used as a client of that surface and never by
speaking its wire, with no element in the basis because it ships. An
**obstruction** is a guarantee of the level — identity, scope, ordering,
correlation, cancellation, lifetime, refusal — that the composition loses,
argued in a written scenario; an executed construction is not a
precondition of a verdict, though one decision may require it of itself.
More code, worse ergonomics, absent automatic coordination, performance, a
defect and a missing document are not obstructions and are routed
elsewhere. *Atomic* means irreducible against that basis, not one method,
a mutex or an indivisible transition. The test applies to what exists as it
applies to what is proposed, and *shipped* is not a class: a composition
every consumer would write identically is shipped as mechanism and stays a
composition. [Admitting a concept](../admission.md) is the test and the
table it has produced; the *Design* form requires its evidence.

**Why.** The alternatives each left the central ambiguity open or closed it
vacuously. Clarifying the scope guidance without requiring decomposition
evidence would have kept admitting reusable domain models on their names,
which is how controller roles, deciding operations and question handover
came to be spoken of as generic. Taking the whole public surface as the
basis would have made the present inventory the definition of a primitive,
so that the review meant to judge the session's concepts could have cited
them. Taking the seam and raw frames as the basis would have made
correlation, channels and credit "compositions" too, so that nothing was
irreducible and the test said nothing. Requiring an executed construction
before every verdict would have priced a design round by its slowest
scenario, when the decision that needs one — live binding, [#202](https://github.com/Bitspark/nightseam/issues/202)
— can ask for it by name; a written argument can be re-read and answered,
where a run that was never written cannot. What the decision costs is
stated: a proposal now writes six answers, and a concept that has none is
not admitted, however useful.

**Serves.** Boundary — the line is stated sharply enough that a proposal can
be classified by a reader who did not write it, and the same question asked
twice gets the same side; and composability — a part is admitted as a
primitive only when no assembly of the parts beneath it could have been the
answer.

**Since.** 0.5.0, [#199](https://github.com/Bitspark/nightseam/issues/199),
the operator's verdict of 2026-09-19; landed by #208. It supersedes nothing
by itself: [#139](https://github.com/Bitspark/nightseam/issues/139) and
[#167](https://github.com/Bitspark/nightseam/issues/167) are superseded by
[#196](https://github.com/Bitspark/nightseam/issues/196) and
[#200](https://github.com/Bitspark/nightseam/issues/200), which the table
records.
