# Tiers are promises, not rankings

**The question.** Several languages, at different stages. What does a tier
say about a language, and how are languages assigned to tiers?

**Decided.** A tier says which profiles a language guarantees and when —
which red cells stop a release — and nothing about which scenarios run:
every language runs the whole suite and the matrix shows every cell. Four
promises nest; there is no fifth. The assignment is a policy about
promises: the four pilots are planned for tier 2 because they are the four
that get pushed to every profile first, and two of them are there because
they are the hardest. [Languages, profiles and tiers](../languages/tiers.md).

**Why.** A tier that ranked languages would have said something about
effort or esteem and nothing a consumer could rely on; a tier that is a
bundle of promises says exactly what a consumer choosing a language is
promised there, and the suite can enforce it — the release workflow
refuses a tag whose matrix has a cell the tier table says stops it. Running
the whole suite for every language, rather than only a tier's, is what
makes the matrix a picture of progress instead of a picture of policy.
Putting the hardest languages among the pilots is deliberate: this
repository tests its limits to find out where the model breaks while
breaking it is still cheap, and a language that would only ever be held to
`core` is a language nobody pushed.

**Serves.** Agnosticism — the definition is outside every language, and
each language's standing against it is one table, not one opinion.

**Since.** 0.3.0.
