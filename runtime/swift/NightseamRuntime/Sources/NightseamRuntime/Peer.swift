import Foundation
import NightseamDuplex

public struct PublicError: Error, Sendable {
    public let code: String
    public let message: String
    public let data: Data?
    public init(code: String, message: String, data: Data? = nil) {
        self.code = code; self.message = message; self.data = data
    }
}

/// Absence is independent of an explicitly present JSON null.
public enum Presence<Value: Sendable>: Sendable {
    case absent
    case present(Value)
}

/// Cancellation is independent of handler lifetime. A handler that ignores it
/// retains its active-work slot until it actually returns.
public final class Cancellation: @unchecked Sendable {
    private let lock = NSLock()
    private var cancelled = false
    private var callbacks: [UUID: @Sendable () -> Void] = [:]
    public init() {}
    public var isCancelled: Bool { lock.withLock { cancelled } }
    public func cancel() {
        let pending = lock.withLock { () -> [@Sendable () -> Void] in
            if cancelled { return [] }; cancelled = true
            let pending = Array(callbacks.values); callbacks.removeAll(); return pending
        }
        for callback in pending { callback() }
    }
    public func onCancel(_ callback: @escaping @Sendable () -> Void) -> @Sendable () -> Void {
        let id = UUID()
        let run = lock.withLock { () -> Bool in
            if cancelled { return true }; callbacks[id] = callback; return false
        }
        if run { callback() }
        return { [weak self] in self?.remove(id) }
    }
    private func remove(_ id: UUID) { lock.withLock { _ = callbacks.removeValue(forKey: id) } }
    public func wait() async {
        await withCheckedContinuation { continuation in
            _ = onCancel { continuation.resume() }
        }
    }
}

public struct RequestContext: Sendable {
    public let id: String
    public let method: String
    public let traceparent: String?
    public let tracestate: String?
    public let meta: Metadata?
    public let cancellation: Cancellation
    public init(id: String = "", method: String = "", traceparent: String? = nil,
                tracestate: String? = nil, meta: Metadata? = nil,
                cancellation: Cancellation = Cancellation()) {
        self.id = id; self.method = method; self.traceparent = traceparent
        self.tracestate = tracestate; self.meta = meta; self.cancellation = cancellation
    }
}

public struct PeerOptions: Sendable {
    public var maxConcurrentHandlers = 64
    public var maxPendingRequests = 128
    public var queueCapacity = 128
    public var maxFrameBytes = 1 << 20
    public var requestTimeoutMilliseconds = 30_000
    public var writeTimeoutMilliseconds = 10_000
    public init() {}
}

/// A cancellation-aware one-shot result, also used by the conformance driver.
public actor AsyncResult<Value: Sendable> {
    private var result: Result<Value, any Error>?
    private var waiters: [UUID: CheckedContinuation<Value, any Error>] = [:]
    public init() {}
    public func resolve(_ value: Result<Value, any Error>) {
        guard result == nil else { return }; result = value
        let waiting = waiters.values; waiters.removeAll()
        for waiter in waiting { waiter.resume(with: value) }
    }
    public func value() async throws -> Value {
        if let result { return try result.get() }
        let id = UUID()
        return try await withTaskCancellationHandler {
            try Task.checkCancellation()
            return try await withCheckedThrowingContinuation { waiters[id] = $0 }
        } onCancel: { Task { await self.cancelWaiter(id) } }
    }
    private func cancelWaiter(_ id: UUID) { waiters.removeValue(forKey: id)?.resume(throwing: CancellationError()) }
}

public func withTimeout<Value: Sendable>(milliseconds: Int, operation: @escaping @Sendable () async throws -> Value) async throws -> Value {
    try await withThrowingTaskGroup(of: Value.self) { group in
        group.addTask { try await operation() }
        group.addTask {
            try await Task.sleep(for: .milliseconds(milliseconds))
            throw PublicError(code: "timeout", message: "Operation deadline passed")
        }
        defer { group.cancelAll() }
        return try await group.next()!
    }
}

public typealias Handler = @Sendable (RequestContext, Peer, Data?) async throws -> Data?
public typealias EventHandler = @Sendable (RequestContext, Peer, Data?) async throws -> Void

