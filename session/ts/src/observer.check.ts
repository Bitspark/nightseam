/**
 * A consumer's switch, and the whole of what it proves: with these packages
 * imported the runtime's `ObserverEvent` carries the session's ten events
 * beside the runtime's ten and the tunnel's five, every one of them narrows
 * to the fields declared for it, and a switch that answers all twenty-five
 * leaves `never` — so a layer added below is a case the compiler asks the
 * consumer for. It is checked by `pnpm check` and is no part of what the
 * package ships.
 *
 * The tunnel is imported here and nowhere else in this package: a session
 * runs over a connection of the seam, of which a channel is one, so the
 * session no longer declares the tunnel's five events on a consumer's
 * behalf. A program that opens channels imports the tunnel itself, and this
 * is that program.
 */
import type { ObserverEvent } from '@nightseam/runtime';
import '@nightseam/tunnel';
import './index.ts';

export function describe(event: ObserverEvent): string {
  switch (event.type) {
    case 'session.bound':
      return `session ${event.session} bound`;
    case 'session.unbound':
      return `session ${event.session} unbound ${event.code} ${event.reason}`;
    case 'session.attached':
      return `${event.origin} attached to ${event.session} as ${event.role} after ${event.after}`;
    case 'session.detached':
      return `${event.origin} detached from ${event.session} as ${event.role}`;
    case 'ask.raised':
      return `${event.session} was asked ${event.method} as ${event.id}${event.asking ? ', which it waits on' : ''}`;
    case 'ask.routed':
      return `${event.session} routed ${event.id} to ${event.origin}`;
    case 'ask.answered':
      return `${event.origin} answered ${event.method} of ${event.session}`;
    case 'control.changed':
      return `${event.session} is ${event.origin ? `held by ${event.origin}` : 'held by nobody'}`;
    case 'frame.appended':
      return `${event.session} appended ${event.direction} #${event.sequence} of ${event.bytes} bytes from ${event.origin || 'the machine'}`;
    case 'session.refused':
      return `${event.session} refused ${event.method} of ${event.origin} with ${event.code}`;
    case 'channel.opened':
      return `${event.family} channel ${event.id} after ${event.after} ${event.opener ? 'opened here' : 'opened there'}`;
    case 'channel.accepted':
      return `${event.family} channel ${event.id} after ${event.after} accepted`;
    case 'channel.closed':
      return `${event.family} channel ${event.id} closed ${event.code} ${event.reason}`;
    case 'credit.stall':
      return `${event.family} channel ${event.id} stalled with ${event.waiting} waiting`;
    case 'open.refused':
      return `${event.family} open refused: ${event.reason}`;
    case 'connection.opened':
      return `connection opened as ${event.role}`;
    case 'connection.closed':
      return `connection closed ${event.code}`;
    case 'frame.sent':
    case 'frame.received':
      return `${event.type} ${event.name} of ${event.bytes} bytes`;
    case 'request.started':
      return `${event.method} started`;
    case 'request.ended':
      return `${event.method} ${event.outcome}`;
    case 'event.emitted':
    case 'event.delivered':
      return `${event.type} ${event.name}`;
    case 'backpressure':
      return `${event.queued} queued`;
    case 'handler.panic':
      return `${event.method} panicked`;
    default: {
      const unreached: never = event;
      return unreached;
    }
  }
}
