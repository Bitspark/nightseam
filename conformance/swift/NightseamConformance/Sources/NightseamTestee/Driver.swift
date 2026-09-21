import Foundation
import NightseamDuplex
import NightseamRuntime

actor Inbox {
    private var entries: [JSONValue] = []
    private var ended = false
    func put(_ value: JSONValue) { entries.append(value) }
    func close() { ended = true }
    func take(within: Int, matching: @Sendable (JSONValue) -> Bool = { _ in true }) async throws -> JSONValue {
        let deadline = ContinuousClock.now.advanced(by: .milliseconds(within))
        while true {
            if let at = entries.firstIndex(where: matching) { return entries.remove(at: at) }
            if ended { throw PublicError(code: "disconnected", message: "Inbox closed") }
            if ContinuousClock.now >= deadline { throw PublicError(code: "timeout", message: "Nothing arrived") }
            try await Task.sleep(for: .milliseconds(1))
        }
    }
}

actor ControlledConnection {
    let connection: any Connection
    let lazy: Bool
    private var frames: [Frame] = []
    private var end: CloseError?
    private var reader: Task<Void, Never>?
    init(_ connection: any Connection, lazy: Bool = false) { self.connection = connection; self.lazy = lazy }
    func start() {
        if !lazy { reader = Task {
            do { while true { frames.append(try await connection.receive()) } }
            catch let error as CloseError { end = error }
            catch { end = CloseError(code: 1006, reason: "") }
        } }
    }
    func receive(within: Int) async throws -> Frame {
        if lazy {
            do { return try await withTimeout(milliseconds: within) { [connection] in try await connection.receive() } }
            catch let error as CloseError { end = error; throw error }
        }
        let deadline = ContinuousClock.now.advanced(by: .milliseconds(within))
        while frames.isEmpty {
            if let end { throw end }
            if ContinuousClock.now >= deadline { throw PublicError(code: "timeout", message: "No frame arrived") }
            try await Task.sleep(for: .milliseconds(1))
        }
        return frames.removeFirst()
    }
    func awaitClose(within: Int) async throws -> CloseError {
        if lazy, end == nil {
            do { _ = try await receive(within: within) } catch let error as CloseError { end = error }
        }
        let deadline = ContinuousClock.now.advanced(by: .milliseconds(within))
        while end == nil {
            if ContinuousClock.now >= deadline { throw PublicError(code: "timeout", message: "Connection remains open") }
            try await Task.sleep(for: .milliseconds(1))
        }
        return end!
    }
    func shutdown() async { reader?.cancel(); await connection.abort(); end = CloseError(code: 1006, reason: "") }
}

struct ControlledPeer: Sendable {
    let peer: Peer
    let requests = Inbox()
    let events = Inbox()
    func start() async {
        _ = await peer.observeEvents { context, _, data in
            var event: [String: JSONValue] = ["name": .string(context.method), "data": try data.map(JSONValue.init(data:)) ?? .null]
            if let meta = context.meta, !meta.isEmpty { event["meta"] = meta.jsonValue }
            await events.put(.object(event))
        }
        await peer.start()
    }
    func shutdown() async { await peer.close(); await requests.close(); await events.close() }
}

struct Listener: Sendable {
    let socket: WebSocketListener
    let accepted: AsyncResult<any Connection>
    let options: PeerOptions?
}

struct Call: Sendable {
    let task: Task<Data?, any Error>
    let result: AsyncResult<Data?>
}

