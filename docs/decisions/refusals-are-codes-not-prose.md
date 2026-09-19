# Refusals are codes, not prose

**The question.** A consumer's server calls into the session layer, and a
call may be refused. What does the refusal carry, and is it the same thing
in every language?

**Decided.** A code a program branches on, never prose it would have to
match: one vocabulary of ten, the same in both languages name for name,
each code a constant. In Go a `*session.Error` with `Code` and `Message`
reached by `errors.As` and by `errors.Is` matching on the code alone; in
TypeScript a `DuplexError` whose `code` is the same string. A close is not a
refusal and carries a code of its own, and the reason a close carries is
one closed set of sentences in both languages, word for word. [The session's
surface](../runtime/session.md#what-it-refuses-with) and [the
session](../wire/session.md#what-is-refused-and-how-a-connection-ends).

**Why.** What a call refuses with is part of the surface a consumer writes
against, and a consumer cannot ask which runtime wrote the relay it called
— so a refusal that differed between the languages, or that could only be
told apart by its wording, would have been a surface with two shapes. A
message is the part of a refusal that may be reworded, which is why `Is`
matches on the code alone. The close reasons went the same way when the
two relays were found ending a connection with 1008 and one sentence in
one language and 1002 and three in the other: a consumer branching on the
close read one event two ways, and the fix was one set, the decoder's own
words, in both.

**Serves.** Agnosticism — a language is the same thing in that language's
idiom, and a consumer never learns which one wrote the other side.

**Since.** 0.3.0.
