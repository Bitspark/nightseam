# Strings are Unicode scalar values

**The question.** A declaration and a wire value both carry text, and JSON
admits text no language agrees about: invalid UTF-8, and `\uD800` with no
low surrogate after it. A decoder may refuse such input or replace it with
U+FFFD, and the two choices are indistinguishable afterwards. What is the
input domain of a string, and who decides it?

**Decided.** Unicode scalar values, refused at the boundary. Invalid UTF-8
and unpaired UTF-16 surrogate escapes are rejected before decoding or
publishing can replace them — in declarations and on the wire alike, and so
in names, descriptors, literals, enum and union tags, object keys, nested
arbitrary JSON and custom encodings. Valid surrogate pairs, an ordinary
U+FFFD and ASCII text that spells a regular-expression escape are three
different things and each passes unchanged. There is no Unicode
normalization. The operator chose option A of
[#170](https://github.com/Bitspark/nightseam/issues/170), "reject malformed
Unicode".

**Why.** The alternative was to let each language's decoder do what it does,
which is not the same thing twice: Go would read a descriptor's lone
surrogate as U+FFFD and accept a literal that TypeScript had refused, and
the two peers would then disagree about what had been said with neither able
to discover it. [A pattern has one dialect](a-pattern-has-one-dialect.md)
settles how text is *matched* and cannot settle what text *is*; a shared
matching semantics over an input domain each language decides for itself is
not shared. The refusal costs a pass over every frame read and every value
published, and it is bought at the only moment it can be: while the
malformed text is still there to see. Once a decoder has substituted for it,
no later check can tell that it did.

**Serves.** Agnosticism — one input domain, decided outside every language,
so that each runtime realizes it rather than spelling a dialect of its own.

**Since.** 0.5.0, [#170](https://github.com/Bitspark/nightseam/issues/170),
implemented by [#182](https://github.com/Bitspark/nightseam/issues/182).

Both runtimes read [the Unicode cases](../../conformance/tables/unicode.json)
and [the frame table](../../conformance/tables/frames.json), and the latter
runs over real connections in the shared conformance suite. The peer applies
the guard as it reads a frame — `decodeFrame` in Go, `decodeEnvelope` in
TypeScript — before a method name is interpreted or a payload is forwarded,
and an optional or nullable member is held to it as it is read; a live scope
reads a reference through the same peer, so nothing it decodes is checked
less. Loader diagnostics name the source file. Outgoing values are checked
while malformed text is still visible, including whatever a custom encoder
produced, and generated codecs use the same strict boundary as the peer.
