import { DuplexPeer, DuplexError, checkIdentity } from '@nightseam/runtime';

interface Row {
  name: string;
  nativeSocket: boolean;
  selected: string;
  outcome: string;
  message?: string;
}

const output = document.querySelector<HTMLPreElement>('#results')!;
const status = document.querySelector<HTMLHeadingElement>('#status')!;
const report: { userAgent: string; rows: Row[]; failure?: string } = {
  userAgent: navigator.userAgent,
  rows: [],
};

try {
  for (const protocol of ['none', 'ticket']) {
    for (const mode of ['match', 'mismatch', 'absent', 'digestless']) {
      const name = `${mode}-${protocol}`;
      let socket: WebSocket | undefined;
      const offered = protocol === 'ticket' ? ['ticket.fixture'] : [];
      const peer = new DuplexPeer({
        subprotocols: offered,
        requestTimeoutMs: 3000,
        webSocketFactory: (url, protocols) => {
          // This is the browser's native constructor, with no WebSocket shim.
          socket = new WebSocket(url, protocols);
          return socket;
        },
      });
      try {
        await peer.connect(`${location.origin.replace('http', 'ws')}/socket/${name}`);
        let refusal: unknown;
        try {
          await checkIdentity(peer.call.bind(peer), {
            path: 'browser.identity',
            digest: (mode === 'mismatch' ? 'b' : 'a').repeat(64),
          });
        } catch (error) {
          refusal = error;
        }
        const row: Row = {
          name,
          nativeSocket: socket instanceof WebSocket,
          selected: socket!.protocol,
          outcome: 'accepted',
        };
        if (row.selected !== (offered[0] ?? '')) throw new Error(`${name}: consumer subprotocol changed`);
        if (mode === 'mismatch') {
          if (!(refusal instanceof DuplexError) || refusal.code !== 'contract_mismatch') {
            throw new Error(`${name}: expected contract_mismatch, received ${String(refusal)}`);
          }
          if (refusal.message !== 'the declaration identity for browser.identity differs') {
            throw new Error(`${name}: mismatch did not preserve its exact diagnostic`);
          }
          row.outcome = refusal.code;
          row.message = refusal.message;
          // No application method runs after the identity refusal.
        } else {
          if (refusal !== undefined) throw refusal;
          const answer = await peer.call('application.echo', { value: name });
          if (answer !== name) throw new Error(`${name}: application call failed after identity acceptance`);
        }
        report.rows.push(row);
      } finally {
        peer.close();
      }
    }
  }
  status.textContent = 'PASS: 8 native-browser identity cases';
} catch (error) {
  report.failure = error instanceof Error ? `${error.name}: ${error.message}` : String(error);
  status.textContent = 'FAIL: browser identity exchange';
}
output.textContent = JSON.stringify(report, null, 2);
await fetch('/result', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(report) });
