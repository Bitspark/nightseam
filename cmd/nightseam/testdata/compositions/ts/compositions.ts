// The same composition, in the other language, against the Go worker over a
// real socket. The argument is the URL the Go case started; the scope below
// is the TypeScript twin of scope_test.go, rule for rule, because parity is
// one suite and not two implementations that happen to agree.

import assert from 'node:assert/strict';
import { DuplexPeer, forwardWire } from '@nightseam/runtime';
import type { Wire } from '@nightseam/duplex';
import { Tunnel, type Channel } from '@nightseam/tunnel';
import { fromWire as workerFromWire, type Handle, type Held } from './api/ts/worker-binding/src/index.ts';
import { toWire as sinkToWire, type Ending, type ReportRequest } from './api/ts/sink-client/src/index.ts';
import { fromWire as jobFromWire } from './api/ts/job-binding/src/index.ts';

const url = process.argv[2];
assert.ok(url, 'the worker URL is the first argument');

/** What this side serves, and the one attachment it keeps per imported binding. */
interface Served {
  family: string;
  channel: Channel;
  close(): void;
}
interface Held_ {
  family: string;
  aliases: number;
  close(): void;
  client: unknown;
}

/** The live layer, composed — the rules of #202, as ordinary bookkeeping. */
class Scope {
  private readonly served = new Map<number, Served>();
  private readonly held = new Map<number, Held_>();
  private closed = false;
  private readonly carrier: Tunnel;

  constructor(carrier: Tunnel) {
    this.carrier = carrier;
  }

  /** Opens a channel, serves an implementation over it, answers the reference. */
  async export(family: string, model: Wire): Promise<number> {
    if (this.closed) {
      model.close();
      throw new Error('the scope is closed');
    }
    let channel: Channel;
    try {
      channel = await this.carrier.open(family, '', {
        prepare(peer) {
          forwardWire(peer.wire(), model);
          peer.onClose(() => model.close());
        },
      });
    } catch (error) {
      model.close();
      throw error;
    }
    this.served.set(channel.id, {
      family,
      channel,
      close: () => {
        model.close();
        channel.close();
      },
    });
    return channel.id;
  }

  /** Resolves a reference this connection carried, once per binding. */
  async import<T>(id: number, family: string, interpret: (channel: Channel) => Promise<T>): Promise<T> {
    if (this.closed) throw new Error('the scope is closed');
    if (this.served.has(id)) throw new Error('the reference was minted here');
    const already = this.held.get(id);
    if (already) {
      if (already.family !== family) throw new Error('the channel speaks ' + already.family + ', not ' + family);
      already.aliases += 1;
      return already.client as T;
    }
    const channel = await this.carrier.channel(id, {});
    if (!channel) throw new Error('the reference names no channel of this connection');
    if (channel.family !== family) throw new Error('the channel speaks ' + channel.family + ', not ' + family);
    const client = await interpret(channel);
    this.held.set(id, { family, aliases: 1, close: () => channel.close(), client });
    return client;
  }

  /** Drops one alias; the last one closes the attachment and calls nothing. */
  release(id: number): boolean {
    const held = this.held.get(id);
    if (!held) return false;
    held.aliases -= 1;
    if (held.aliases > 0) return true;
    this.held.delete(id);
    held.close();
    return true;
  }

  aliases(id: number): number {
    return this.held.get(id)?.aliases ?? 0;
  }

  close(): void {
    this.closed = true;
    for (const served of this.served.values()) served.close();
    for (const held of this.held.values()) held.close();
    this.served.clear();
    this.held.clear();
  }
}

/** A sink an application supplies: the client side of the sink family. */
class Recorder {
  readonly items: number[] = [];
  ended = '';
  endings = 0;
  taken = 0;

  report(params: ReportRequest): number {
    this.taken += 1;
    this.items.push(params.sequence);
    return this.taken;
  }

  end(params: Ending): number {
    this.endings += 1;
    this.ended = params.how;
    return this.taken;
  }
}

const peer = new DuplexPeer();
const carrier = new Tunnel(peer);
const workerModel = await workerFromWire(peer.wire(), {});
const client = workerModel({ methods: {}, events: {} }).methods;
await peer.connect(url);
const scope = new Scope(carrier);

const exportSink = (sink: Recorder) =>
  scope.export(
    'sink',
    sinkToWire(() => ({
      methods: { report: params => sink.report(params), end: params => sink.end(params) },
      events: {},
    }), {}),
  );
