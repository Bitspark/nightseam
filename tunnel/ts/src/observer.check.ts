/**
 * A consumer's switch, and the whole of what it proves: with this package
 * imported the runtime's `ObserverEvent` carries the tunnel's five events
 * beside the runtime's ten, every one of them narrows to the fields declared
 * for it, and a switch that answers all fifteen leaves `never` — so a layer
 * added below is a case the compiler asks the consumer for. It is checked by
 * `pnpm check` and is no part of what the package ships.
 */
import type { ObserverEvent } from '@nightseam/runtime';
import './index.ts';

export function describe(event: ObserverEvent): string {
  switch (event.type) {
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
