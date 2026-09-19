# No subprotocol by default

**The question.** A WebSocket handshake may negotiate a subprotocol. Does
the profile name itself there, and does a peer require it?

**Decided.** The profile names itself as none: a peer offers nothing by
default, selects nothing by default, refuses nothing on that ground, and
reads nothing into what was selected. A consumer may name something there
— a token a gateway discriminates on, a browser's ticket — through the
transport's own surface, and a server's selection may be a function of the
request. [The profile](../wire/profile.md#the-subprotocol) and [the
peer](../runtime/peer.md#the-subprotocol).

**Why.** `nightseam.duplex/1` is what an endpoint speaks, not a token on the
wire, and a peer that required its own name there would break every
deployment behind a server that selects none. The trap is the browser's: a
client that offers a subprotocol must be met by a server that selects one
of them, or the browser refuses the connection — so offering and selecting
are one decision, taken on both sides together, and a default that offered
would have made every deployment take it whether it meant to or not. The
ticket case is real and is why the surface exists at all: a browser cannot
set a header on an upgrade, so a ticket has nowhere else to travel, and it
is not a list, so the selection had to be a function of the request rather
than a fixed set.

**Serves.** Agnosticism — a peer assumes nothing about what carries it or
what stands between it and the other side.

**Since.** 0.3.0.
