/** The session component under control: a registry, its attachments, and every change it made. */
import {
  Attachment,
  Registry,
  memoryLog,
  type Change,
  type Governance,
  type RegistryOptions,
  type Role,
} from '@nightseam/session';
import { DuplexError, type Trace } from '@nightseam/runtime';
import {
  Inbox,
  fail,
  invalid,
  unsupported,
  boolOf,
  intOf,
  stringOf,
  withinOf,
  type Args,
  type Op,
  type Testee,
} from './testee.ts';
import { isChannelConn } from './tunnel.ts';

class RegistryOn {
  readonly registry: Registry;
  readonly changes = new Inbox<Change>();
  readonly stop: () => void;
  readonly handles = new Map<Attachment, string>();
  constructor(registry: Registry) {
    this.registry = registry;
    this.stop = registry.onChange((change) => this.changes.put(change));
  }
  shutdown(): void {
    this.stop();
  }
}

const isRegistry = (object: unknown): object is RegistryOn => object instanceof RegistryOn;

class AttachmentOn {
  readonly attachment: Attachment;
  constructor(attachment: Attachment) {
    this.attachment = attachment;
  }
  shutdown(): void {
    try {
      this.attachment.detach();
    } catch {
      /* Gone with its session. */
    }
  }
}

const isAttachment = (object: unknown): object is AttachmentOn => object instanceof AttachmentOn;

const splitTrace = (trace: Trace | undefined): Record<string, unknown> | undefined => {
  if (!trace?.traceparent) return undefined;
  const parts = trace.traceparent.split('-');
  if (parts.length !== 4) return { traceparent: trace.traceparent };
  const split: Record<string, unknown> = { trace_id: parts[1], span_id: parts[2], flags: parts[3] };
  if (trace.tracestate) split.state = trace.tracestate;
  return split;
};

/** A change as DRIVER.md says every language reports it. */
const normalizeChange = (reg: RegistryOn, c: Change, withTrace: boolean): Record<string, unknown> => {
  const out: Record<string, unknown> = { kind: c.kind, session: c.session };
  if (c.attachment) {
    out.origin = c.attachment.origin;
    out.role = c.attachment.role;
    const handle = reg.handles.get(c.attachment);
    if (handle) out.attachment = handle;
  }
  if (c.sequence !== undefined && c.sequence !== 0) out.sequence = c.sequence;
  if (c.method) out.method = c.method;
  if (withTrace) {
    const split = splitTrace(c.trace);
    if (split) out.trace = split;
  }
  return out;
};

const sessionError = (error: unknown) => {
  if (error instanceof DuplexError) return fail(error.code, error.message);
  return fail('failed', error instanceof Error ? error.message : String(error));
};

