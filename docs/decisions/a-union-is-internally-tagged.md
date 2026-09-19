# A union is internally tagged

**The question.** `enum` of strings was the only sum the language had.
Everything event-like, polymorphic or result-shaped was declared as `json`
and validated by hand in every consumer — the thing the generator exists to
remove. What is a union, and what does it look like on the wire?

**Decided.** `{"kind": "union", "tag": "type", "variants": {…}}`:
**internally tagged, with a declared discriminator**. One wire form, which
every planned language reads —

- A **variant is any type expression**, not only a named record: a
  primitive, an application, a nullable, a shape written inline.
- A variant whose value is an object on the wire carries the tag as a
  member beside its own: `{"type": "text", "body": "…"}`.
- A variant whose value is **not** an object rides under a `value` member
  beside the tag: `{"type": "count", "value": 3}`. The union may name that
  member otherwise with `"value": "<member>"`.
- A variant record that **declares the tag member itself** is a variant
  without a wrapper — and then it declares it as `{"literal": "text"}`,
  the literal of that variant, required. Declaring it any other way is a
  `check` diagnostic and not a convention, because the first family that did
  it would hand two languages one frame to read two ways.
- Unions take parameters like anything else ([one parameter mechanism, of
  two sorts](one-parameter-mechanism-of-two-sorts.md)), so `Result<T, E>`
  and `Option<T>` are declared in a family rather than built into the
  language.
- A union may `extends` another, **adding** variants: the two read the same
  discriminator and the same value member, a tag the base already carries
  may not be redeclared, and a chain that returns is refused. A value of the
  base validates against the extended; the reverse does not, and that
  direction is the validators' to hold.
- `enum` stays, as the union of variants with no payload.

**Why.** External tagging (`{"text": {…}}`) is compact and every language
special-cases the single-key object, a payload that is itself an object
nests twice, and the tag cannot be read without unwrapping. An untagged
`oneOf` is ambiguous on the wire and undecidable for a validator the moment
two variants overlap. Internal tagging is what each planned language renders
natively or nearly: a discriminated union in TypeScript, `#[serde(tag)]` in
Rust, a sealed hierarchy in Haskell and Swift, a value that owns its codec
and switches on its kind in Go, a `Literal`-tagged union in Python.
`std::variant` in C++ has no tag of its own and is the case the form was
pushed against: it reads the
declared discriminator and selects the alternative, which is more code than
the others write and is not a different wire form — which is the test a form
has to pass, and the reason the reach was taken to its limit here rather
than discovered at the eighth language.

**Serves.** Agnosticism — one wire form every language reads, rather than a
`json` field each consumer reads its own way.

**Since.** 0.4.0, [#56](https://github.com/Bitspark/nightseam/issues/56),
landed by #101. The concrete Go value is the verdict of
[#140](https://github.com/Bitspark/nightseam/issues/140), with implementation
tracked in [#102](https://github.com/Bitspark/nightseam/issues/102): its
codec composes through generic declarations without extra codec arguments. The Go target
still refuses unions until that renderer is complete.
