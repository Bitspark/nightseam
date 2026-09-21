// The probe family's client side: dial the server, answer what it calls
// back, hear what it emits. The generated client is in api/ts/probe-client
// and is never edited; the handler the server calls is written here, the
// way the repository's README writes it.
import { argv, env } from 'node:process';
import { prepareFromWire, type Payload } from '../../api/ts/probe-binding/src/index.ts';
import { DuplexPeer } from '@nightseam/runtime';
import { liveOver, valueEnvironment } from '@nightseam/live';

const url = argv[2] ?? env.PROBE_URL ?? 'ws://127.0.0.1:8080/probe';

// The server calls the client inside the request it is serving: this is
// the other half of duplex, and it is typed like the first half.
// Preparation holds early events until the identity check completes and the
// model binds its handlers, even if the server emits before dialing returns.
let onChanged!: (payload: Payload) => void;
const changed = new Promise<Payload>(resolve => {
  onChanged = resolve;
});
const peer = new DuplexPeer();
const scope = liveOver(peer, {});
const prepared = prepareFromWire(peer.wire(), { valueEnvironment: valueEnvironment(scope) });
await peer.connect(url);
const model = await prepared.complete();
const client = model({
  methods: { reverse: ({ text, count }: Payload): Payload => ({ text: [...text].reverse().join(''), count }) },
  events: { changed: onChanged },
});

const result = await client.methods.echo({ text: 'hello', count: 1 });

console.log('echo    ->', result.text);
console.log('changed ->', (await changed).text);

// The third level. `notice` is written here as an ordinary function and
// handed over inside the request; the generated client exported it into the
// connection's live scope, so what the server received is a reference to this
// implementation and not a copy of anything. `stop` comes back the same way.
const notices: Payload[] = [];
const subscription = await client.methods.watch({
  label: 'demo',
  watcher: { notice: async (payload: Payload) => { notices.push(payload); } },
});

console.log('notice  ->', notices.at(-1)?.text);

// Calling it runs server code written before this line existed, which calls
// `notice` again — the reference this client handed over is still good after
// the call that carried it returned. That is what makes it live rather than a
// callback for the duration of one request.
await subscription.stop();

console.log('stopped ->', notices.at(-1)?.text);

prepared.close();
peer.close();
