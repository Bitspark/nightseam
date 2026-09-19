# A tier is a built-in family

**The question.** A tier brings declarations to every family that carries
it: the protocol tier brings `Envelope` and `Handle`, the session tier
brings `session.control` and its kin. The first was a Go literal in the
generator (`model.Injected()`); the second was about to become a second
mechanism beside it. One injection table for both, or no injection at all?

**Decided.** No injection. The profile, the tunnel and the session are
**families**, declared in the declaration language under
`internal/model/builtin/<name>/` as ordinary tier files, carried in the
binary and held to the same shape schemas a consumer's family is. A family
that has a tier file **imports** that tier's built-in, with no `imports`
line — the tiers' table names which — and a declaration writes
`duplex.Envelope`, the built-in that declares it, rather than a bare name
that came from nowhere. The generator's one import code path renders it; a
diagnostic that points into one locates it as
`nightseam:duplex/model.json`, which is plainly not a file of the
consumer's checkout; a family's specification prints the carried types with
their members, so a reader sees them without reading this repository; and
`conformance/tables/frames.json` is held to `duplex`'s `Envelope` by a test
rather than kept in step by hand. Naming a built-in in `imports` is
refused, and so is declaring a type it carries.

The same spec target renders [standalone references](../declaration/builtins/README.md)
for every built-in family, including its operations as well as its types.
The fast test tier compares those checked-in documents with the embedded
declarations, so a changed built-in cannot leave its reference behind.

The protocol tier's built-in is **carried**: its types are the importing
family's own, because a family's envelope is a message of *that* family and
`probe.Envelope` and `codex.Envelope` are two types a language must keep
apart. A tier whose built-in is not carried — the session's — has one
declaration for every family, reached as an extended side ([a side may
extend another family's](a-side-may-extend-another-familys.md)). The
distinction is in the tiers' table, one field, not in a renderer.

**Why.** Two mechanisms for one idea is a seam every new language learns
twice, and the first of them was a list of Go structs that no schema, no
check and no specification ever saw: a reader of `api/spec/` could not find
out what an `Envelope` was, and the frames table had to be kept in step
with it by reading both. The alternative — one injection table, the option
this was raised as — would have been one mechanism, but still a mechanism
of the generator's rather than of the language's, and it would have left
the question this repository exists to answer unasked: *can a profile be
declared in its own declaration language?* It can. What it cost is stated
plainly: the built-in families are loaded into every world and a consumer
cannot see them in `api/contracts/`, which is answered by locating them
apart, printing them in the specification, and refusing a family that
declares over one.

**Serves.** Declarative — what a family carries is declared, in the same
language, and read by the same checks; and boundary, since the profile's
own vocabulary is now stated where every other vocabulary is.

**Since.** 0.4.0, [#62](https://github.com/Bitspark/nightseam/issues/62),
landed by #101.