/// The profile over any ordered frame connection. The transport is owned from
/// start until closure; all application dispatch leaves the receive loop.
public actor Peer {
    public nonisolated let connection: any Connection
    public nonisolated let role: String
    public nonisolated let options: PeerOptions
    private var handlers: [Data: Handler] = [:]
    private var eventHandlers: [Data: EventHandler] = [:]
    private var listeners: [UUID: EventHandler] = [:]
    private var requestFallback: (@Sendable (String) -> Handler?)?
    private var eventFallback: (@Sendable (String) -> EventHandler?)?
    private struct Pending: Sendable { let reply: AsyncResult<Data?>; let trace: [String: JSONValue] }
    private var pending: [String: Pending] = [:]
    private var incoming: [String: Cancellation] = [:]
    private var outgoing: [Data] = []
    private var events: [(RequestContext, Data?)] = []
    private var nextID: UInt64 = 0
    private var readTask: Task<Void, Never>?
    private var writeTask: Task<Void, Never>?
    private var eventTask: Task<Void, Never>?
    private var end: CloseError?
    private let ended = AsyncResult<CloseError>()
    private var wirePreparation: Task<PeerWire, any Error>?

    public func wire() async throws -> any Wire {
        if let wirePreparation { return try await wirePreparation.value }
        let preparation = Task { try await PeerWire.attach(peer: self) }
        wirePreparation = preparation
        return try await preparation.value
    }

    public init(connection: any Connection, role: String, options: PeerOptions = .init()) throws {
        guard ["client", "server"].contains(role), options.maxPendingRequests > 0,
              options.maxConcurrentHandlers > 0, options.queueCapacity > 0, options.maxFrameBytes > 0,
              options.requestTimeoutMilliseconds > 0, options.writeTimeoutMilliseconds > 0 else {
            throw PublicError(code: "invalid", message: "Invalid peer role or limits")
        }
        self.connection = connection; self.role = role; self.options = options
    }

    public func start() {
        guard readTask == nil else { return }
        readTask = Task { await self.readLoop() }
    }
    public func handle(method: String, handler: @escaping Handler) throws {
        guard !method.isEmpty, handlers[Data(method.utf8)] == nil else { throw PublicError(code: "invalid", message: "Duplicate or empty method") }
        handlers[Data(method.utf8)] = handler
    }
    public func onEvent(name: String, handler: @escaping EventHandler) throws {
        guard !name.isEmpty, eventHandlers[Data(name.utf8)] == nil else { throw PublicError(code: "invalid", message: "Duplicate or empty event") }
        eventHandlers[Data(name.utf8)] = handler
    }
    public func observeEvents(_ handler: @escaping EventHandler) -> UUID {
        let id = UUID(); listeners[id] = handler; return id
    }
    public func removeEventObserver(_ id: UUID) { listeners.removeValue(forKey: id) }
    public func installFallback(request: @escaping @Sendable (String) -> Handler?, event: @escaping @Sendable (String) -> EventHandler?) {
        requestFallback = request; eventFallback = event
    }

    public func call(method: String, params: Data?, context: RequestContext? = nil,
                     timeoutMilliseconds: Int? = nil, meta: Metadata? = nil, preservingTrace: Bool = false, admitted: (@Sendable () -> Void)? = nil, withdrawn: (@Sendable () -> Void)? = nil) async throws -> Data? {
        let deadline = ContinuousClock.now.advanced(by: .milliseconds(min(timeoutMilliseconds ?? options.requestTimeoutMilliseconds, options.requestTimeoutMilliseconds)))
        guard !method.isEmpty else { throw PublicError(code: "invalid", message: "Empty method") }
        try Task.checkCancellation()
        guard context?.cancellation.isCancelled != true else { throw PublicError(code: "cancelled", message: "Request cancelled") }
        guard end == nil else { throw disconnected() }
        guard pending.count < options.maxPendingRequests else { throw PublicError(code: "busy", message: "Outstanding call limit reached") }
        nextID += 1
        let id = (role == "client" ? "c:" : "s:") + String(nextID)
        let trace = preservingTrace ? carriedTrace(context) : childTrace(context)
        var frame = trace
        frame["version"] = .number("1"); frame["kind"] = .string("request")
        frame["id"] = .string(id); frame["method"] = .string(method)
        if let meta { frame["meta"] = meta.filter { !$0.0.hasPrefix("nightseam.") }.jsonValue }
        let bytes = try encode(frame, raw: ["params": params ?? Data("null".utf8)])
        let reply = AsyncResult<Data?>()
        pending[id] = Pending(reply: reply, trace: trace)
        do { try await enqueue(bytes, requestDeadline: deadline, cancellation: context?.cancellation) }
        catch {
            pending.removeValue(forKey: id)
            if error is CancellationError { throw PublicError(code: "cancelled", message: "Request cancelled") }
            throw error
        }
        admitted?()
        let remove = context?.cancellation.onCancel { Task { await self.withdraw(id: id, code: "cancelled"); withdrawn?() } }
        defer { remove?() }
        return try await withTaskCancellationHandler {
            do {
                let remaining = ContinuousClock.now.duration(to: deadline).components
                let milliseconds = max(0, Int(remaining.seconds) * 1_000 + Int(remaining.attoseconds / 1_000_000_000_000_000))
                return try await withTimeout(milliseconds: milliseconds) {
                    try await reply.value()
                }
            } catch let failure as PublicError where failure.code == "timeout" {
                await withdraw(id: id, code: "request_timeout")
                throw PublicError(code: "request_timeout", message: "Request deadline passed")
            } catch is CancellationError {
                await withdraw(id: id, code: "cancelled")
                throw PublicError(code: "cancelled", message: "Request cancelled")
            }
        } onCancel: { Task { await self.withdraw(id: id, code: "cancelled") } }
    }

    private func withdraw(id: String, code: String) async {
        guard let call = pending.removeValue(forKey: id) else { return }
        await call.reply.resolve(.failure(PublicError(code: code, message: "Request withdrawn")))
        var frame = call.trace
        frame["version"] = .number("1"); frame["kind"] = .string("cancel"); frame["id"] = .string(id)
        if end == nil, outgoing.count < options.queueCapacity, let bytes = try? encode(frame), bytes.count <= options.maxFrameBytes {
            outgoing.append(bytes); launchWriter()
        }
    }

    public func emit(name: String, data: Data?, context: RequestContext? = nil, meta: Metadata? = nil, preservingTrace: Bool = false) async throws {
        guard !name.isEmpty else { throw PublicError(code: "invalid", message: "Empty event") }
        var frame = preservingTrace ? carriedTrace(context) : childTrace(context)
        frame["version"] = .number("1"); frame["kind"] = .string("event"); frame["event"] = .string(name)
        if let meta { frame["meta"] = meta.filter { !$0.0.hasPrefix("nightseam.") }.jsonValue }
        try await enqueue(encode(frame, raw: ["data": data ?? Data("null".utf8)]), cancellation: context?.cancellation)
    }

    private func enqueue(_ bytes: Data, requestDeadline: ContinuousClock.Instant? = nil,
                         cancellation: Cancellation? = nil, immediate: Bool = false) async throws {
        guard bytes.count <= options.maxFrameBytes else { throw PublicError(code: "frame_too_large", message: "Frame exceeds size limit") }
        let deadline = ContinuousClock.now.advanced(by: .milliseconds(options.writeTimeoutMilliseconds))
        var yielded = false
        while true {
            try Task.checkCancellation()
            guard cancellation?.isCancelled != true else { throw PublicError(code: "cancelled", message: "Request cancelled") }
            if let requestDeadline, ContinuousClock.now >= requestDeadline {
                throw PublicError(code: "request_timeout", message: "Request deadline passed")
            }
            guard end == nil else { throw disconnected() }
            if outgoing.count < options.queueCapacity { break }
            if immediate {
                if !yielded { yielded = true; await Task.yield(); continue }
                await finish(code: 1006, reason: "", send: false, abort: true)
                throw disconnected()
            }
            if ContinuousClock.now >= deadline {
                await finish(code: 1006, reason: "", send: false, abort: true)
                throw disconnected()
            }
            try await Task.sleep(for: .milliseconds(1))
        }
        guard end == nil else { throw disconnected() }
        outgoing.append(bytes); launchWriter()
    }
    private func launchWriter() {
        guard writeTask == nil else { return }
        writeTask = Task { await self.writeLoop() }
    }
    private func writeLoop() async {
        while end == nil, !outgoing.isEmpty {
            let bytes = outgoing.removeFirst()
            do {
                try await withTimeout(milliseconds: options.writeTimeoutMilliseconds) { [connection] in
                    try await connection.send(Frame(kind: .text, data: bytes))
                }
            } catch {
                await finish(code: 1006, reason: "", send: false, abort: true); break
            }
        }
        writeTask = nil
    }
    private func readLoop() async {
        while end == nil {
            do {
                let received = try await connection.receive()
                guard received.kind == .text, received.data.count <= options.maxFrameBytes,
                      let text = String(data: received.data, encoding: .utf8) else {
                    await finish(code: 4011, reason: "Invalid profile frame", send: true); return
                }
                let frame: JSONValue
                do { frame = try validateFrame(text: text, role: role) }
                catch { await finish(code: 4011, reason: "Invalid profile frame", send: true); return }
                try await dispatch(frame, raw: text)
            } catch let close as CloseError {
                await finish(code: close.code, reason: close.reason, send: false)
            } catch {
                await finish(code: 1006, reason: "", send: false)
            }
        }
    }
    private func dispatch(_ frame: JSONValue, raw: String) async throws {
        let kind = frame["kind"]?.string ?? ""
        let id = frame["id"]?.string ?? ""
        switch kind {
        case "response":
            if let call = pending.removeValue(forKey: id) {
                if let error = frame["error"] {
                    await call.reply.resolve(.failure(PublicError(code: error["code"]?.string ?? "internal",
                        message: error["message"]?.string ?? "Internal error", data: error["data"].map { Data($0.encoded().utf8) })))
                } else {
                    await call.reply.resolve(.success(try rawMember("result", in: raw).map { Data($0.utf8) }))
                }
            }
        case "cancel": incoming[id]?.cancel()
        case "request":
            guard incoming[id] == nil else { await finish(code: 4011, reason: "Duplicate active request", send: true); return }
            let method = frame["method"]?.string ?? ""
            let context = contextFor(frame, method: method)
            guard let handler = handlers[Data(method.utf8)] ?? requestFallback?(method) else {
                await respond(context, result: .failure(PublicError(code: "method_not_found", message: "Unknown method")), immediate: true); return
            }
            guard incoming.count < options.maxConcurrentHandlers else {
                await respond(context, result: .failure(PublicError(code: "busy", message: "Too many concurrent requests")), immediate: true); return
            }
            incoming[id] = context.cancellation
            let payload = try rawMember("params", in: raw).map { Data($0.utf8) }
            Task {
                let timeout = Task {
                    do { try await Task.sleep(for: .milliseconds(options.requestTimeoutMilliseconds)); context.cancellation.cancel() } catch {}
                }
                let result: Result<Data?, any Error>
                do { result = .success(try await handler(context, self, payload)) } catch { result = .failure(error) }
                timeout.cancel()
                await self.respond(context, result: context.cancellation.isCancelled ? .failure(CancellationError()) : result)
                self.completeIncoming(id)
            }
        case "event":
            let context = contextFor(frame, method: frame["event"]?.string ?? "")
            let deadline = ContinuousClock.now.advanced(by: .milliseconds(options.writeTimeoutMilliseconds))
            while events.count >= options.queueCapacity, end == nil {
                if ContinuousClock.now >= deadline { await finish(code: 1006, reason: "", send: false, abort: true); return }
                try await Task.sleep(for: .milliseconds(1))
            }
            events.append((context, try rawMember("data", in: raw).map { Data($0.utf8) }))
            if eventTask == nil { eventTask = Task { await self.eventLoop() } }
        default: break
        }
    }
    private func completeIncoming(_ id: String) { incoming.removeValue(forKey: id) }
    private func contextFor(_ frame: JSONValue, method: String) -> RequestContext {
        RequestContext(id: frame["id"]?.string ?? "", method: method,
            traceparent: frame["traceparent"]?.string, tracestate: frame["tracestate"]?.string,
            meta: frame["meta"].flatMap { try? Metadata(jsonValue: $0) })
    }
    private func respond(_ context: RequestContext, result: Result<Data?, any Error>, immediate: Bool = false) async {
        guard end == nil else { return }
        var frame: [String: JSONValue] = ["version": .number("1"), "kind": .string("response"), "id": .string(context.id)]
        if let trace = context.traceparent { frame["traceparent"] = .string(trace) }
        if let state = context.tracestate { frame["tracestate"] = .string(state) }
        var payload: Data?
        switch result {
        case .success(let value): payload = value ?? Data("null".utf8)
        case .failure(let error):
            let publicError: PublicError
            if let value = error as? PublicError, !value.code.isEmpty, !value.message.isEmpty { publicError = value }
            else if error is CancellationError { publicError = PublicError(code: "cancelled", message: "Request cancelled") }
            else { publicError = PublicError(code: "internal", message: "Internal error") }
            var encoded: [String: JSONValue] = ["code": .string(publicError.code), "message": .string(publicError.message)]
            if let data = publicError.data, let value = try? JSONValue(parsing: String(decoding: data, as: UTF8.self)) { encoded["data"] = value }
            frame["error"] = .object(encoded)
        }
        do { try await enqueue(encode(frame, raw: ["result": payload]), immediate: immediate) }
        catch {
            guard end == nil else { return }
            if immediate { await finish(code: 1006, reason: "", send: false, abort: true); return }
            frame.removeValue(forKey: "result")
            frame["error"] = .object(["code": .string("internal"), "message": .string("Response could not be encoded")])
            do { try await enqueue(encode(frame)) }
            catch { await finish(code: 1006, reason: "", send: false, abort: true) }
        }
    }
    private func eventLoop() async {
        while end == nil, !events.isEmpty {
            let (context, payload) = events.removeFirst()
            do {
                if let handler = eventHandlers[Data(context.method.utf8)] ?? eventFallback?(context.method) { try await handler(context, self, payload) }
                for listener in listeners.values { try await listener(context, self, payload) }
            } catch { await finish(code: 1006, reason: "", send: false, abort: true) }
        }
        eventTask = nil
    }
    public func close() async { await finish(code: 1000, reason: "", send: true) }
    public func close(code: Int, reason: String) async { await finish(code: code, reason: reason, send: true) }
    public func awaitClose() async throws -> CloseError { try await ended.value() }
    private func finish(code: Int, reason: String, send: Bool, abort: Bool = false) async {
        guard end == nil else { return }
        let closing = CloseError(code: code, reason: reason); end = closing
        for token in incoming.values { token.cancel() }
        for call in pending.values { await call.reply.resolve(.failure(disconnected())) }
        pending.removeAll(); outgoing.removeAll(); events.removeAll()
        await ended.resolve(.success(closing))
        if abort { await connection.abort() }
        else if send { try? await connection.close(code: code, reason: reason) }
    }
    private func disconnected() -> PublicError { PublicError(code: "disconnected", message: "Connection ended") }
    private func carriedTrace(_ context: RequestContext?) -> [String: JSONValue] {
        var trace: [String: JSONValue] = [:]
        if let parent = context?.traceparent { trace["traceparent"] = .string(parent) }
        if let state = context?.tracestate { trace["tracestate"] = .string(state) }
        return trace
    }
    private func childTrace(_ context: RequestContext?) -> [String: JSONValue] {
        let span = UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased().prefix(16)
        var trace = UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()
        var flags = "01"
        if let parent = context?.traceparent {
            let pieces = parent.split(separator: "-")
            if pieces.count == 4 { trace = String(pieces[1]); flags = String(pieces[3]) }
        }
        var result: [String: JSONValue] = ["traceparent": .string("00-\(trace)-\(span)-\(flags)")]
        if let state = context?.tracestate { result["tracestate"] = .string(state) }
        return result
    }
}

/// Raw payload members retain their numeric tokens and byte representation.
private func encode(_ fields: [String: JSONValue], raw: [String: Data?] = [:]) throws -> Data {
    var parts = fields.keys.sorted().map { JSONValue.string($0).encoded() + ":" + fields[$0]!.encoded() }
    for key in raw.keys.sorted() {
        if let data = raw[key] ?? nil {
            guard let value = String(data: data, encoding: .utf8) else { throw PublicError(code: "invalid", message: "Payload is not UTF-8") }
            _ = try JSONValue(parsing: value)
            parts.append(JSONValue.string(key).encoded() + ":" + value)
        }
    }
    return Data(("{" + parts.joined(separator: ",") + "}").utf8)
}
