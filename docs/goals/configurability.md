# Configurability

## At the limit

Every bound Nightseam imposes is the consumer's to set, under one name and
one meaning in every language, with a default that is right for the common
case and a refusal for a value that is not a value; nothing that decides a
policy is a setting, because a policy is not Nightseam's to offer; and what
a peer settled on is readable back, so that a consumer and a reader of the
wire agree on what it was. Most consumers set nothing and get a working
thing; the ones that must set something find one knob, named for what it
bounds.

## The dimensions

- **Coverage of bounds.** Whether any bound is fixed that a consumer would
  need to set — a queue depth, a frame size, a window, a deadline, a count
  of anything — and whether any bound exists that no consumer could
  reasonably want to change, which is a knob for its own sake.
- **Uniformity.** Whether each bound is one name, one meaning and one
  default across languages, so that a consumer who set it in one knows
  what to set in another; a unit that differs by idiom is fine, a meaning
  that differs is not.
- **Absence of policy.** Whether any setting is a policy in disguise — a
  mode, a flag that changes what a thing does rather than how much of it,
  a preference between two consumers' worlds. Each is a decision the tree
  made for a consumer under the appearance of leaving it open.
- **Defaults.** Whether the default is the common case, so that setting
  nothing is the right choice for most; and whether a default is the same
  in every language.
- **Legibility and refusal.** Whether what was settled is readable back;
  whether a value that is no value — negative, absent where required, of
  the wrong shape — is refused where it is given rather than defaulted in
  silence.

## What it yields to

The boundary, entirely: a knob that would decide a policy is refused
however often it is asked for, and the consumer that wants the policy
builds it outside and calls in. Agnosticism, in one respect: a bound is
expressed in each language's own unit and shape — a duration where the
language has one, an integer where it does not — and parity is of name
and meaning, not of spelling.

## What it is not

Not flexibility, and not the number of knobs: the goal counts bounds, and
a tree with fewer settings each of which is a bound is more configurable
than one with many of which some are modes. Not feature flags, not a
configuration file, not profiles of settings. Not a way to change what a
peer *is*: every setting bounds a mechanism that behaves the same at every
value of it.
