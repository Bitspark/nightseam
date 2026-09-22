import Bitwire
import Foundation

/// Explicit path dispatch over one attachment to a borrowed Bitwire endpoint.
public final class Dispatcher: Wire, @unchecked Sendable {
    private struct Route: Hashable { let path: Data; let prefix: Bool }
    private struct Registration: Sendable { let id: UUID; let path: [String]; let receiver: Receiver }
    private struct Captured {
        weak var address: ReturnAddress?
        var calls: [String: Registration]
    }
    private let root: any Endpoint
    private let lock = NSLock()
    private var routes: [Route: Registration] = [:]
    private var captured: [ObjectIdentifier: Captured] = [:]
    private var ended = false
    private var detach: Detach?

    public init(_ root: any Endpoint) throws {
        self.root = root
        let acquired = try root.receive(receiver: Receiver(message: { [weak self] path, message in
            self?.dispatch(path, message)
        }, closed: { [weak self] code, reason in self?.close(code: code, reason: reason) }))
        let active = lock.withLock { if ended { return false }; detach = acquired; return true }
        if !active { acquired(); throw DuplexError.closed }
    }
    deinit { detach?() }
    public func send(path: [String], message: Message) throws {
        guard lock.withLock({ !ended }) else { throw DuplexError.closed }
        try root.send(path: path, message: message)
    }
    public func register(path: [String], receiver: Receiver) throws -> Detach {
        try register(path: path, prefix: false, receiver: receiver)
    }
    public func registerPrefix(path: [String], receiver: Receiver) throws -> Detach {
        try register(path: path, prefix: true, receiver: receiver)
    }
    private func register(path: [String], prefix: Bool, receiver: Receiver) throws -> Detach {
        let route = Route(path: Path.key(path), prefix: prefix)
        let registration = Registration(id: UUID(), path: path, receiver: receiver)
        try lock.withLock {
            guard !ended else { throw DuplexError.closed }
            guard routes[route] == nil else { throw DuplexError.receiverExists }
            routes[route] = registration
        }
        return { [weak self] in self?.lock.withLock {
            if self?.routes[route]?.id == registration.id { self?.routes.removeValue(forKey: route) }
        } }
    }
    private func dispatch(_ path: [String], _ message: Message) {
        let selected: Registration? = lock.withLock {
            guard !ended else { return nil }
            captured = captured.filter { $0.value.address != nil }
            if message.frame.kind == .cancel, let address = message.returnAddress, let id = message.frame.id {
                return captured[ObjectIdentifier(address)]?.calls.removeValue(forKey: id)
            }
            var selected = routes[Route(path: Path.key(path), prefix: false)]
            if selected == nil {
                for (route, candidate) in routes where route.prefix && Path.hasPrefix(path, candidate.path) {
                    if selected == nil || candidate.path.count > selected!.path.count { selected = candidate }
                }
            }
            if let selected, message.frame.kind == .request, let address = message.returnAddress, let id = message.frame.id {
                let key = ObjectIdentifier(address)
                if captured[key] == nil { captured[key] = Captured(address: address, calls: [:]) }
                captured[key]?.calls[id] = selected
            }
            return selected
        }
        if let selected { selected.receiver.message(path, message) }
        else if message.frame.kind == .request, let address = message.returnAddress {
            try? address.wire.send(path: [], message: Message(frame: ProfileFrame(kind: .response, id: message.frame.id,
                error: ProfileError(code: "method_not_found", message: "Unknown method"),
                traceparent: message.frame.traceparent, tracestate: message.frame.tracestate)))
        }
    }
    public func select(path: [String]) -> any Endpoint { RoutedEndpoint(owner: self, path: path) }
    public func close(code: Int = 1000, reason: String = "done") {
        let held: ([Registration], Detach?) = lock.withLock {
            guard !ended else { return ([], nil) }
            ended = true
            let held = (Array(routes.values), detach)
            routes.removeAll(); captured.removeAll(); detach = nil
            return held
        }
        held.1?()
        for registration in held.0 { registration.receiver.closed(code, reason) }
    }
}

private final class RoutedEndpoint: Endpoint, @unchecked Sendable {
    let owner: Dispatcher
    let path: [String]
    let lock = NSRecursiveLock()
    var ended = false
    var attachment: (UUID, Receiver, Detach)?
    init(owner: Dispatcher, path: [String]) { self.owner = owner; self.path = path }
    deinit { attachment?.2() }
    func send(path: [String], message: Message) throws {
        guard lock.withLock({ !ended }) else { throw DuplexError.closed }
        try owner.send(path: self.path + path, message: message)
    }
    func receive(receiver: Receiver) throws -> Detach {
        let id = UUID()
        try lock.withLock {
            guard !ended else { throw DuplexError.closed }
            guard attachment == nil else { throw DuplexError.receiverExists }
            let prefix = path
            let detach = try owner.registerPrefix(path: prefix, receiver: Receiver(message: { delivered, message in
                receiver.message(Array(delivered.dropFirst(prefix.count)), message)
            }, closed: { [weak self] code, reason in try? self?.close(code: code, reason: reason) }))
            attachment = (id, receiver, detach)
        }
        return { [weak self] in
            guard let self else { return }
            let detach: Detach? = lock.withLock {
                guard attachment?.0 == id else { return nil }
                let held = attachment?.2; attachment = nil; return held
            }
            detach?()
        }
    }
    func close(code: Int, reason: String) throws {
        let held: (UUID, Receiver, Detach)? = lock.withLock {
            guard !ended else { return nil }
            ended = true; let held = attachment; attachment = nil; return held
        }
        held?.2(); held?.1.closed(code, reason)
    }
}
