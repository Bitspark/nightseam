# A pattern has one dialect

**The question.** `pattern` constrains a string and the validators enforce
it — in whichever regular-expression language each runtime's library
happens to be. Go validated with RE2, TypeScript with ECMAScript, and the
generator compiled the pattern with Go's engine only. `^[[:alpha:]]+$`
passed `check`, matched letters at the Go end and matched a class of
`[ : a l p h` at the TypeScript end, with no error anywhere. Which language
is a pattern written in?

**Decided.** Nightseam's own, small, and named in one sentence:

> A pattern is written in Nightseam's own regular-expression language:
> ECMAScript syntax without lookaround and without backreferences, which is
> what every planned runtime's engine can express and mean the same by.

`internal/check` holds a pattern to it **in both directions** — the
spellings only an RE2 engine reads (`(?P<`, inline flag groups, POSIX
classes, `\A`, `\z`, `\Q`) and the ones only an ECMAScript engine reads
(lookahead, lookbehind, `\1`, `\k<`, named groups) are refused alike — and
the `patterns` rows of `conformance/tables/validator.json` hold the
generator and every runtime's validator to the same list. A runtime refuses
the same spellings at validate time, since a descriptor can be hand-written
or come from a target that skipped `check`.

**Why.** The alternative is the stronger claim — "a pattern is an RE2
expression; every runtime carries an RE2 engine" — which is honest and has
one meaning, and which puts a dependency into every runtime that today has
none, against the profile's own dependency-freedom. The intersection is the
bolder choice *for the model*: Nightseam defines a regular-expression
language of its own, small enough that every engine already implements it,
and each runtime implements it with what it has. It was one sentence to
decide with two runtimes and would have been eight reconciliations with
eight, four of which were already queued when this was found.

**Serves.** Agnosticism — a facet means one thing in every language, rather
than whatever the nearest library means by it.

**Since.** 0.4.0, [#71](https://github.com/Bitspark/nightseam/issues/71),
landed by #101.
