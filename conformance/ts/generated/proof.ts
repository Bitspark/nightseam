import * as proof from './api/ts/proof-client/src/index.ts';
import * as binding from './api/ts/proof-binding/src/index.ts';
import * as probe from './api/ts/probe-client/src/index.ts';
import { callWire, validateUnicodeJSON, type TypeExpression } from '@nightseam/runtime';
import { Served, Session } from './server.ts';
import { jsonAdapter } from '@nightseam/live';

type Args = Record<string, unknown>;
type Client = proof.Server<probe.Family, string>;
export class ProofFailure extends Error {
  readonly code: string;
  constructor(code: string, message: string) { super(message); this.code=code; }
}
class Dialled {
  connection!: Session<Client>;
  get client(): Client { return this.connection.model; }
  readonly events: proof.RichPart[] = [];
  readonly waiters = new Set<() => void>();
  receive(data: proof.RichPart): void { this.events.push(data); for (const wake of this.waiters) wake(); }
  async event(within: number): Promise<proof.RichPart> {
    if (this.events.length) return this.events.shift()!;
    return new Promise((resolve, reject) => {
      const wake = () => { const value = this.events.shift(); if (value) { clearTimeout(timer); this.waiters.delete(wake); resolve(value); } };
      const timer = setTimeout(() => { this.waiters.delete(wake); reject(new ProofFailure('timeout', 'no proof event')); }, within);
      this.waiters.add(wake);
    });
  }
}
const handles = new Map<string, Dialled>();
const servers = new Map<string, Served<Session<proof.Client<probe.Family, string>>>>();
let next = 0;
export function resetProof(): void {
  for (const d of handles.values()) d.connection.close();
  for (const s of servers.values()) s.shutdown();
  handles.clear();
  servers.clear();
}
function lookup(args: Args): Dialled {
  const d = handles.get(String(args.on));
  if (!d) throw new ProofFailure('unknown_handle', String(args.on));
  return d;
}
const slots = { S: probe.family, Item: {type: 'string', validate: probe.validateWire} } satisfies proof.Slots;
const itemAdapter = jsonAdapter<string>(slots.Item);
// These references must compile against the names actually emitted by the target.
const inlineNames = {
  OptionNone: {} as proof.OptionNone,
  PartImage: {url:'https://example.org/image'} as proof.PartImage,
  PartsRequest: {} as proof.PartsRequest,
  RichPartTable: {rows:[]} as proof.RichPartTable,
};
function eventKind(value: proof.RichPart): string {
  switch (value.type) {
    case 'text': { const body: string = value.value.body; return body.length ? 'text' : 'invalid'; }
    case 'image': { const image: proof.PartImage = value.value; return image.url ? 'image' : 'invalid'; }
    case 'count': { const count: number = value.value; return Number.isInteger(count) ? 'count' : 'invalid'; }
    case 'table': { const table: proof.RichPartTable = value.value; return Array.isArray(table.rows) ? 'table' : 'invalid'; }
  }
}
function classify(value: proof.Part): string {
  switch (value.type) {
    case 'text': return 'text:' + value.value.body;
    case 'image': return 'image:' + value.value.url;
    case 'count': return 'count:' + String(value.value);
  }
}
const handler: proof.Server<probe.Family, string>['methods'] = {
  echo: params => params,
  noArgs: () => 'proof',
  seen: () => [],
  classify,
  classifyRich: params => params.type === 'table' ? 'table:' + String(params.value.rows.length) : classify(params),
  parts: () => ({ kind: 'ok', value: { items: [
    { type: 'text', value: { type: 'text', body: 'hello 😀 �' } },
    { type: 'count', value: 7 },
    { type: 'image', value: { url: 'https://example.org/image', alt: null } },
  ] } }),
  relay: params => ({ kind: 'some', value: params.message }),
};
export const proofOps: Record<string, (args: Args) => unknown | Promise<unknown>> = {
  'gen.proof_serve': async () => {
    const s = await new Served(socket => {
      const connection = new Session<proof.Client<probe.Family, string>>({ role: 'server' });
      connection.expose(binding.toWire<probe.Family, string>(remote => {
        connection.model = remote;
        return { methods: handler, events: {} };
      }, {}, probe.family, itemAdapter));
      return connection.attach(socket);
    }).listen();
    const handle = `proofsrv${++next}`;
    servers.set(handle, s);
    return { handle, url: s.url };
  },
  'server.proof_emit': async args => {
    const s = servers.get(String(args.on));
    if (!s) throw new ProofFailure('unknown_handle', String(args.on));
    const remote = await s.remote(typeof args.within_ms === 'number' ? args.within_ms : 5000);
    if (!remote) throw new ProofFailure('timeout', 'no proof client');
    await remote.model.events.partAdded(args.data as proof.RichPart);
    return {};
  },
  'gen.proof_dial': async args => {
    const d = new Dialled();
    d.connection = new Session<Client>();
    d.connection.expose(proof.toWire<probe.Family, string>(remote => {
      d.connection.model = remote;
      return { methods: {}, events: { changed: () => {}, partAdded: data => d.receive(data) } };
    }, {}, probe.family, itemAdapter));
    await d.connection.connect(String(args.url));
    const handle = `proofcl${++next}`; handles.set(handle, d); return {handle};
  },
  'gen.proof_names': () => Object.keys(inlineNames).sort(),
  'gen.proof_validate': args => {
    try {
      const expression = JSON.parse(String(args.type)) as TypeExpression;
      let value = args.value;
      if (typeof args.text === 'string') { validateUnicodeJSON(args.text); value = JSON.parse(args.text); }
      proof.validateWire(expression, value, '$', slots);
      return {valid:true};
    } catch (error) { return {valid:false,code:'invalid',message:error instanceof Error ? error.message : String(error)}; }
  },
  'client.proof_call': async args => {
    const {client} = lookup(args);
    const options = {timeoutMs:typeof args.within_ms === 'number' ? args.within_ms : 5000};
    try {
      let result: unknown;
      if (args.raw) result = await callWire(lookup(args).connection.peer.wire(), [String(args.method)], args.params, options);
      else switch (args.method) {
        case 'classify': result = await client.methods.classify(args.params as proof.Part, options); break;
        case 'classify_rich': result = await client.methods.classifyRich(args.params as proof.RichPart, options); break;
        case 'parts': result = await client.methods.parts(args.params as proof.PartsRequest, options); break;
        case 'relay': result = await client.methods.relay(args.params as proof.Carried<probe.Family,string>, options); break;
        default: throw new ProofFailure('invalid','unknown proof method');
      }
      return {result};
    } catch (error) {
      return {error:{code:error instanceof proof.DuplexError ? error.code : 'failed',message:error instanceof Error ? error.message : String(error)}};
    }
  },
  'client.proof_event': async args => {
    const data = await lookup(args).event(typeof args.within_ms === 'number' ? args.within_ms : 5000);
    return {data,kind:eventKind(data)};
  },
};
