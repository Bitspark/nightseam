import Bitwire
import Foundation
import NightseamDuplex

/// Bounds belong to the local carrier, including the work of handlers that
/// continue running after their caller has cancelled or timed out.
public struct WireOptions: Sendable {
    public var queueCapacity: Int
    public var maxPendingRequests: Int
    public var maxConcurrentHandlers: Int
    public var requestTimeoutMilliseconds: Int
    public var writeTimeoutMilliseconds: Int
    public var maxFrameBytes: Int

    public init(queueCapacity: Int = 64, maxPendingRequests: Int = 128,
                maxConcurrentHandlers: Int = 64, requestTimeoutMilliseconds: Int = 30_000,
                writeTimeoutMilliseconds: Int = 10_000, maxFrameBytes: Int = 1_048_576) {
        self.queueCapacity = queueCapacity
        self.maxPendingRequests = maxPendingRequests
        self.maxConcurrentHandlers = maxConcurrentHandlers
        self.requestTimeoutMilliseconds = requestTimeoutMilliseconds
        self.writeTimeoutMilliseconds = writeTimeoutMilliseconds
        self.maxFrameBytes = maxFrameBytes
    }
}

public enum WireError: Error, Sendable, Equatable {
    case closed
    case backpressure
    case invalidMessage
    case invalidOptions
    case receiverExists
}

/// Each side sends to the other side's receivers. Delivery never runs on the
/// caller's stack, and selecting or mounting either endpoint adds no carrier.
public func wirePair(options: WireOptions = .init()) throws -> (any Endpoint, any Endpoint) {
    guard options.queueCapacity > 0, options.maxPendingRequests > 0,
          options.maxConcurrentHandlers > 0, options.requestTimeoutMilliseconds > 0,
          options.writeTimeoutMilliseconds > 0, options.maxFrameBytes > 0 else {
        throw WireError.invalidOptions
    }
    let pair = LocalWirePair(options: options)
    return (LocalWire(pair: pair, side: 0), LocalWire(pair: pair, side: 1))
}

private struct WireCallKey: Hashable {
    let address: ObjectIdentifier
    let id: String
    init(_ message: Message) {
        address = ObjectIdentifier(message.returnAddress!)
        id = message.frame.id ?? ""
    }
}

private struct WireRoute: Hashable {
    let path: Data
    let namespace: Bool
}

private final class WireRegistration: Sendable {
    let path: [String]
    let receiver: Receiver
    init(path: [String], receiver: Receiver) { self.path = path; self.receiver = receiver }
}

// Mutable members are protected by LocalWirePair.lock.
private final class LocalWireCall: @unchecked Sendable {
    let key: WireCallKey
    let path: [String]
    let original: Message
    var returning: ReturnAddress?
    var registration: WireRegistration?
    var context: RequestContext?
    var timer: DispatchWorkItem?
    var completed = false
    var responded = false
    var active = false
    var cancelQueued = false
    var cancelled = false
    init(path: [String], message: Message) {
        self.key = WireCallKey(message); self.path = path; self.original = message
    }
}

private struct LocalWireDelivery: Sendable {
    let path: [String]
    var message: Message
    var call: LocalWireCall?
    var refusal: ProfileError?
}

private final class LocalWireState {
    var queue: [LocalWireDelivery] = []
    var dataQueued = 0
    var active = 0
    var running = false
    var calls: [WireCallKey: LocalWireCall] = [:]
    var receivers: [WireRoute: WireRegistration] = [:]
    var eventTimer: DispatchWorkItem?
}

private final class LocalWirePair: @unchecked Sendable {
    let lock = NSLock()
    let options: WireOptions
    let states = [LocalWireState(), LocalWireState()]
    let workers = [DispatchQueue(label: "nightseam.wire.0"), DispatchQueue(label: "nightseam.wire.1")]
    var closed = false
    init(options: WireOptions) { self.options = options }

