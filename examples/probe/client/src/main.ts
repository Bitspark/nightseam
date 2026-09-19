// The probe family's client side: dial the server, answer what it calls
// back, hear what it emits. The generated client is in api/ts/probe-client
// and is never edited; the handler the server calls is written here, the
// way the repository's README writes it.
import { argv, env } from 'node:process';
import { Client, type Payload } from '../../api/ts/probe-client/src/index.ts';

const url = argv[2] ?? env.PROBE_URL ?? 'ws://127.0.0.1:8080/probe';

// The server calls the client inside the request it is serving: this is
// the other half of duplex, and it is typed like the first half.
// The event handler is part of construction, ready even if the server emits
// before dialing returns.
let onChanged!: (payload: Payload) => void;
const changed = new Promise<Payload>(resolve => {
  onChanged = resolve;
});
const client = await Client.dial(url, {}, {
  reverse: ({ text, count }: Payload): Payload => ({ text: [...text].reverse().join(''), count }),
}, { changed: onChanged });

const result = await client.echo({ text: 'hello', count: 1 });

console.log('echo    ->', result.text);
console.log('changed ->', (await changed).text);

client.close();
