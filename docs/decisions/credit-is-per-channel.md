# Credit is per channel

**The question.** Channels are multiplexed over one peer, and a channel's
consumer may stall. Who is paced, and whose buffer holds what a stalled
channel has not taken?

**Decided.** Flow control is per channel, by credit: each side may have at
most a window of frames in flight to the other on a channel — the window
the other side declared at open — and a receiver returns credit as its
consumer takes frames, in halves of the window. A send beyond the window
waits; a frame beyond it ends the channel. What arrives before anyone
receives is held one window deep and no deeper. [The
tunnel](../wire/tunnel.md#credit).

**Why.** The outer peer ends a connection whose events it cannot deliver
([queues are paced](queues-are-paced-for-one-deadline.md)), so a stalled
channel may never be the outer peer's to buffer: it would stall every other
channel on the connection and then end the connection for all of them.
Per-channel credit makes a stalled channel stall its own sender and nothing
else. The window is declared by the receiver at open because the receiver
is the one that will hold the frames; returning credit in halves rather
than per frame is what keeps the credit traffic a fraction of the frame
traffic.

**Serves.** Composability — one channel's behaviour is invisible to the
others and to the peer beneath.

**Since.** 0.2.0, with the tunnel.
