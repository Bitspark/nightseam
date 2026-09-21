import { DuplexPeer, DuplexError } from '@nightseam/runtime';
import { encodePath } from '@nightseam/duplex';
import * as current from '@example/browser-identity-binding';
import * as previous from '@previous/browser-identity-binding';

interface Row {
  name: string;
  nativeSocket: boolean;
  selected: string;
  outcome: string;
  message?: string;
  earlyFrames: number;
  beforeBind: number;
  reverseCalls: number;
  events: number;
}

const output = document.querySelector<HTMLPreElement>('#results')!;
const status = document.querySelector<HTMLHeadingElement>('#status')!;
const report: { userAgent: string; rows: Row[]; failure?: string } = {
  userAgent: navigator.userAgent,
  rows: [],
};

async function until(test: () => boolean, label: string): Promise<void> {
  const deadline = Date.now() + 5000;
  while (!test()) {
    if (Date.now() >= deadline) throw new Error(`${label}: delivery deadline`);
    await new Promise(resolve => setTimeout(resolve, 1));
  }
}

try {
  if (String(current.wireDigest) === String(previous.wireDigest)) throw new Error('generated revisions have the same digest');
  for (const route of ['prepare', 'from']) {
    for (const protocol of ['none', 'ticket']) {
      for (const mode of ['match', 'mismatch', 'absent', 'digestless']) {
        const name = `${route}-${mode}-${protocol}`;
        const adapter = mode === 'mismatch' ? previous : current;
        let socket: WebSocket | undefined;
        const offered = protocol === 'ticket' ? ['ticket.fixture'] : [];
        const frames = new Set<string>();
        let reverseCalls = 0, events = 0;
        const receiver = {
          methods: { reverse(input: { value: string }) { reverseCalls++; return input.value; } },
          events: { ready(input: { value: string }) {
            if (input.value !== name) throw new Error(`${name}: wrong early event`);
            events++;
          } },
        };
        const peer = new DuplexPeer({
          subprotocols: offered,
          requestTimeoutMs: 5000,
          webSocketFactory: (url, protocols) => {
            // Observe the real frames without consuming or replacing the native socket.
            socket = new WebSocket(url, protocols);
            socket.addEventListener('message', event => {
              const frame = JSON.parse(String(event.data));
              if (frame.kind === 'request' && frame.method === encodePath(['reverse'])) frames.add('reverse');
              if (frame.kind === 'event' && frame.event === encodePath(['ready'])) frames.add('ready');
            });
            return socket;
          },
        });
        const context = { options: { requestTimeoutMs: 10000 } };
        const preparation = route === 'prepare' ? adapter.prepareFromWire(peer.wire(), context) : undefined;
        try {
          await peer.connect(`${location.origin.replace('http', 'ws')}/socket/${name}`);
          if (preparation) {
            // Both frames arrive while the generated receiver has no model at all.
            await until(() => frames.size === 2, name);
            if (reverseCalls + events !== 0) throw new Error(`${name}: model ran before identity checking`);
          }
          let refusal: unknown;
          let factory: current.ServerModel | undefined;
          try {
            factory = preparation
              ? await preparation.complete({ timeoutMs: 5000 })
              : await adapter.fromWire(peer.wire(), context);
          } catch (error) {
            refusal = error;
          }
          const row: Row = {
            name,
            nativeSocket: socket instanceof WebSocket,
            selected: socket!.protocol,
            outcome: 'accepted',
            earlyFrames: frames.size,
            beforeBind: reverseCalls + events,
            reverseCalls,
            events,
          };
          if (row.selected !== (offered[0] ?? '')) throw new Error(`${name}: consumer subprotocol changed`);
          if (row.beforeBind !== 0) throw new Error(`${name}: model ran before factory binding`);
          if (mode === 'mismatch') {
            if (!(refusal instanceof DuplexError) || refusal.code !== 'contract_mismatch' || factory !== undefined) {
              throw new Error(`${name}: mismatch exposed a factory or returned ${String(refusal)}`);
            }
            if (refusal.message !== 'the declaration identity for browser-identity differs') {
              throw new Error(`${name}: mismatch lost its exact diagnostic`);
            }
            row.outcome = refusal.code;
            row.message = refusal.message;
          } else {
            if (refusal !== undefined) throw refusal;
            if (!factory) throw new Error(`${name}: no model factory`);
            const model = factory(receiver);
            if (preparation) await until(() => reverseCalls === 1 && events === 1, name);
            const answer = await model.methods.echo({ value: name });
            if (answer !== name) throw new Error(`${name}: generated application call failed`);
          }
          row.reverseCalls = reverseCalls;
          row.events = events;
          report.rows.push(row);
        } finally {
          preparation?.close();
          peer.close();
        }
      }
    }
  }
  status.textContent = 'PASS: 16 generated native-browser identity cases';
} catch (error) {
  report.failure = error instanceof Error ? `${error.name}: ${error.message}` : String(error);
  status.textContent = 'FAIL: generated browser identity interpretation';
}
output.textContent = JSON.stringify(report, null, 2);
await fetch('/result', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(report) });
