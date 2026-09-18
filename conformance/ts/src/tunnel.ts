/** The tunnel under control: channels as connections, lazily read by default. */
import { Channel, Tunnel, type TunnelOptions } from '@nightseam/tunnel';
import { DuplexError } from '@nightseam/runtime';
import { fail, invalid, unsupported, intOf, stringOf, withinOf, within, type Args, type Op, type Testee } from './testee.ts';
import { Conn } from './seam.ts';
import { isPeer } from './peer.ts';

class TunnelOn {
  readonly tunnel: Tunnel;
  constructor(tunnel: Tunnel) {
    this.tunnel = tunnel;
  }
  shutdown(): void { /* Its peer's close ends it. */ }
}

export const isTunnel = (object: unknown): object is TunnelOn => object instanceof TunnelOn;

/** A channel under control: a connection whose reader is attached only when asked, so that credit is held back until then. */
export class ChannelConn extends Conn {
  readonly channel: Channel;
  constructor(channel: Channel, lazy: boolean) {
    super(channel, undefined, lazy);
    this.channel = channel;
  }
}

export const isChannelConn = (object: unknown): object is ChannelConn => object instanceof ChannelConn;

/** consume with lazy as the default, a channel's own. */
const lazyChannel = (args: Args): boolean => {
  const mode = stringOf(args, 'consume');
  if (mode === '' || mode === 'lazy') return true;
  if (mode === 'eager') return false;
  throw invalid('consume is eager or lazy');
};

const tunnelError = (error: unknown) => {
  if (error instanceof DuplexError) {
    if (error.code === 'disconnected' || error.code === 'not_connected') return fail('disconnected', error.message);
    return fail(error.code, error.message);
  }
  return fail('failed', String(error));
};

export function tunnelOps(t: Testee): Record<string, Op> {
  return {
    'tunnel.over': args => {
      const p = t.lookup(args.on, isPeer, 'a peer');
      const options: TunnelOptions = {};
      const raw = args.options;
      if (raw !== undefined) {
        if (typeof raw !== 'object' || raw === null) throw invalid('options is an object');
        for (const [key, value] of Object.entries(raw as Record<string, unknown>)) {
          if (typeof value !== 'number') throw invalid(`options.${key} is an integer`);
          switch (key) {
            case 'window': options.window = value; break;
            case 'max_frame_bytes': options.maxFrameBytes = value; break;
            case 'accept_capacity': options.acceptCapacity = value; break;
            default: throw unsupported(`tunnel option ${key}`);
          }
        }
      }
      return { handle: t.mint('t', new TunnelOn(new Tunnel(p.peer, options))) };
    },
    'tunnel.open': async args => {
      const tn = t.lookup(args.on, isTunnel, 'a tunnel');
      const family = stringOf(args, 'family', true);
      const after = intOf(args, 'after', 0);
      const lazy = lazyChannel(args);
      let channel: Channel;
      try {
        channel = await within(withinOf(args), tn.tunnel.open(family, after), 'the open');
      } catch (error) {
        throw tunnelError(error);
      }
      return { handle: t.mint('ch', new ChannelConn(channel, lazy)), id: channel.id };
    },
    'tunnel.accept': async args => {
      const tn = t.lookup(args.on, isTunnel, 'a tunnel');
      const lazy = lazyChannel(args);
      let channel: Channel;
      try {
        channel = await within(withinOf(args), tn.tunnel.accept(), 'the accept');
      } catch (error) {
        throw tunnelError(error);
      }
      return { handle: t.mint('ch', new ChannelConn(channel, lazy)), id: channel.id, family: channel.family, after: channel.after };
    },
  };
}
