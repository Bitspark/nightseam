// The same suite over the seam's own pipe, with no tunnel anywhere: a relay
// sends on the connection, receives from it and closes it, and what
// multiplexed it — nothing, here — is none of its business.
import { connections, run } from './conformance.ts';

run(connections);
