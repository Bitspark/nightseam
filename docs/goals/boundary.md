# Boundary

## At the limit

Nightseam holds exactly what is the same for every consumer of a component's
declared profile and can be stated in that profile and the declaration —
mechanism — and nothing that names a concept of a consumer's or decides a policy. Every
policy is outside and calls in; every mechanism is inside and is called. No
consumer re-implements a mechanism, because the one inside is complete;
Nightseam decides no policy, because a policy is a consumer's fact about
its own world. The line is stated sharply enough that a change can say
which side it is on before it is made, and a change that cannot say is not
made.

Checking evidence against an explicitly selected authority profile is
mechanism; choosing trusted roots, issued authority, resource meanings and
current access policy is the consumer's. The resource owner supplies and
enforces those facts at the point of effect or disclosure. An optional
component preserves this line by its responsibility and dependency direction,
not merely by living in a different directory.

## The dimensions

- **What is inside that a consumer would decide differently.** A policy
  that leaked in: a rule about who may do what, for how long, in what order
  of preference; a default that is one consumer's answer and not the common
  case's; a lifecycle a consumer would have run otherwise. Each is a place
  where the tree decided for a consumer it has never met.
- **What is outside that every consumer builds the same way.** A mechanism
  that never came in: something each consumer writes, in the same shape,
  against the same surface, because the tree stopped one step short. The
  boundary is too tight there, and the sign is repetition across consumers,
  not a request from one.
- **How sharply the line is stated.** Whether a proposal can be classified
  by a reader who did not write it; whether the same question, asked twice,
  gets the same side. The question is asked in one written form,
  [admitting a concept](../admission.md), and the answers it has given are
  kept there, so that the next asking is measured against the last.
- **The shape of the crossing.** A consumer calls in with facts in its own
  terms — who it is, what it decided — and the mechanism takes them as
  given. Where a consumer must translate its concept into the tree's to be
  understood, or the tree must know a consumer's concept to act, the line
  is in the wrong place.

## What it yields to

Nothing: the boundary is what the other goals yield to. Where
configurability wants a knob that would decide a policy, the boundary
refuses the knob; where observability wants to count, sample or choose
where the facts go, the boundary leaves that to whoever watches; where
extensibility would open a point for a consumer to put a policy inside,
the boundary keeps the point for mechanism. A conflict between two other
goals is settled by asking which side of this line each answer is on.

## What it is not

Not minimalism: a mechanism every consumer needs belongs inside however
large it is, and leaving it out to keep the tree small is the second
failure above. Not a plugin architecture: policy calls in through a
surface, it is not loaded into the mechanism. Not a refusal to ship
defaults: a default is the mechanism's when it is right for the common
case, and a consumer that sets nothing should get a working thing. And not
a line between "core" and "extras": every component is inside the boundary
or it is not shipped.
