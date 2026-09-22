import Bitwire
import Foundation

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

}

public func mount(_ children: [String: any Endpoint]) -> any Endpoint { MountedWire(Array(children)) }
/// The pair form admits distinct canonically equivalent Unicode keys, which a
/// Swift String-keyed Dictionary itself cannot represent.
public func mount(_ children: [(String, any Endpoint)]) -> any Endpoint { MountedWire(children) }

private final class MountedWire: Endpoint, @unchecked Sendable {
    private struct Registration { let receiver: Receiver; var detach: Detach? }
    private let lock = NSLock()
    private let children: [Data: (String, any Endpoint)]
    private var ended = false
    private var registrations: [UUID: Registration] = [:]
    init(_ children: [(String, any Endpoint)]) {
        self.children = Dictionary(children.map { (Data($0.0.utf8), $0) }, uniquingKeysWith: { _, last in last })
    }
    private func destination(_ path: [String]) throws -> any Endpoint {
        try lock.withLock {
            guard !ended else { throw DuplexError.closed }
            guard let first = path.first, let child = children[Data(first.utf8)] else { throw DuplexError.noRoute }
            return child.1
        }
    }
    func send(path: [String], message: Message) throws {
        try destination(path).send(path: Array(path.dropFirst()), message: message)
    }
    func receive(receiver: Receiver) throws -> Detach {
        let id = UUID()
        let group = MountGroup(count: children.count)
        try lock.withLock {
            guard !ended else { throw DuplexError.closed }
            guard registrations.isEmpty else { throw DuplexError.receiverExists }
            registrations[id] = Registration(receiver: receiver, detach: { group.detach() })
        }
        do {
            for (_, (key, child)) in children {
                let detach = try child.receive(receiver: Receiver(message: { path, message in receiver.message([key] + path, message) },
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