    func admit(side: Int, path: [String], message: Message) throws {
        var delivery = LocalWireDelivery(path: path, message: message)
        let inherited = (message.returnAddress?.wire as? any WireContextProvider)?.wireContext
        lock.lock()
        let state = states[side]
        guard !closed else { lock.unlock(); throw WireError.closed }
        if message.frame.kind == .cancel {
            guard let call = state.calls[WireCallKey(message)], !call.completed,
                  !call.cancelQueued, !call.cancelled else { lock.unlock(); return }
            call.cancelQueued = true
            delivery.call = call
            delivery.message.returnAddress = call.returning
        } else {
            guard state.dataQueued < options.queueCapacity else {
                lock.unlock()
                end(code: 4011, reason: "Local wire queue limit reached")
                throw WireError.backpressure
            }
            state.dataQueued += 1
            if message.frame.kind == .request {
                let key = WireCallKey(message)
                if state.calls[key] != nil {
                    delivery.refusal = ProfileError(code: "invalid_message", message: "Duplicate active request identifier")
                } else if state.calls.count >= options.maxPendingRequests {
                    delivery.refusal = ProfileError(code: "busy", message: "Outstanding call limit reached")
                } else {
                    let call = LocalWireCall(path: path, message: message)
                    call.context = RequestContext(id: message.frame.id ?? "", method: Path.encode(path),
                        traceparent: inherited?.traceparent ?? message.frame.traceparent,
                        tracestate: inherited?.tracestate ?? message.frame.tracestate, meta: message.frame.meta)
                    call.returning = ReturnAddress(wire: LocalWireReturn(pair: self, side: side, call: call))
                    state.calls[key] = call
                    delivery.call = call
                    delivery.message.returnAddress = call.returning
                }
            }
        }
        state.queue.append(delivery)
        let start = !state.running
        state.running = true
        lock.unlock()
        if start { workers[side].async { self.run(side: side) } }
    }

    func match(_ state: LocalWireState, path: [String]) -> WireRegistration? {
        state.receivers[WireRoute(path: Data(), namespace: false)]
    }

    func retire(_ state: LocalWireState, _ call: LocalWireCall) {
        if call.completed && !call.cancelQueued && state.calls[call.key] === call {
            state.calls.removeValue(forKey: call.key)
            // Break the return-capability cycle after dispatch and cancellation
            // have relinquished their reservations.
            call.returning = nil
        }
    }

    func complete(side: Int, call: LocalWireCall) {
        let cancellation: Cancellation? = lock.withLock {
            let state = states[side]
            if !call.completed {
                call.completed = true
                if call.active { state.active -= 1; call.active = false }
                call.timer?.cancel(); call.timer = nil
            }
            retire(state, call)
            return call.context?.cancellation
        }
        cancellation?.cancel()
    }

    func run(side: Int) {
        let state = states[side]
        while true {
            lock.lock()
            guard !closed, !state.queue.isEmpty else {
                state.running = false; lock.unlock(); return
            }
            let delivery = state.queue.removeFirst()
            if delivery.message.frame.kind != .cancel { state.dataQueued -= 1 }
            if let refusal = delivery.refusal {
                lock.unlock(); sendWireResponse(delivery.message, error: refusal); continue
            }
            if delivery.message.frame.kind == .cancel, let call = delivery.call {
                call.cancelQueued = false; call.cancelled = true
                let registration = call.completed ? nil : call.registration
                let cancellation = call.context?.cancellation
                retire(state, call)
                lock.unlock()
                cancellation?.cancel()
                registration?.receiver.message(call.path, delivery.message)
                continue
            }
            let registration = match(state, path: delivery.path)
            if let call = delivery.call {
                guard let registration, state.active < options.maxConcurrentHandlers else {
                    lock.unlock()
                    sendWireResponse(delivery.message, error: ProfileError(
                        code: registration == nil ? "method_not_found" : "busy",
                        message: registration == nil ? "Unknown method" : "Too many concurrent requests"))
                    continue
                }
                call.registration = registration; call.active = true; state.active += 1
                let timer = DispatchWorkItem { [weak self, weak call] in
                    guard let self, let call else { return }
                    self.timeout(side: side, call: call)
                }
                call.timer = timer
                DispatchQueue.global().asyncAfter(deadline: .now() + .milliseconds(options.requestTimeoutMilliseconds), execute: timer)
            }
            if delivery.message.frame.kind == .event && registration != nil {
                let timer = DispatchWorkItem { [weak self] in
                    self?.end(code: 4011, reason: "Local wire event consumer stalled")
                }
                state.eventTimer = timer
                DispatchQueue.global().asyncAfter(deadline: .now() + .milliseconds(options.writeTimeoutMilliseconds), execute: timer)
            }
            lock.unlock()
            registration?.receiver.message(delivery.path, delivery.message)
            if delivery.message.frame.kind == .event {
                lock.withLock { state.eventTimer?.cancel(); state.eventTimer = nil }
            }
        }
    }

