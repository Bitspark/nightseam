import Foundation

public enum ProfileKind: String, Sendable { case request, response, event, cancel }
public struct ProfileError: Sendable {
    public var code: String
    public var message: String
    public var data: Data?
    public init(code: String, message: String, data: Data? = nil) {
        self.code = code; self.message = message; self.data = data
    }
}

/// JSON fields retain their bytes: nil is absent, Data("null".utf8) is null.
public struct ProfileFrame: Sendable {
    public var version: Int
    public var kind: ProfileKind
    public var id: String?
    public var params: Data?
    public var result: Data?
    public var error: ProfileError?
    public var data: Data?
    public var traceparent: String?
    public var tracestate: String?
    public var meta: Metadata?
    public init(version: Int = 1, kind: ProfileKind, id: String? = nil,
                params: Data? = nil, result: Data? = nil, error: ProfileError? = nil,
                data: Data? = nil, traceparent: String? = nil, tracestate: String? = nil,
                meta: Metadata? = nil) {
        self.version = version; self.kind = kind; self.id = id
        self.params = params; self.result = result; self.error = error; self.data = data
        self.traceparent = traceparent; self.tracestate = tracestate; self.meta = meta
    }
}

public final class ReturnAddress: Sendable {
    public let wire: any Wire
    public init(wire: any Wire) { self.wire = wire }
}
public struct Message: Sendable {
    public var frame: ProfileFrame
    public var returnAddress: ReturnAddress?
    public init(frame: ProfileFrame, returnAddress: ReturnAddress? = nil) {
        self.frame = frame; self.returnAddress = returnAddress
    }
}
public struct Receiver: Sendable {
    public var namespace: Bool
    public var message: @Sendable ([String], Message) -> Void
    public var closed: @Sendable (Int, String) -> Void
    public init(namespace: Bool = false,
                message: @escaping @Sendable ([String], Message) -> Void,
                closed: @escaping @Sendable (Int, String) -> Void = { _, _ in }) {
        self.namespace = namespace; self.message = message; self.closed = closed
    }
}
public typealias Detach = @Sendable () -> Void
public protocol Wire: Sendable {
    func send(path: [String], message: Message) throws
    func receive(path: [String], receiver: Receiver) throws -> Detach
    func close(code: Int, reason: String) throws
}

public enum Path {
    public static func encode(_ path: [String]) -> String {
        path.map { "\($0.utf8.count):\($0)" }.joined()
    }
    /// Data keys preserve scalar spelling. Swift String equality would normalize it.
    public static func key(_ path: [String]) -> Data { Data(encode(path).utf8) }
    public static func decode(_ encoded: String) throws -> [String] {
        let bytes = Array(encoded.utf8)
        var position = 0
        var result: [String] = []
        while position < bytes.count {
            let start = position
            var count = 0
            while position < bytes.count && bytes[position] != 58 {
                let digit = bytes[position]
                guard digit >= 48 && digit <= 57 else { throw DuplexError.invalidPath }
                let (product, overflow) = count.multipliedReportingOverflow(by: 10)
                let (next, additionOverflow) = product.addingReportingOverflow(Int(digit - 48))
                guard !overflow && !additionOverflow else { throw DuplexError.invalidPath }
                count = next; position += 1
            }
            guard position > start && position < bytes.count,
                  position - start == 1 || bytes[start] != 48 else { throw DuplexError.invalidPath }
            position += 1
            guard count <= bytes.count - position,
                  let segment = String(bytes: bytes[position..<(position + count)], encoding: .utf8)
            else { throw DuplexError.invalidPath }
            result.append(segment); position += count
        }
        return result
    }
    public static func hasPrefix(_ path: [String], _ prefix: [String]) -> Bool {
        path.count >= prefix.count && zip(path, prefix).allSatisfy { $0.utf8.elementsEqual($1.utf8) }
    }
}

public func at(_ wire: any Wire, path: [String]) -> any Wire { SelectedWire(root: wire, prefix: path) }
private struct SelectedWire: Wire {
    let root: any Wire
    let prefix: [String]
    func send(path: [String], message: Message) throws { try root.send(path: prefix + path, message: message) }
    func receive(path: [String], receiver: Receiver) throws -> Detach {
        try root.receive(path: prefix + path, receiver: Receiver(namespace: receiver.namespace,
            message: { delivered, message in receiver.message(Array(delivered.dropFirst(prefix.count)), message) },
            closed: receiver.closed))
    }
    func close(code: Int, reason: String) throws { try root.close(code: code, reason: reason) }
}

