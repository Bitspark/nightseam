# Goals

The north stars. A goal page says what Nightseam is *for*, in one respect,
at the limit — what fully achieving it would look like — so that the tree
can be measured against it at any point, and found short, and moved. The
tree is never done with a goal; a goal is a direction, not a state.

That is the difference between these pages and every other page under
`docs/`. The state pages (`wire/`, `runtime/`, `declaration/`,
`languages/`) say what the tree is; the record (`decisions/`) says what it
was chosen over and why; a goal page says where it is going, and names
nothing in the tree — because a specific example in a goal hides the
concept. A reviewer reading "a channel is a connection of the seam" checks
that it is and stops; a reviewer reading "every part stacks on any part
beneath it without knowing what carried it" asks where else something is
carried, and finds the places the example would have hidden.

## The two tests

A goal is written specific to the domain — a duplex profile, families
declared once, peers in several languages, layers over one seam — and
agnostic to the solution. Two tests bound the level, sentence by sentence:

- **If a sentence could go unchanged into another repository's goal page,
  it is not yet this repository's goal.** "We value composability" says
  nothing a reviewer can walk the tree with.
- **If a sentence names something in the tree — a package, a type, a file,
  a test, an option — it describes today, not the direction.** It belongs
  in a state page, and the goal page says the thing the name is an instance
  of.

A page passes when a reviewer can walk the tree with it and find things it
does not mention.

## What a page holds

Four parts, in this order:

1. **The goal at its limit** — what fully achieving it would look like,
   stated so that *more* and *less* of it are distinguishable.
2. **The dimensions** — the axes along which the tree can have more or
   less of it, so that a review has directions rather than a checkbox.
3. **What it yields to** — the goals it is in tension with, and which way
   the tension resolves here, so that a review against one goal does not
   propose spending another.
4. **What it is not** — the near-miss readings to rule out.

A page changes when the understanding of the goal deepens, never when the
tree moves. Its stability is what makes one review comparable with the
next.

## The review

A review takes one goal page and the tree, and answers three questions:
where does the tree fall short of the goal, how could it get closer, and
what would that cost. It is asked for in one sentence — *review the tree
against `docs/goals/<goal>.md`* — and returns:

- **Findings**, ordered by distance from the goal: each names the place in
  the tree, the dimension it falls short on, and what the goal would have
  instead. The specifics belong here, in the review's output, never in the
  goal.
- **Proposals**, at whichever of three levels the finding needs —
  **concept** (a model the tree lacks or gets wrong), **architecture**
  (what belongs where), **implementation** (how a thing is done) — each
  with its cost and the other goal it presses on.
- What it read in the record. A review reads [decisions](../decisions/)
  before proposing: a proposal that reopens a decision says so and argues
  against the reason recorded there, and a proposal that repeats one is
  withdrawn before it is made.

A finding whose answer is the operator's — which way a design goes, what a
language promises, what the model says — becomes a design issue in the
*Design* form, with the goal page as its provenance; the verdict is copied
onto it, becomes a decision page when it settles something, and lanes are
cut from it ([COLLABORATION.md](../../COLLABORATION.md), *Asking and
reporting*). A finding whose answer is plain is a lane. A review is never
a change: it produces the issues a change is cut from.

Reviews earn most while a rewrite is cheap — a finding at the concept level
can still be executed rather than filed — and the pages themselves, naming
no mechanism, are the part of Nightseam that a successor inherits verbatim.

## The goals

| goal | at the limit |
| --- | --- |
| [boundary](boundary.md) | Nightseam holds exactly what is the same for every consumer; every policy and every concept of a consumer's is outside and calls in |
| [layering](layering.md) | each layer speaks the one beneath and knows nothing of those above; everything on the wire belongs to exactly one layer |
| [composability](composability.md) | every part is usable alone, stacks on any part beneath, and is replaced without any part beside it knowing |
| [agnosticism](agnosticism.md) | Nightseam is of no host, no transport, no backend and no language; each language is one realization of a thing defined outside all of them |
| [declarative](declarative.md) | what two of anything would each hold a copy of is stated once as data all of them read, and a stale derivation is a failure |
| [configurability](configurability.md) | every bound is the consumer's to set under one name and meaning everywhere, and nothing that decides a policy is a setting |
| [extensibility](extensibility.md) | a language, a target, a transport, a layer, an adapter joins at one named point by adding, and nothing that exists moves |
| [observability](observability.md) | a consumer can answer, after the fact, any question about what a peer did, at every layer, in one order, from one place, without seeing what was said |

Candidates a first round of reviews may add rather than this page
anticipating: minimality — the profile refuses what it does not know so
that the set stays small; verifiability — no promise without a test that
holds it.
