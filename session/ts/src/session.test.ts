// The session component over the connections of the seam: the suite, once
// over the channels of a tunnel and once over the pipes themselves with no
// tunnel anywhere, and the two things only a bare connection can be asked —
// a session mixing transports, and one observed through the registry because
// what it is bound over observes through nothing of its own.
import { connections, pipes, run } from './conformance.ts';
import './seam.test.ts';

run(pipes);
run(connections);