    func timeout(side: Int, call: LocalWireCall) {
        lock.lock()
        guard !closed, !call.completed else { lock.unlock(); return }
        let state = states[side]
        if !call.cancelQueued && !call.cancelled {
            call.cancelQueued = true
            let cancel = Message(frame: ProfileFrame(kind: .cancel, id: call.original.frame.id), returnAddress: call.returning)
            state.queue.append(LocalWireDelivery(path: call.path, message: cancel, call: call))
        }
        let respond = !call.responded
        let cancellation = call.context?.cancellation
        call.responded = true
        let start = !state.running
        state.running = true
        lock.unlock()
        cancellation?.cancel()
        if start { workers[side].async { self.run(side: side) } }
        // Answering a deadline does not retire its active handler.
        if respond { sendWireResponse(call.original, error: ProfileError(code: "cancelled", message: "Request cancelled")) }
    }

    func end(code: Int, reason: String) {
        lock.lock()
        guard !closed else { lock.unlock(); return }
        closed = true
        var receivers: [Receiver] = []
        var requests: [Message] = []
        var cancellations: [Cancellation] = []
        for state in states {
            state.eventTimer?.cancel(); state.eventTimer = nil
            receivers += state.receivers.values.map(\.receiver)
            for call in state.calls.values {
                call.timer?.cancel(); call.timer = nil
                if let cancellation = call.context?.cancellation { cancellations.append(cancellation) }
                if !call.responded { requests.append(call.original); call.responded = true }
                call.completed = true; call.returning = nil
            }
            state.calls.removeAll(); state.receivers.removeAll(); state.queue.removeAll(); state.dataQueued = 0
        }
        lock.unlock()
        for cancellation in cancellations { cancellation.cancel() }
        let endedReceivers = receivers, endedRequests = requests
        DispatchQueue.global().async {
            for receiver in endedReceivers { receiver.closed(code, reason) }
            for request in endedRequests {
                sendWireResponse(request, error: ProfileError(code: "disconnected", message: "Connection ended; outcome may be unknown"))
            }
        }
    }
}

private final class LocalWire: Endpoint, Sendable {
    let pair: LocalWirePair
    let side: Int
    init(pair: LocalWirePair, side: Int) { self.pair = pair; self.side = side }

    func send(path: [String], message: Message) throws {
        guard message.frame.kind != .response,
              message.frame.kind == .event || message.returnAddress != nil else { throw WireError.invalidMessage }
        try validateWireMessage(path: path, message: message, limit: pair.options.maxFrameBytes)
        try pair.admit(side: 1 - side, path: path, message: message)
    }

    func receive(receiver: Receiver) throws -> Detach {
        let route = WireRoute(path: Data(), namespace: false)
        let registration = WireRegistration(path: [], receiver: receiver)
        try pair.lock.withLock {
            guard !pair.closed else { throw WireError.closed }
            guard pair.states[side].receivers[route] == nil else { throw WireError.receiverExists }
            pair.states[side].receivers[route] = registration
        }
        return { [pair, side] in
            pair.lock.withLock {
                if pair.states[side].receivers[route] === registration {
                    pair.states[side].receivers.removeValue(forKey: route)
                }
            }
        }
    }

