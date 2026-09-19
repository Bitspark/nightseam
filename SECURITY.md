# Security

## Reporting

Report a vulnerability privately, through GitHub's *Report a vulnerability*
on this repository's Security tab, and not in an issue or a pull request.
Say what the flaw is, where it is, and how to observe it; a reproduction over
the in-memory pipe or a local socket is enough. You will hear back within
five working days with whether it is confirmed and what the fix will be.

## Scope

The runtimes — `duplex`, `runtime`, `tunnel`, `session` and the `otel` adapter, in both languages —
carry frames between parties who may not trust each other: a consumer, a
relay, a machine. What they promise on that front is stated in their
documentation and held by their suites: a frame over the limit is refused
before delivery; a malformed frame ends the connection; a stalled consumer is
disconnected rather than allowed to hold up the rest; an observer never
decides; what a consumer sends reaches the machine under the session's own
ids; a member of a frame the relay does not know travels verbatim. A way to
break one of those is a security report.

The generator is developer tooling that runs on a checkout's own files; a
flaw in it is a bug unless it lets a declaration reach a place outside the
directories it owns.

Authentication, authorization and the durability of what is logged are the
consumer's, by the boundary rule; a flaw in a consumer's use of these
packages is reported to that consumer.

## Supported versions

Nightseam is pre-1.0. The latest minor release is supported; a fix ships as
a new release rather than a patch to an old one.