export function sessionOps(t: Testee): Record<string, Op> {
  const registryOf = (args: Args) => t.lookup(args.on, isRegistry, 'a registry');
  const channelOf = (args: Args) => {
    const c = t.lookup(args.channel, isChannelConn, 'a tunnel channel');
    if (!c.lazy) throw invalid('a session takes a lazily consumed channel, since it reads it itself');
    return c.channel;
  };
  return {
    'session.new': (args) => {
      const options: RegistryOptions = {};
      const raw = args.options;
      if (raw !== undefined) {
        if (typeof raw !== 'object' || raw === null) throw invalid('options is an object');
        for (const [key, value] of Object.entries(raw as Record<string, unknown>)) {
          if (typeof value !== 'number') throw invalid(`options.${key} is an integer`);
          switch (key) {
            case 'max_attachments':
              options.maxAttachments = value;
              break;
            case 'max_inflight':
              options.maxInflight = value;
              break;
            default:
              throw unsupported(`session option ${key}`);
          }
        }
      }
      try {
        return { handle: t.mint('reg', new RegistryOn(new Registry(options))) };
      } catch (error) {
        throw sessionError(error);
      }
    },
    'session.bind': async (args) => {
      const reg = registryOf(args);
      const id = stringOf(args, 'session', true);
      const up = channelOf(args);
      const raw = args.governance;
      if (typeof raw !== 'object' || raw === null) throw invalid('governance names decides and asks');
      const { decides = [], asks = [] } = raw as { decides?: string[]; asks?: string[] };
      const deciding = new Set(decides);
      const asking = new Set(asks);
      const governance: Governance = { decides: (m) => deciding.has(m), asks: (m) => asking.has(m) };
      const options = (args.log ?? {}) as Record<string, unknown>;
      const bound = typeof options.max_frame_bytes === 'number' ? options.max_frame_bytes : 1 << 20;
      const log = memoryLog(bound);
      // A log with frames in it before anything is bound, which is what a
      // durable one holds after the process that wrote them ended; it is
      // filled through the Log interface, as the component's own suites fill
      // theirs.
      if (options.prefill !== undefined) {
        if (!Array.isArray(options.prefill)) throw invalid('log.prefill is a list of frames');
        for (const entry of options.prefill as Record<string, unknown>[]) {
          const direction = entry.direction ?? 'down';
          if (direction !== 'up' && direction !== 'down') throw invalid('log.prefill direction is up or down');
          if (typeof entry.text !== 'string') throw invalid('log.prefill names each frame by its message, as text');
          let message: unknown;
          try {
            message = JSON.parse(entry.text);
          } catch {
            throw invalid('log.prefill names each frame by its message, as text');
          }
          const origin = typeof entry.origin === 'string' ? entry.origin : '';
          await log.append({ sequence: 0, direction, origin, at: new Date(), message, truncated: false });
        }
      }
      try {
        reg.registry.bind(id, up, governance, log);
      } catch (error) {
        throw sessionError(error);
      }
      return {};
    },
    'session.attach': (args) => {
      const reg = registryOf(args);
      const id = stringOf(args, 'session', true);
      const down = channelOf(args);
      // A role that is not one is handed on rather than refused here, so
      // that a scenario holds the layer's own refusal and not the testee's
      // reading of an argument.
      const role = stringOf(args, 'role', true);
      const origin = stringOf(args, 'origin', true);
      const after = intOf(args, 'after', 0);
      let attachment: Attachment;
      try {
        attachment = reg.registry.attach(id, down, role as Role, origin, after);
      } catch (error) {
        throw sessionError(error);
      }
      const handle = t.mint('att', new AttachmentOn(attachment));
      reg.handles.set(attachment, handle);
      return { handle };
    },
    'session.control': (args) => {
      const reg = registryOf(args);
      const id = stringOf(args, 'session', true);
      let holder: Attachment | null = null;
      if (args.attachment !== undefined && args.attachment !== null) {
        holder = t.lookup(args.attachment, isAttachment, 'an attachment').attachment;
      }
      try {
        reg.registry.control(id, holder);
      } catch (error) {
        throw sessionError(error);
      }
      return {};
    },
    'session.attention': (args) => registryOf(args).registry.attention(),
    'session.changes': (args) => {
      const reg = registryOf(args);
      const withTrace = boolOf(args, 'trace');
      return reg.changes.drain(boolOf(args, 'drain', true)).map((c) => normalizeChange(reg, c, withTrace));
    },
    'session.await_change': async (args) => {
      const reg = registryOf(args);
      const kind = stringOf(args, 'kind', true);
      const { item } = await reg.changes.await(withinOf(args), (c) => c.kind === kind);
      if (!item) throw fail('timeout', `no ${kind}`);
      return normalizeChange(reg, item, boolOf(args, 'trace'));
    },
    // attachment.state is what the relay last told this consumer of the
    // session's own vocabulary: who holds control, and where in the log it
    // stands. A testee reports it so that a scenario can hold the
    // attachment's state and the frames on the wire to each other.
    'attachment.state': (args) => {
      const { attachment } = t.lookup(args.on, isAttachment, 'an attachment');
      return { holder: attachment.holder, sequence: attachment.sequence };
    },
    'attachment.detach': (args) => {
      t.lookup(args.on, isAttachment, 'an attachment').attachment.detach();
      return {};
    },
  };
}