    func close(code: Int, reason: String) throws { pair.end(code: code, reason: reason) }
}

protocol WireContextProvider: Wire { var wireContext: RequestContext? { get } }

private final class LocalWireReturn: WireContextProvider, Sendable {
    let pair: LocalWirePair
    let side: Int
    let call: LocalWireCall
    init(pair: LocalWirePair, side: Int, call: LocalWireCall) { self.pair = pair; self.side = side; self.call = call }
    var wireContext: RequestContext? { pair.lock.withLock { call.context } }
    func send(path: [String], message: Message) throws {
        guard path.isEmpty, message.frame.kind == .response,
              message.frame.id == call.original.frame.id else { throw WireError.invalidMessage }
        try validateWireMessage(path: path, message: message, limit: pair.options.maxFrameBytes)
        defer { pair.complete(side: side, call: call) }
        try pair.lock.withLock {
            guard !call.responded, !call.completed else { throw WireError.closed }
            call.responded = true
        }
        try call.original.returnAddress!.wire.send(path: [], message: message)
    }
}

/// Validate the same envelope a physical carrier would emit, retaining raw JSON
/// payload spelling and keeping the local capability out of that envelope.
func validateWireMessage(path: [String], message: Message, limit: Int = 0) throws {
    let frame = message.frame
    var fields: [String: JSONValue] = ["version": .number(String(frame.version)), "kind": .string(frame.kind.rawValue)]
    if let id = frame.id { fields["id"] = .string(id) }
    if frame.kind == .request { fields["method"] = .string(Path.encode(path)) }
    if frame.kind == .event { fields["event"] = .string(Path.encode(path)) }
    for (key, value) in [("params", frame.params), ("result", frame.result), ("data", frame.data)] {
        if let value {
            guard let text = String(data: value, encoding: .utf8) else { throw WireError.invalidMessage }
            fields[key] = try JSONValue(parsing: text)
        }
    }
    if let error = frame.error {
        var value: [String: JSONValue] = ["code": .string(error.code), "message": .string(error.message)]
        if let data = error.data {
            guard let text = String(data: data, encoding: .utf8) else { throw WireError.invalidMessage }
            value["data"] = try JSONValue(parsing: text)
        }
        fields["error"] = .object(value)
    }
    if let trace = frame.traceparent { fields["traceparent"] = .string(trace) }
    if let state = frame.tracestate { fields["tracestate"] = .string(state) }
    if let meta = frame.meta { fields["meta"] = .members(meta.entries.map { JSONMember(name: $0.0, value: .string($0.1)) }) }
    let encoded = JSONValue.object(fields).encoded()
    guard limit == 0 || encoded.utf8.count <= limit else { throw WireError.invalidMessage }
    _ = try validateFrame(text: encoded, role: nil)
    if let id = frame.id, !validID(id, prefix: "c:"), !validID(id, prefix: "s:") { throw WireError.invalidMessage }
}

/// Completes a local request with the normalized public response. A rejected
/// result gets one bounded fallback before the request is abandoned.
func sendWireResponse(_ request: Message, result: Data? = nil, error: ProfileError? = nil) {
    guard let returning = request.returnAddress else { return }
    var frame = ProfileFrame(kind: .response, id: request.frame.id, result: error == nil ? (result ?? Data("null".utf8)) : nil,
                             error: error, traceparent: request.frame.traceparent, tracestate: request.frame.tracestate)
    do { try returning.wire.send(path: [], message: Message(frame: frame)) }
    catch {
        frame.result = nil
        frame.error = ProfileError(code: "internal", message: "Response could not be encoded")
        try? returning.wire.send(path: [], message: Message(frame: frame))
    }
}

