# Strings are Unicode scalar values

The operator chose option A of
[#170](https://github.com/Bitspark/nightseam/issues/170):
“A — Reject malformed Unicode (recommended)”.

Declarations and wire values admit Unicode scalar strings. Reject invalid
UTF-8 and unpaired UTF-16 surrogate escapes before decoding or publishing
can replace them. The rule covers names, descriptors, literals, enum and
union tags, object keys, nested arbitrary JSON and custom encodings. Valid
surrogate pairs, ordinary U+FFFD and ASCII text spelling a regular-expression
escape remain distinct and unchanged. There is no Unicode normalization.

Without this rule, Go could read a descriptor's lone surrogate as U+FFFD
and accept a literal that TypeScript rejected. The pattern dialect's
Unicode matching semantics alone cannot settle the input domain.

Both runtimes read [the Unicode cases](../../conformance/tables/unicode.json)
and [the frame table](../../conformance/tables/frames.json). The latter runs
over real connections in the shared conformance suite. Loader diagnostics
name the source file; outgoing values are checked while malformed text is
still visible, including the output of custom encoders. Generated codecs
use the same strict boundary as the peer.

Sessions apply the guard when reading profile frames over raw connections,
before names are interpreted or payloads forwarded. An origin must be
scalar text before attachment, since control notifications publish it.
