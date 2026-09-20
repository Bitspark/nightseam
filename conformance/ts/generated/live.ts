/**
 * The live half of the TypeScript generated testee: what the generator
 * renders for the worker family, driven over the live ops of
 * conformance/DRIVER.md.
 *
 * Nothing here touches the live runtime. Every callable is an ordinary
 * function — one written here and handed across, one received and called —
 * which is the whole claim the live tier makes, and the reason this file can
 * be read as a consumer would write it.
 *
 * Both generated roles run here: a callable supplied by this peer is invoked
 * by the other, and one returned by the other is invoked here.
 */
import * as worker from './api/ts/worker-client/src/index.ts';
import * as binding from './api/ts/worker-binding/src/index.ts';
import { Served } from './server.ts';

type Args = Record<string, unknown>;

export class LiveFailure extends Error {
  readonly code: string;
  constructor(code: string, message: string) {
    super(message);
    this.code = code;
  }
  toJSON(): Record<string, unknown> {
    return { code: this.code, message: this.message };
  }
}

class LiveDialled {
  client!: worker.Client;
  /** What this peer's own callback was told, in the order it was told. */
  readonly reports: number[] = [];
  private readonly jobs = new Map<string, worker.Job>();
  private next = 0;

  /** A sink of this peer's own making: an ordinary function in a record. */
  sink(): worker.ProgressSink {
    return {
      report: async (percent: number) => {
        this.reports.push(percent);
      },
    };
  }

  hold(job: worker.Job): string {
    this.next += 1;
    const name = 'job' + String(this.next);
    this.jobs.set(name, job);
    return name;
  }

  job(name: string): worker.Job {
    const job = this.jobs.get(name);
    if (!job) throw new LiveFailure('invalid', 'no job ' + name);
    return job;
  }

  shutdown(): void {
    this.client.close();
  }
}

const handles = new Map<string, LiveDialled>();
class LiveServer implements binding.Handler {
  sink?: worker.ProgressSink;
  started = 0;
  readonly calls: string[] = [];

  describe(ticket: worker.Ticket): string { return ticket.label; }
  start: binding.Handler['start'] = async (params, _remote, context) => {
    await params.progress.report(50, { signal: context.signal });
    this.sink = params.progress;
    this.started += 1;
    return {
      ticket: params.ticket.id,
      cancel: async () => { this.calls.push('cancelled'); },
      rename: async ticket => { this.calls.push(ticket.label); return ticket; },
    };
  };
}
const servers = new Map<string, { served: Served<binding.Remote>; server: LiveServer }>();
let next = 0;

export function resetLive(): void {
  for (const d of handles.values()) d.shutdown();
  for (const s of servers.values()) s.served.shutdown();
  handles.clear();
  servers.clear();
}

function lookup(args: Args): LiveDialled {
  const d = handles.get(String(args.on));
  if (!d) throw new LiveFailure('unknown_handle', String(args.on));
  return d;
}

function servedOf(args: Args) {
  const s = servers.get(String(args.on));
  if (!s) throw new LiveFailure('unknown_handle', String(args.on));
  return s;
}

function callError(error: unknown): Record<string, unknown> {
  return { code: error instanceof worker.DuplexError ? error.code : 'failed', message: error instanceof Error ? error.message : String(error) };
}

/** The worker family's client side, which the server calls on this peer. */
const handler: worker.Handler = {
  async supervise(params: worker.Supervise): Promise<worker.Outcome> {
    for (const name of Object.keys(params.sinks).sort()) await params.sinks[name]!.report(1);
    return { state: 'finished' };
  },
};

export const liveOps: Record<string, (args: Args) => unknown | Promise<unknown>> = {
  'gen.live_serve': async () => {
    const server = new LiveServer();
    const served = await new Served(socket => binding.serve(socket, {}, server, {}).then(peer => new binding.Remote(peer))).listen();
    const handle = 'livesrv' + String(++next);
    servers.set(handle, { served, server });
    return { handle, url: served.url };
  },
  'gen.live_report_again': async args => {
    const sink = servedOf(args).server.sink;
    if (!sink) throw new LiveFailure('invalid', 'nothing was supplied to this server');
    try { await sink.report(100); return {}; }
    catch (error) { return { error: callError(error) }; }
  },
  'gen.live_seen': args => {
    const { server } = servedOf(args);
    return { started: server.started, calls: [...server.calls] };
  },
  'gen.live_supervise': async args => {
    const { served, server } = servedOf(args);
    const within = typeof args.within_ms === 'number' ? args.within_ms : 5000;
    const signal = AbortSignal.timeout(within);
    const remote = await served.remote(within);
    if (!remote) throw new LiveFailure('timeout', 'nobody is attached to this server');
    const sinks: worker.Sinks = Object.fromEntries((args.sinks as string[]).map(name => [name, {
      report: async (percent: number) => { server.calls.push(name + ':' + String(percent)); },
    }]));
    try {
      const outcome = await remote.supervise({ sinks }, { signal });
      return { state: outcome.state };
    } catch (error) { return { error: callError(error) }; }
  },
  'gen.live_dial': async (args: Args) => {
    const d = new LiveDialled();
    d.client = await worker.Client.dial(String(args.url), {}, handler, {});
    next += 1;
    const handle = 'livecl' + String(next);
    handles.set(handle, d);
    return { handle };
  },
  'client.live_describe': async (args: Args) => {
    const d = lookup(args);
    const label = await d.client.describe({ id: String(args.ticket), label: String(args.label) });
    return { label };
  },
  'client.live_start': async (args: Args) => {
    const d = lookup(args);
    const job = await d.client.start({
      ticket: { id: String(args.ticket), label: String(args.label) },
      progress: d.sink(),
    });
    return { job: d.hold(job), ticket: job.ticket };
  },
  'client.live_reports': (args: Args) => ({ values: [...lookup(args).reports] }),
  'client.live_cancel': async (args: Args) => {
    await lookup(args).job(String(args.job)).cancel();
    return {};
  },
  'client.live_rename': async (args: Args) => {
    const job = lookup(args).job(String(args.job));
    if (!job.rename) throw new LiveFailure('invalid', 'the job carries no rename');
    const renamed = await job.rename({ id: String(args.ticket), label: String(args.label) });
    return { label: renamed.label };
  },
};