private final class WireForwarding: @unchecked Sendable {
    let lock = NSLock()
    var detaches: [@Sendable () -> Void] = []
    var ended = false
    func stop() {
        let owned: [@Sendable () -> Void] = lock.withLock {
            guard !ended else { return [] }
            ended = true
            let owned = detaches; detaches.removeAll(); return owned
        }
        for detach in owned { detach() }
    }
    func add(_ detach: @escaping @Sendable () -> Void) throws {
        let accepted = lock.withLock {
            if ended { return false }
            detaches.append(detach); return true
        }
        if !accepted { detach(); throw WireError.closed }
    }
}

/// Forward both directions while retaining each request's local return identity.
/// Detach releases registrations and leaves both borrowed endpoints usable.
public func forwardWire(_ left: any Endpoint, _ right: any Endpoint) throws -> @Sendable () -> Void {
    let forwarding = WireForwarding()
    do {
        for (source, destination) in [(left, right), (right, left)] {
            let detach = try source.receive(receiver: Receiver( message: { path, message in
                do { try destination.send(path: path, message: message) }
                catch {
                    forwarding.stop()
                    if message.frame.kind == .request {
                        sendWireResponse(message, error: ProfileError(code: "disconnected", message: "Connection ended; outcome may be unknown"))
                    }
                }
            }, closed: { _, _ in forwarding.stop() }))
            try forwarding.add(detach)
        }
    } catch { forwarding.stop(); throw error }
    return { forwarding.stop() }
}

private final class WireReply: WireContextProvider, @unchecked Sendable {
    let lock = NSLock()
    let wireContext: RequestContext?
    let id = "c:1"
    var result: Result<Data?, any Error>?
    var continuation: CheckedContinuation<Data?, any Error>?
    var published = false
    var withdrawn = false
    init(context: RequestContext?) { self.wireContext = context }
    func resolve(_ result: Result<Data?, any Error>) {
        let waiter: CheckedContinuation<Data?, any Error>? = lock.withLock {
            guard self.result == nil else { return nil }
            self.result = result
            let waiter = continuation; continuation = nil; return waiter
        }
        waiter?.resume(with: result)
    }
    func value() async throws -> Data? {
        try await withCheckedThrowingContinuation { continuation in
            let ready = lock.withLock { () -> Result<Data?, any Error>? in
                if let result { return result }
                self.continuation = continuation; return nil
            }
            if let ready { continuation.resume(with: ready) }
        }
    }
    func publish(wire: any Wire, path: [String], message: Message) throws {
        try lock.withLock {
            guard !withdrawn else { throw CancellationError() }
            try wire.send(path: path, message: message)
            published = true
        }
    }
    func withdraw(wire: any Wire, path: [String], address: ReturnAddress, error: any Error) {
        let cancel = lock.withLock {
            guard result == nil, !withdrawn else { return false }
            withdrawn = true; return published
        }
        resolve(.failure(error))
        if cancel {
            try? wire.send(path: path, message: Message(frame: ProfileFrame(kind: .cancel, id: id), returnAddress: address))
        }
    }
    func send(path: [String], message: Message) throws {
        guard path.isEmpty, message.frame.kind == .response, message.frame.id == id else { throw WireError.invalidMessage }
        try validateWireMessage(path: [], message: message)
        if let error = message.frame.error {
            resolve(.failure(PublicError(code: error.code, message: error.message, data: error.data)))
        } else { resolve(.success(message.frame.result)) }
    }
}

