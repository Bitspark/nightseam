export { DuplexPeer, DuplexError, UnpublishedError, positiveInteger, DUPLEX_PROFILE, DUPLEX_DEFAULTS } from './peer.ts';
export type {
  PeerOptions,
  CallOptions,
  EmitOptions,
  RequestContext,
  EventContext,
  Meta,
  RequestHandler,
  Dispatcher,
  EventListener,
  PeerStatus,
} from './peer.ts';
export { NO_OBSERVER } from './observer.ts';
export type { Observer, ObserverEvent, ObserverEvents } from './observer.ts';
export { consoleObserver } from './console.ts';
export { defaultPropagator } from './trace.ts';
export type { Trace, TraceContext, Propagator } from './trace.ts';
export { callWire, handleWire, emitWire, onWireEvent, forwardWire, registerWire } from './wire.ts';
export { wirePair } from './wire-pair.ts';
export type {
  AdapterContext,
  WireModelContext,
  WireRequestContext,
  WireCallOptions,
  WireEmitOptions,
  WireEventContext,
  WireHandler,
  WireHandlers,
  WireEventListener,
} from './wire.ts';
export { webSocketConnection } from '@nightseam/duplex';
export type { Frame, ConnectionState, ConnectionHandlers, FrameConnection, WebSocketLike } from '@nightseam/duplex';
export { createValidator } from './validate.ts';
export { familyTypeAdapter } from './family-adapter.ts';
export { validateDrawnType } from './validate.ts';
export { withDeclaration, boundDeclaration, declarationDigest, typeDeclaration } from './declaration_identity.ts';
export { callableIdentity } from './callable_identity.ts';
export { IDENTITY_METHOD, checkIdentity, identityHandler } from './identity.ts';
export type { DeclarationIdentity } from './identity.ts';
export { prepareIdentity } from './identity_wire.ts';
export type { IdentityPreparation } from './identity_wire.ts';
export { scalarJSON as validateUnicodeJSON } from './unicode.ts';
export {
  jsonAdapter,
  type ValueAdapter,
  type ValueEnvironment,
  type ValueContext,
  type ValueOptions,
} from './value-adapter.ts';
export type {
  TypeExpression,
  WireField,
  WireType,
  WireFamily,
  WireParameter,
  TypeBinding,
  AnyFamily,
  FamilyBinding,
  Slots,
  Validator,
} from './validate.ts';

export { createDispatcher, WireDispatcher, SelectedEndpoint } from './dispatcher.ts';
export type { HandlerRegistry, DispatcherOptions } from './dispatcher.ts';
export {
  Invocation,
  InvocationError,
  InvocationCaptureHandle,
  InvocationBodyHandle,
  beginInvocationBody,
  captureInvocation,
  relayInvocationControl,
  defaultInvocationLimits,
  defaultInvocationCaptures,
  defaultInvocationBodies,
  invocationCapture,
  invocationReady,
  invocationRelease,
  invocationBegin,
  invocationDone,
  invocationControl,
} from './invocation.ts';
export type { InvocationLimits, InvocationRefusal } from './invocation.ts';