actor Driver {
    var next = 0
    var connections: [String: ControlledConnection] = [:]
    var peers: [String: ControlledPeer] = [:]
    var listeners: [String: Listener] = [:]
    var calls: [String: Call] = [:]
    func mint() -> String { next += 1; return "s\(next)" }
    func reset() async {
        for call in calls.values { call.task.cancel() }
        for peer in peers.values { await peer.shutdown() }
        for connection in connections.values { await connection.shutdown() }
        for listener in listeners.values { listener.socket.close() }
        calls.removeAll(); peers.removeAll(); connections.removeAll(); listeners.removeAll()
    }
    func options(_ value: JSONValue?) throws -> PeerOptions {
        var result = PeerOptions()
        for (key, value) in value?.object ?? [:] {
            let number = value.number.flatMap(Int.init) ?? 0
            switch key {
            case "queue_capacity": result.queueCapacity = number
            case "max_frame_bytes": result.maxFrameBytes = number
            case "max_pending_requests": result.maxPendingRequests = number
            case "request_timeout_ms": result.requestTimeoutMilliseconds = number
            case "write_timeout_ms": result.writeTimeoutMilliseconds = number
            case "propagate", "families": break
            case "observe": if value.bool == true { throw PublicError(code: "unsupported", message: "Observer is a separate profile") }
            default: throw PublicError(code: "unsupported", message: "Unknown option \(key)")
            }
        }
        return result
    }
    func adopt(_ connection: any Connection, lazy: Bool) async -> String {
        let id = mint(); let controlled = ControlledConnection(connection, lazy: lazy)
        connections[id] = controlled; await controlled.start(); return id
    }
    func adoptPeer(_ connection: any Connection, role: String, options: PeerOptions) async throws -> String {
        let peer = ControlledPeer(peer: try Peer(connection: connection, role: role, options: options))
        let id = mint(); peers[id] = peer; await peer.start(); return id
    }
    func process(_ r: JSONValue, raw: String) async throws -> JSONValue {
        let op = r["op"]?.string ?? ""
        let on = r["on"]?.string ?? ""
        let within = r["within_ms"]?.number.flatMap(Int.init) ?? 5000
        func text(_ name: String, _ fallback: String = "") -> String { r[name]?.string ?? fallback }
        func payload(_ name: String) throws -> Data? { try rawMember(name, in: raw).map { Data($0.utf8) } }
        func protocols() -> [String] { r["subprotocols"]?.array?.compactMap(\.string) ?? [] }
        func handle(_ id: String) -> JSONValue { .object(["handle": .string(id)]) }
        func unknown() -> PublicError { PublicError(code: "unknown_handle", message: on) }
        switch op {
        case "hello": return .object(["driver": .number("1"), "language": .string("swift"),
            "layers": .array([.string("seam"), .string("peer")]),
            "features": .array([.string("listen"), .string("pipe"), .string("lazy"), .string("propagator")])])
        case "bye", "reset": await reset(); return .object([:])
        case "conn.listen", "peer.listen":
            let settings = op == "peer.listen" ? try options(r["options"]) : nil
            let socket = try WebSocket.listen(limit: settings?.maxFrameBytes ?? r["limit"]?.number.flatMap(Int.init) ?? (1 << 20), subprotocols: protocols())
            let accepted = AsyncResult<any Connection>()
            Task { do { await accepted.resolve(.success(try await socket.accept())) } catch { await accepted.resolve(.failure(error)) } }
            let id = mint(); listeners[id] = Listener(socket: socket, accepted: accepted, options: settings)
            return .object(["handle": .string(id), "url": .string(socket.url)])
        case "conn.accept", "peer.accept":
            guard let listener = listeners[on] else { throw unknown() }
            let connection = try await withTimeout(milliseconds: within) { try await listener.accepted.value() }
            if op == "peer.accept" {
                let id = try await adoptPeer(connection, role: "server", options: listener.options ?? .init())
                return .object(["handle": .string(id), "subprotocol": .string(connection.subprotocol)])
            }
            return handle(await adopt(connection, lazy: text("consume") == "lazy"))
        case "conn.dial", "peer.dial":
            let settings = op == "peer.dial" ? try options(r["options"]) : nil
            let connection = try await WebSocket.dial(url: text("url"), limit: settings?.maxFrameBytes ?? r["limit"]?.number.flatMap(Int.init) ?? (1 << 20), subprotocols: protocols())
            if op == "peer.dial" {
                let id = try await adoptPeer(connection, role: "client", options: settings!)
                return .object(["handle": .string(id), "subprotocol": .string(connection.subprotocol)])
            }
            return handle(await adopt(connection, lazy: text("consume") == "lazy"))
        case "conn.pipe":
            let pair = Pipe.pair(limit: r["limit"]?.number.flatMap(Int.init) ?? (1 << 20))
            let a = await adopt(pair.0, lazy: text("consume") == "lazy")
            let b = await adopt(pair.1, lazy: text("consume") == "lazy")
            return .object(["a": .string(a), "b": .string(b)])
        case "conn.send":
            guard let c = connections[on] else { throw unknown() }
            let frame = Frame(kind: text("kind") == "binary" ? .binary : .text,
                data: text("kind") == "binary" ? Data(base64Encoded: text("base64")) ?? Data() : Data(text("text").utf8))
            try await withTimeout(milliseconds: within) { try await c.connection.send(frame) }
        case "conn.receive":
            guard let c = connections[on] else { throw unknown() }
            let frame = try await c.receive(within: within)
            return frame.kind == .text ? .object(["kind": .string("text"), "text": .string(String(decoding: frame.data, as: UTF8.self))]) : .object(["kind": .string("binary"), "base64": .string(frame.data.base64EncodedString())])
        case "conn.close":
            guard let c = connections[on] else { throw unknown() }
            let code = r["code"]?.number.flatMap(Int.init) ?? 1000; let reason = text("reason")
            try await withTimeout(milliseconds: within) { try await c.connection.close(code: code, reason: reason) }
        case "conn.abort":
            guard let c = connections[on] else { throw unknown() }; await c.connection.abort()
        case "conn.await_close":
            guard let c = connections[on] else { throw unknown() }
            let closed = try await c.awaitClose(within: within)
            return .object(["code": .number(String(closed.code)), "reason": .string(closed.reason)])
        case "peer.over":
            guard let c = connections[on] else { throw unknown() }
            return handle(try await adoptPeer(c.connection, role: text("role"), options: options(r["options"])))
        case "peer.handle":
            guard let p = peers[on] else { throw unknown() }
            let behavior = r["behavior"] ?? .object([:]); let method = text("method")
            let behaviorRaw = try rawMember("behavior", in: raw) ?? "{}"
            try await p.peer.handle(method: method) { context, peer, params in
                var started: [String: JSONValue] = ["id": .string(context.id), "method": .string(method), "phase": .string("started")]
                if let meta = context.meta, !meta.isEmpty { started["meta"] = meta.jsonValue }
                await p.requests.put(.object(started))
                var ended = started; ended["phase"] = .string("ended"); ended.removeValue(forKey: "meta")
                do {
                    let result = try await runBehavior(behavior, raw: behaviorRaw, context: context, peer: peer, controlled: p, params: params)
                    ended["outcome"] = .string("ok"); await p.requests.put(.object(ended)); return result
                } catch {
                    ended["outcome"] = .string(error is CancellationError ? "cancelled" : "error")
                    await p.requests.put(.object(ended)); throw error
                }
            }
        case "peer.on_event":
            guard let p = peers[on] else { throw unknown() }; let behavior = text("behavior", "record")
            try await p.peer.onEvent(name: text("name")) { _, peer, _ in
                if behavior == "block" { _ = try await peer.awaitClose() }
                if behavior == "panic" { throw PublicError(code: "internal", message: "Test handler gave up") }
            }
        case "peer.call":
            guard let p = peers[on] else { throw unknown() }
            let method = text("method"); let params = try payload("params") ?? Data("null".utf8)
            let timeout = r["timeout_ms"]?.number.flatMap(Int.init)
            let meta = try r["meta"].map { try Metadata(jsonValue: $0) }
            let result = AsyncResult<Data?>()
            let task = Task { () throws -> Data? in
                do { let value = try await p.peer.call(method: method, params: params, timeoutMilliseconds: timeout, meta: meta); await result.resolve(.success(value)); return value }
                catch { await result.resolve(.failure(error)); throw error }
            }
            let id = mint(); calls[id] = Call(task: task, result: result); return handle(id)
        case "call.await":
            guard let call = calls[on] else { throw unknown() }
            do {
                let value = try await withTimeout(milliseconds: within) { try await call.result.value() }
                return .object(["result": try value.map(JSONValue.init(data:)) ?? .null])
            } catch let error as PublicError where error.code == "timeout" { throw error }
            catch { return .object(["error": errorValue(error)]) }
        case "call.cancel": guard let call = calls[on] else { throw unknown() }; call.task.cancel()
        case "peer.emit":
            guard let p = peers[on] else { throw unknown() }
            try await p.peer.emit(name: text("event"), data: payload("data") ?? Data("null".utf8), meta: try r["meta"].map { try Metadata(jsonValue: $0) })
        case "peer.await_event":
            guard let p = peers[on] else { throw unknown() }; let name = text("name")
            var result = try await p.events.take(within: within) { $0["name"]?.string?.utf8.elementsEqual(name.utf8) == true }.object!
            result.removeValue(forKey: "name"); return .object(result)
        case "peer.await_request":
            guard let p = peers[on] else { throw unknown() }; let method = text("method"); let phase = text("phase")
            return try await p.requests.take(within: within) { $0["method"]?.string?.utf8.elementsEqual(method.utf8) == true && $0["phase"]?.string == phase }
        case "peer.identity":
            guard let p = peers[on] else { throw unknown() }
            try await p.peer.installIdentity(DeclarationIdentity(path: text("path"), digest: r["digest"]?.string))
        case "peer.check_identity":
            guard let p = peers[on] else { throw unknown() }
            try await p.peer.checkIdentity(DeclarationIdentity(path: text("path"), digest: r["digest"]?.string))
        case "peer.close": guard let p = peers[on] else { throw unknown() }; await p.peer.close()
        case "peer.await_close":
            guard let p = peers[on] else { throw unknown() }
            let closed = try await withTimeout(milliseconds: within) { try await p.peer.awaitClose() }
            return .object(["clean": .bool(closed.code == 1000), "code": .number(String(closed.code))])
        case "peer.recorded_wire_witness": return try recordedWireWitness(withinMilliseconds: within)
        default: throw PublicError(code: "unsupported", message: op)
        }
        return .object([:])
    }
}