/// Call an operation at an origin without allocating a Peer. Return addresses
/// have independent identity, so concurrent calls can use the same local id.
public func callWire(_ wire: any Wire, path: [String], params: Data? = nil,
                     context: RequestContext? = nil, timeoutMilliseconds: Int = 30_000,
                     meta: Metadata? = nil) async throws -> Data? {
    guard timeoutMilliseconds > 0 else { throw WireError.invalidOptions }
    let reply = WireReply(context: context)
    let address = ReturnAddress(wire: reply)
    let withdraw: @Sendable (any Error) -> Void = { error in
        reply.withdraw(wire: wire, path: path, address: address, error: error)
    }
    let removeCancellation = context?.cancellation.onCancel { withdraw(CancellationError()) }
    defer { removeCancellation?() }
    return try await withTaskCancellationHandler {
        try Task.checkCancellation()
        let request = Message(frame: ProfileFrame(kind: .request, id: reply.id, params: params ?? Data("null".utf8),
            traceparent: context?.traceparent, tracestate: context?.tracestate, meta: meta), returnAddress: address)
        try reply.publish(wire: wire, path: path, message: request)
        let timer = DispatchWorkItem { withdraw(PublicError(code: "request_timeout", message: "Request deadline exceeded")) }
        DispatchQueue.global().asyncAfter(deadline: .now() + .milliseconds(timeoutMilliseconds), execute: timer)
        defer { timer.cancel() }
        return try await reply.value()
    } onCancel: { withdraw(CancellationError()) }
}

private final class WireEventContext: WireContextProvider, Sendable {
    let wireContext: RequestContext?
    init(_ context: RequestContext) { wireContext = context }
    func send(path: [String], message: Message) throws { throw WireError.invalidMessage }
    func close(code: Int, reason: String) throws {}
}

public func emitWire(_ wire: any Wire, path: [String], data: Data? = nil,
                     context: RequestContext? = nil, meta: Metadata? = nil) throws {
    if context?.cancellation.isCancelled == true { throw CancellationError() }
    let frame = ProfileFrame(kind: .event, data: data ?? Data("null".utf8), traceparent: context?.traceparent,
                             tracestate: context?.tracestate, meta: meta)
    try wire.send(path: path, message: Message(frame: frame,
        returnAddress: context.map { ReturnAddress(wire: WireEventContext($0)) }))
}

public typealias WireHandler = @Sendable (RequestContext, Data?) async throws -> Data?

private final class WireHandlers: @unchecked Sendable {
    let lock = NSLock()
    var incoming: [WireCallKey: Cancellation] = [:]
    func cancelAll() {
        let tokens = lock.withLock { Array(incoming.values) }
        for token in tokens { token.cancel() }
    }
}

/// The dispatch callback only installs the task. Cancellation and return
/// completion remain attached to the admitted request after receiver detach.
public func handleWire(_ wire: Dispatcher, path: [String], handler: @escaping WireHandler) throws -> @Sendable () -> Void {
    let handlers = WireHandlers()
    return try wire.register(path: path, receiver: Receiver(message: { delivered, message in
        guard message.frame.kind == .request || message.frame.kind == .cancel,
              message.returnAddress != nil else { return }
        let key = WireCallKey(message)
        if message.frame.kind == .cancel {
            handlers.lock.withLock { handlers.incoming[key] }?.cancel()
            return
        }
        let inherited = (message.returnAddress?.wire as? any WireContextProvider)?.wireContext
        let context = inherited ?? RequestContext(id: message.frame.id ?? "", method: Path.encode(delivered),
            traceparent: message.frame.traceparent, tracestate: message.frame.tracestate, meta: message.frame.meta)
        let installed = handlers.lock.withLock {
            guard handlers.incoming[key] == nil else { return false }
            handlers.incoming[key] = context.cancellation; return true
        }
        guard installed else {
            sendWireResponse(message, error: ProfileError(code: "invalid_message", message: "Duplicate active request identifier")); return
        }
        Task {
            defer {
                _ = handlers.lock.withLock { handlers.incoming.removeValue(forKey: key) }
                context.cancellation.cancel()
            }
            do {
                let value = try await handler(context, message.frame.params)
                if context.cancellation.isCancelled { throw CancellationError() }
                sendWireResponse(message, result: value)
            } catch let error as PublicError {
                sendWireResponse(message, error: ProfileError(code: error.code, message: error.message, data: error.data))
            } catch is CancellationError {
                sendWireResponse(message, error: ProfileError(code: "cancelled", message: "Request cancelled"))
            } catch {
                sendWireResponse(message, error: ProfileError(code: "internal", message: "Internal error"))
            }
        }
    }, closed: { _, _ in handlers.cancelAll() }))
}
