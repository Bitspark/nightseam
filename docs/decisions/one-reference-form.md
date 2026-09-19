# One reference form

**The question.** A type expression may name a type of this family, of an
imported family, or drawn from a parameter the family is generic in. How
many ways of writing a reference are there?

**Decided.** One: a qualifier in upper camel case is a parameter, in lower
case a family, and every family a declaration names is imported. `Payload`
is this family's, `identity.User` an import's, `S.Envelope` a parameter's;
a generic type of an import is applied with `{"apply": …, "with": …}`, and
a plain reference to one is allowed only where the family has exactly one
parameter to fill it with — otherwise refused, the diagnostic naming the
application to write. [A family in tiers](../declaration/families.md#modeljson).

**Why.** Every additional form is a form every target must read and every
diagnostic must name; the earlier language had several, and each was a
place where two families could say the same thing differently and a reader
could not tell a parameter from an import without the declaration in
front of them. One form, told apart by case, needs no lookup to read. The
plain reference with any number of parameters but one is refused rather
than guessed because a guess is a rule nobody wrote down.

**Serves.** Declarative — a declaration says one thing one way, so that
what is derived from it is derived by one rule.

**Since.** v2 of the declaration language.
