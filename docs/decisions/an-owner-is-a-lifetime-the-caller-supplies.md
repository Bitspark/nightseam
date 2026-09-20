# An owner is a lifetime the caller supplies

**The question.** A generated live value is a plain native function or a
record of functions. Who releases the bindings created while converting that
value, especially when one conversion reuses an attachment another already
holds or fails after acquiring only part of a value?

**Decided.** Give conversion a caller-chosen owner inside the connection's
scope. The scope supplies a root owner; owners have children, and releasing
one releases its children first, then its own allocations. An owner owns each
fresh export and each newly created import attachment. Reusing an attachment
borrows it: releasing the borrower leaves it usable, while releasing its
allocating owner invalidates all its aliases. Release is idempotent and a
binding-wide barrier, not cancellation or reference counting.

Generated helpers take an owner. Generated calls and events carrying live
values select one from the Go context or TypeScript options, defaulting to the
scope's root. Their handlers and callable implementations receive a child
owner; returned functions are exported under it. The implementation can keep
that owner and release it later. Finishing the invocation does not dispose of
its live values, and values gain no wrapper or native identity registry.

An import or export conversion is a synchronous batch under the owner. It
passes an active owner view through nested conversions and unwinds only fresh
allocations on failure, preserving aliases it borrowed. An intrinsically live
generic helper passes that view into parameter converters in both directions;
a generic data helper stays independent of the live runtime and its caller
closes converters over the view. The wire remains unchanged. The
[runtime surface](../runtime/live.md#choosing-a-lifetime) and
[generated surface](../declaration/generated.md#live-values) give the concrete
names in both languages.

**Why.** Raw-reference release alone leaves a consumer of generated native
functions with no handle for the lifetime it chose: conversion has hidden the
references. Requiring the connection to close would keep every temporary
binding until the longest-lived use ends. A disposal wrapper around every
value would change the plain-function projection, and discovering references
by native identity would make aliases and separately exported copies depend
on host-language identity rules.

An owner names the missing local lifetime without assigning meaning to a
consumer's job, subscription or record. Exclusive allocation ownership also
keeps a borrowed alias from silently extending a binding's lifetime. Reference
counting would promise a different release rule, while re-parenting would need
a transfer rule that the demonstrated cases do not require. Fresh-only batch
rollback prevents partial conversions from consuming bounds forever without
revoking attachments an earlier successful conversion still uses. The caller
still has to choose and end a lifetime; the library does not infer that choice
from RPC completion or garbage collection.

**Serves.** [Boundary](../goals/boundary.md): the caller supplies the lifetime's
meaning, and the runtime supplies uniform bookkeeping.
[Composability](../goals/composability.md): nested lifetimes and transactional
conversion compose while values remain ordinary functions and records.

**Since.** 0.5.0, [#257's verdict](https://github.com/Bitspark/nightseam/issues/257#issuecomment-5752853362),
implemented by [#296](https://github.com/Bitspark/nightseam/issues/296).