public func mount(_ children: [String: any Wire]) -> any Wire { MountedWire(Array(children)) }
/// The pair form admits distinct canonically equivalent Unicode keys, which a
/// Swift String-keyed Dictionary itself cannot represent.
public func mount(_ children: [(String, any Wire)]) -> any Wire { MountedWire(children) }

private final class MountedWire: Wire, @unchecked Sendable {
    private struct Registration { let receiver: Receiver; var detach: Detach? }
    private let lock = NSLock()
    private let children: [Data: (String, any Wire)]
    private var ended = false
    private var registrations: [UUID: Registration] = [:]
    init(_ children: [(String, any Wire)]) {
        self.children = Dictionary(children.map { (Data($0.0.utf8), $0) }, uniquingKeysWith: { _, last in last })
    }
    private func destination(_ path: [String]) throws -> any Wire {
        try lock.withLock {
            guard !ended else { throw DuplexError.closed }
            guard let first = path.first, let child = children[Data(first.utf8)] else { throw DuplexError.noRoute }
            return child.1
        }
    }
    func send(path: [String], message: Message) throws {
        try destination(path).send(path: Array(path.dropFirst()), message: message)
    }
    func receive(path: [String], receiver: Receiver) throws -> Detach {
        if path.isEmpty && receiver.namespace { return try receiveNamespace(receiver) }
        let child = try destination(path)
        let key = path[0]
        let id = UUID()
        try lock.withLock {
            guard !ended else { throw DuplexError.closed }
            registrations[id] = Registration(receiver: receiver)
        }
        do {
            let detach = try child.receive(path: Array(path.dropFirst()), receiver: Receiver(
                namespace: receiver.namespace,
                message: { path, message in receiver.message([key] + path, message) },
                closed: { [weak self] code, reason in self?.remove(id, close: (code, reason)) }))
            let active = lock.withLock {
                guard registrations[id] != nil else { return false }
                registrations[id]?.detach = detach; return true
            }
            guard active else { detach(); throw DuplexError.closed }
            return { [weak self] in self?.remove(id) }
        } catch { remove(id); throw error }
    }
    private func receiveNamespace(_ receiver: Receiver) throws -> Detach {
        let id = UUID()
        let group = MountGroup(count: children.count)
        try lock.withLock {
            guard !ended else { throw DuplexError.closed }
            registrations[id] = Registration(receiver: receiver, detach: { group.detach() })
        }
        do {
            for (_, (key, _)) in children {
                let detach = try receive(path: [key], receiver: Receiver(namespace: true, message: receiver.message,
                    closed: { [weak self] code, reason in
                        if group.childEnded() { self?.remove(id, close: (code, reason)) }
                    }))
                guard group.add(detach) else { throw DuplexError.closed }
            }
            return { [weak self] in self?.remove(id) }
        } catch { remove(id); throw error }
    }
    private func remove(_ id: UUID, close: (Int, String)? = nil) {
        let registration = lock.withLock { registrations.removeValue(forKey: id) }
        registration?.detach?()
        if let close, let registration { registration.receiver.closed(close.0, close.1) }
    }
    func close(code: Int, reason: String) throws {
        let held = lock.withLock {
            ended = true
            let held = Array(registrations.values); registrations.removeAll(); return held
        }
        for registration in held { registration.detach?() }
        // Child registrations feed the root namespace's closure. The group
        // detach above disables those paths so the root receives only one end.
        for registration in held { registration.receiver.closed(code, reason) }
    }
}

private final class MountGroup: @unchecked Sendable {
    let lock = NSLock()
    var remaining: Int
    var ended = false
    var detaches: [Detach] = []
    init(count: Int) { remaining = count }
    func add(_ detach: @escaping Detach) -> Bool {
        let active = lock.withLock { if ended { return false }; detaches.append(detach); return true }
        if !active { detach() }; return active
    }
    func childEnded() -> Bool {
        lock.withLock { guard !ended else { return false }; remaining -= 1; return remaining == 0 }
    }
    func detach() {
        let held = lock.withLock { ended = true; let held = detaches; detaches = []; return held }
        for detach in held { detach() }
    }
}
