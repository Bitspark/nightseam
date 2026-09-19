# The session runs over any connection of the seam

**The question.** A session's up and down sides were channels of a tunnel.
Must they be, or is a tunnel one way among several to give a relay a
connection?

**Decided.** Each side of a session is a connection of the seam — a tunnel
channel, the seam's pipe, a bare socket: the relay sends on it, receives
from it and closes it, and asks nothing about what multiplexed it. The
tunnel is the answer where one connection carries many sessions and is no
requirement where it carries one. The suite is run twice in each language,
once over a tunnel's channels and once over the pipe with no tunnel at all.
[The session](../wire/session.md).

**Why.** A relay that required a tunnel required a peer beneath the tunnel
and a socket beneath the peer for a machine that runs in the same process
as the registry and already speaks the profile over a pipe — three layers
to reach a thing one layer away. Taking the seam instead of the tunnel is
what makes an in-process machine bind the pipe it has and a consumer over
a bare socket attach its own, and it cost the relay nothing: it never used
anything a channel has that a connection of the seam lacks. Running the
suite over both is what makes "the transport is none of the relay's
business" a fact the tree holds rather than a sentence.

**Serves.** Composability — a part runs over any part beneath it and knows
nothing of what assembled them.

**Since.** #48.
