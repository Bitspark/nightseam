# Close codes are the WebSocket registry's

**The question.** The seam closes explicitly with a code and a reason, and
a WebSocket is one transport of it among several — a pipe, a tunnel
channel. Whose numbers are the codes?

**Decided.** The WebSocket registry's, on every transport: 1000 normal,
1001 going away, 1002 protocol error, 1003 unsupported data, 1006 abnormal
closure, 1008 policy violation, 1009 too large, 1011 internal, and
4000–4999 for what runs above the seam, of which the profile takes 4011 for
the other side having broken it. [The
profile](../wire/profile.md#the-connection-beneath).

**Why.** A close means the same thing whatever carried it, so a consumer
reading one does not ask which transport it came over — and a layer that
runs over any connection of the seam, as a peer does, ends its
connections with codes that mean the same on a channel as on a socket. A
registry of the seam's own would have been a second table to keep in every
language and a translation at every transport boundary, for numbers that
already exist and that every proxy and browser between two peers already
acts on. The 4000-range is what leaves room for the layers without
contesting the registry.

**Serves.** Composability — a transport is substituted without anything
above it knowing.

**Since.** 0.2.0, with the seam.
