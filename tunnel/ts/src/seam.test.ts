// A channel is a connection of the seam, and held to it: the suite duplex/ts
// holds its own transports to, run over a channel of a tunnel over a pipe, as
// tunnel/go runs duplextest over a channel. What the tunnel adds to the seam
// — the window, the ids, the families, the events — is tunnel.test.ts's; this
// file asks only what every transport is asked.
import { pipe } from '@nightseam/duplex';
import { DuplexPeer } from '@nightseam/runtime';
import { run } from '@nightseam/duplex/conformance';
import { Tunnel } from './index.ts';

run('a tunnel channel', async () => {
  const [left, right] = pipe();
  const client = new DuplexPeer({ role: 'client' });
  const server = new DuplexPeer({ role: 'server' });
  await Promise.all([client.attach(left), server.attach(right)]);
  const accepted = new Tunnel(server).acceptConnection();
  const opened = await new Tunnel(client).openConnection('probe');
  return {
    a: opened,
    b: await accepted,
    end: () => {
      client.close();
      server.close();
    },
  };
});
