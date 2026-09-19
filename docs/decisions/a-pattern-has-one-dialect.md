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
> ECMAScript Unicode syntax without lookaround and without backreferences,
> matching code points with ECMAScript character classes and line terminators.

[#141](https://github.com/Bitspark/nightseam/issues/141) settles the matching
rule as ECMAScript `u` mode. NBSP is whitespace; one emoji matches one dot;
dot excludes LF, CR, U+2028 and U+2029. `\d` and `\w` remain the ASCII
classes, and word boundaries use that same word class. `length` already
counts code points, so text has one unit. Unicode mode also refuses legacy
octal escapes, permissive identity escapes and class ranges whose endpoints
are not single characters.

`internal/check` holds a pattern to it **in both directions** — the
spellings only an RE2 engine reads (`(?P<`, inline flag groups, POSIX
classes, `\A`, `\z`, `\Q`) and the ones only an ECMAScript engine reads
(lookahead, lookbehind, `\1`, `\k<`, named groups) are refused alike — and
Unicode property escapes remain outside the declared subset. The
`patterns` and `patternValues` rows of `conformance/tables/validator.json` hold the
generator and every runtime's validator to the same list. A runtime refuses
the same spellings at validate time, since a descriptor can be hand-written
or come from a target that skipped `check`. Go's `internal/pattern` parses
the dialect and translates character sets and escapes for its engine;
TypeScript compiles with `u`. A full-tier test compares the Go translation
against Node's Unicode engine, including repetition bounds and escaped
astral characters. The matching and syntax rules come from the
[ECMAScript pattern specification](https://tc39.es/ecma262/multipage/text-processing.html#sec-patterns).

**Why.** The alternative is the stronger claim — "a pattern is an RE2
expression; every runtime carries an RE2 engine" — which is honest and has
one meaning, and which puts a dependency into every runtime that today has
none, against the profile's own dependency-freedom. The intersection is the
bolder choice *for the model*: Nightseam defines a regular-expression
language of its own, and each runtime implements it with what it has.
Native compilation was insufficient: Go and JavaScript disagreed on NBSP,
emoji and line terminators. The Unicode ruling replaces that assumption
with explicit translation and shared value tests. It was one sentence to
decide with two runtimes and would have been eight reconciliations with
eight, four of which were already queued when this was found.

**Serves.** Agnosticism — a facet means one thing in every language, rather
than whatever the nearest library means by it.

**Since.** 0.4.0, [#71](https://github.com/Bitspark/nightseam/issues/71),
landed by #101.
