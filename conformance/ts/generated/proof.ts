import * as proof from './api/ts/proof-client/src/index.ts';
import * as binding from './api/ts/proof-binding/src/index.ts';
import * as probe from './api/ts/probe-client/src/index.ts';
import { validateUnicodeJSON, type TypeExpression } from '@nightseam/runtime';
import { Served } from './server.ts';

type Args = Record<string, unknown>;
type Client = proof.Client<probe.Family, string>;
export class ProofFailure extends Error {
  readonly code: string;
  constructor(code: string, message: string) { super(message); this.code=code; }
}
class Dialled {
  client!: Client;
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
const servers = new Map<string, Served<binding.Remote<probe.Family, string>>>();
let next = 0;
export function resetProof(): void {
  for (const d of handles.values()) d.client.close();
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
const handler: binding.Handler<probe.Family, string> = {
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
    const s = await new Served(socket => binding.serve<probe.Family, string>(socket, probe.family, slots.Item, {}, handler, {}).then(peer => new binding.Remote<probe.Family, string>(peer, probe.family, slots.Item))).listen();
    const handle = `proofsrv${++next}`;
    servers.set(handle, s);
    return { handle, url: s.url };
  },
  'server.proof_emit': async args => {
    const s = servers.get(String(args.on));
    if (!s) throw new ProofFailure('unknown_handle', String(args.on));
    const remote = await s.remote(typeof args.within_ms === 'number' ? args.within_ms : 5000);
    if (!remote) throw new ProofFailure('timeout', 'no proof client');
    await remote.emitPartAdded(args.data as proof.RichPart);
    return {};
  },
  'gen.proof_dial': async args => {
    const d = new Dialled();
    d.client = await proof.Client.dial<probe.Family,string>(String(args.url), probe.family, slots.Item, {}, undefined, {partAdded:data=>d.receive(data)});
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
      if (args.raw) result = await client.peer.call(String(args.method), args.params, options);
      else switch (args.method) {
        case 'classify': result = await client.classify(args.params as proof.Part, options); break;
        case 'classify_rich': result = await client.classifyRich(args.params as proof.RichPart, options); break;
        case 'parts': result = await client.parts(args.params as proof.PartsRequest, options); break;
        case 'relay': result = await client.relay(args.params as proof.Carried<probe.Family,string>, options); break;
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