const importJob = (id: number) =>
  scope.import(id, 'job', async channel => {
    const factory = await jobFromWire(channel, {});
    return factory({ methods: {}, events: {} }).methods;
  });

const settle = async (why: string, holds: () => boolean) => {
  for (let waited = 0; waited < 600; waited += 1) {
    if (holds()) return;
    await new Promise(resolve => setTimeout(resolve, 5));
  }
  throw new Error('never became true: ' + why);
};

// 1. A supplied callback and a returned callable, across the wire: the
// TypeScript side serves the sink, the Go worker calls it, and the reference
// it answers with is a TypeScript client of a Go implementation.
const sink = new Recorder();
const reference = await exportSink(sink);
const started = await client.start({ ticket: { label: 'typescript', steps: 4 }, progress: { channel: reference } });
assert.equal(started.accepted, true);
const job = await importJob(started.job.channel);

await settle('four reports and one ending', () => sink.ended === 'done' && sink.endings === 1);
assert.deepEqual(sink.items, [1, 2, 3, 4], 'the sink was told out of order');
const status = await job.status({});
assert.equal(status.state, 'done');
assert.equal(status.delivered, 4);

// 2. A wrong contract, a reference minted here, and one that names nothing.
// The reference has to be live for the first of these to mean anything, so it
// comes before the release below rather than after it.
await assert.rejects(
  scope.import(started.job.channel, 'sink', async () => {
    throw new Error('wrong contract reached the interpreter');
  }),
  /speaks job/,
);
await assert.rejects(importJob(reference), /minted here/);
await assert.rejects(importJob(4242), /names no channel/);

// 3. Aliasing and release, by the same rules as the Go scope.
const again = await importJob(started.job.channel);
assert.equal(again, job, 'two imports of one reference gave two attachments');
assert.equal(scope.aliases(started.job.channel), 2);
assert.equal(scope.release(started.job.channel), true);
await job.status({});
assert.equal(scope.release(started.job.channel), true);
assert.equal(scope.aliases(started.job.channel), 0);
await assert.rejects(async () => job.status({}), 'a released reference still answered');
assert.equal(scope.release(started.job.channel), false);

// 4. A wrong contract on the wire: the Go worker refuses a job reference
// where a sink belongs, by the code the family declares.
await assert.rejects(
  async () => client.start({ ticket: { label: 'miscast', steps: 1 }, progress: started.job }),
  (error: unknown) => (error as { code?: string }).code === 'unknown_reference',
);

// 5. Every position the language admits a reference in, sent from
// TypeScript and resolved by Go.
const references: Handle[] = [];
for (let made = 0; made < 6; made += 1) {
  references.push({ channel: await exportSink(new Recorder()) });
}
const held: Held = {
  one: references[0]!,
  many: [references[1]!, { channel: 9999 }],
  by_name: { a: references[2]! },
  maybe: references[3]!,
  either: { kind: 'sink', value: references[4]! },
  inline: { held: references[5]!, label: 'inline' },
};
const counted = await client.collect(held);
assert.equal(counted.resolved, 6, 'the worker resolved ' + counted.resolved + ' references');
assert.equal(counted.refused, 1, 'the worker refused ' + counted.refused + ' inventions');

// 6. The job's own cancellation, which is not the release of a reference and
// not the cancellation of a call: the sink is ended cancelled, or the job had
// already finished and says so.
const cancellable = new Recorder();
const cancellableReference = await exportSink(cancellable);
const long = await client.start({ ticket: { label: 'cancel', steps: 16 }, progress: { channel: cancellableReference } });
const longJob = await importJob(long.job.channel);
const cancelled = await longJob.cancel({});
await settle('the cancelled job ended its sink once', () => cancellable.ended !== '' && cancellable.endings === 1);
assert.equal(cancelled.stopped, cancellable.ended === 'cancelled', 'the answer and the sink disagree about the ending');

// 7. A call cancelled is that call and nothing else: the abort reaches the
// worker, the call rejects, and every reference this side holds is still
// held.
const aborting = new AbortController();
const slow = client.slow({ label: 'slow', steps: 1 }, { signal: aborting.signal });
const rejected = assert.rejects(Promise.resolve(slow));
aborting.abort();
await rejected;
assert.equal(scope.aliases(long.job.channel), 1, 'cancelling a call released a reference');
assert.equal((await longJob.status({})).state.length > 0, true, 'cancelling a call reached the job');

scope.close();
peer.close();
console.log('compositions across the wire: ok');