func runBehavior(_ behavior: JSONValue, raw: String, context: RequestContext, peer: Peer, controlled: ControlledPeer, params: Data?) async throws -> Data? {
    func bytes(_ key: String) throws -> Data? { try rawMember(key, in: raw).map { Data($0.utf8) } }
    switch behavior["kind"]?.string {
    case "echo": return params ?? Data("null".utf8)
    case "return": return try bytes("value") ?? Data("null".utf8)
    case "fail": throw PublicError(code: behavior["code"]?.string ?? "failed", message: behavior["message"]?.string ?? "Failed", data: try bytes("data"))
    case "wait": await context.cancellation.wait(); throw CancellationError()
    case "hold":
        let name = behavior["until"]?.string
        _ = try await controlled.events.take(within: 60_000) { $0["name"]?.string == name }
        return try bytes("value")
    case "panic": throw PublicError(code: "internal", message: "Internal error")
    case "reverse": return try await peer.call(method: behavior["method"]?.string ?? "", params: bytes("params") ?? params, context: context)
    case "emit": try await peer.emit(name: behavior["event"]?.string ?? "", data: bytes("data"), context: context); return try bytes("then")
    default: throw PublicError(code: "invalid", message: "Unknown canned behavior")
    }
}

func errorValue(_ error: any Error) -> JSONValue {
    if let error = error as? PublicError {
        var value: [String: JSONValue] = ["code": .string(error.code), "message": .string(error.message)]
        if let data = error.data, let valueData = try? JSONValue(data: data) { value["data"] = valueData }
        return .object(value)
    }
    if let error = error as? CloseError { return .object(["code": .string("closed"), "message": .string("Connection closed"), "close_code": .number(String(error.code)), "reason": .string(error.reason)]) }
    if let error = error as? DuplexError, error == .closed {
        return .object(["code": .string("closed"), "message": .string("Connection closed")])
    }
    return .object(["code": .string(error is CancellationError ? "cancelled" : "failed"), "message": .string(String(describing: error))])
}

@main struct Main {
    static func main() async {
        let driver = Driver()
        while let line = readLine() {
            var answer: [String: JSONValue] = [:]
            do {
                let request = try JSONValue(parsing: line)
                answer["id"] = request["id"] ?? .null
                do { answer["ok"] = try await driver.process(request, raw: line) }
                catch { answer["error"] = errorValue(error) }
                FileHandle.standardOutput.write(Data((JSONValue.object(answer).encoded() + "\n").utf8))
                if request["op"]?.string == "bye" { break }
            } catch {
                answer["error"] = errorValue(error)
                FileHandle.standardOutput.write(Data((JSONValue.object(answer).encoded() + "\n").utf8))
            }
        }
        await driver.reset()
    }
}
