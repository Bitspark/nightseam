# A union is adjacently tagged

**The question.** What wire carrier preserves every admitted union payload,
including arbitrary JSON, maps and nullable records, through encoding and
decoding in every language?

**Decided.** A union declares its discriminator with `"tag"` and carries
its complete payload under `"value"`, or the member named by its `"value"`
setting. Records, maps, JSON, arrays, primitives and null all use the same
carrier: `{"type":"text","value":{"body":"…"}}` or
`{"type":"count","value":3}`. The discriminator and value member differ.

A variant is any type expression, or `{"empty":true}` for a variant with
no payload. That marker is legal only in a union's `variants` map. It
encodes as the tag alone: `{"type":"none"}`. An empty record is a payload
`{}`, and a nullable payload whose value is null still writes `"value":null`.
A record's own literal member remains inside its payload; it may differ
from the outer tag, be optional, or be nullable.

Unions take parameters through [one parameter mechanism](one-parameter-mechanism-of-two-sorts.md).
They may extend other unions, adding variants with the same discriminator
and value member. A generic base requires explicit arguments at that edge.
A base's tag cannot be redeclared; an inheritance cycle or conflicting
inherited declaration/binding is refused. A base value validates against
the extended union; the reverse does not. String `enum` stays unchanged.

**Why this replaces the first verdict.** #56 chose internal tagging over
external tagging (`{"text":{…}}`) and untagged `oneOf`: a discriminator
selects one alternative without guessing which overlapping shape a value
has. It put object members beside the tag and other payloads under `value`,
and let a record's literal member carry its own tag.

#146 found that this loses information. JSON payloads `3` and `{"value":3}`
both became `{"kind":"json","value":3}`. Nullable-record payloads null
and `{"value":null}` also became one frame. A decoder cannot reconstruct
both meanings, and specializing a generic codec would break the law that
generic and bound declarations behave alike. The adjacent carrier preserves
both examples and prevents payload keys from colliding with envelope keys.
The flat-object and literal-record exceptions are withdrawn.

The representation remains one every language can read. TypeScript has a
discriminated union. Go, under #140, has a concrete value that owns its codec
and switches on its kind, with typed variants and widening/narrowing helpers;
it can be used as a type argument without a codec registry. Other targets
must realize the same wire carrier and generic-versus-bound law.

**Serves.** Agnosticism — one lossless wire form and one meaning for a
payload in every language.

**Since.** 0.4.0: [#56](https://github.com/Bitspark/nightseam/issues/56),
amended by [#140](https://github.com/Bitspark/nightseam/issues/140) for Go's
surface and [#146](https://github.com/Bitspark/nightseam/issues/146) for the
carrier. #133 holds declaration/checker/render facts; #135 holds both
validators and the shared table; #102/#103 hold generated codecs; #105
holds the cross-wire proofs.
