import Foundation
import NightseamDuplex

// Only admission is waited for on the serial dispatch worker. The actual peer
// request runs independently and retains the local carrier's active-work slot.
private final class PeerWireHandoff: @unchecked Sendable {
    let admission = DispatchSemaphore(value: 0)
    let withdrawal = DispatchSemaphore(value: 0)
    private let lock = NSLock()
    private var admitted = false
    private var withdrawn = false
    func admit() {
        let signal = lock.withLock { if admitted { return false }; admitted = true; return true }
        if signal { admission.signal() }
    }
    func withdraw() {
        let signal = lock.withLock { if withdrawn { return false }; withdrawn = true; return true }
        if signal { withdrawal.signal() }
    }
}

/// The existing Peer owns the socket; its Wire owns bounded relative dispatch.
/// Reusing the local dispatch primitive keeps cancellation reservations, return
/// identity, namespace selection and handler lifetime identical for both roots.
final class PeerWire: Wire, @unchecked Sendable {
    private let peer: Peer
    private let front: any Wire
    private let back: any Wire
    private let lock = NSLock()
    private var calls: [ObjectIdentifier: PeerWireHandoff] = [:]

    private init(peer: Peer) throws {
        self.peer = peer
        let options = peer.options
        (front, back) = try wirePair(options: WireOptions(queueCapacity: options.queueCapacity,
            maxPendingRequests: options.maxPendingRequests, maxConcurrentHandlers: options.maxConcurrentHandlers,
            requestTimeoutMilliseconds: options.requestTimeoutMilliseconds,
            writeTimeoutMilliseconds: options.writeTimeoutMilliseconds, maxFrameBytes: options.maxFrameBytes))
    }

    static func attach(peer: Peer) async throws -> PeerWire {
        let wire = try PeerWire(peer: peer)
        try wire.installOutbound()
        await peer.installFallback(request: { [weak wire] method in
            guard let wire, let path = try? Path.decode(method) else { return nil }
            return { context, _, params in
                try await callWire(wire.back, path: path, params: params, context: context,
                    timeoutMilliseconds: peer.options.requestTimeoutMilliseconds, meta: context.meta)
            }
        }, event: { [weak wire] name in
            guard let wire, let path = try? Path.decode(name) else { return nil }
            return { context, _, data in try emitWire(wire.back, path: path, data: data, context: context, meta: context.meta) }
        })
        Task { [weak wire] in
            if let ending = try? await peer.awaitClose() { try? wire?.front.close(code: ending.code, reason: ending.reason) }
        }
        return wire
    }

    private func installOutbound() throws {
        _ = try back.receive(path: [], receiver: Receiver(namespace: true, message: { [weak self] path, message in
            self?.dispatch(path: path, message: message)
        }, closed: { [peer] code, reason in Task { await peer.close(code: code, reason: reason) } }))
    }

    private func dispatch(path: [String], message: Message) {
        let context = (message.returnAddress?.wire as? any WireContextProvider)?.wireContext ?? RequestContext(
            id: message.frame.id ?? "", method: Path.encode(path), traceparent: message.frame.traceparent,
            tracestate: message.frame.tracestate, meta: message.frame.meta)
        switch message.frame.kind {
        case .request:
            guard let address = message.returnAddress else { return }
            let key = ObjectIdentifier(address)
            let handoff = PeerWireHandoff()
            lock.withLock { calls[key] = handoff }
            Task { [self] in
                defer {
                    handoff.admit(); handoff.withdraw()
                    _ = lock.withLock { calls.removeValue(forKey: key) }
                }
                do {
                    let result = try await peer.call(method: Path.encode(path), params: message.frame.params,
                        context: context, meta: message.frame.meta, preservingTrace: true,
                        admitted: { handoff.admit() }, withdrawn: { handoff.withdraw() })
                    sendWireResponse(message, result: result)
                } catch let error as PublicError {
                    sendWireResponse(message, error: ProfileError(code: error.code, message: error.message, data: error.data))
                } catch is CancellationError {
                    sendWireResponse(message, error: ProfileError(code: "cancelled", message: "Request cancelled"))
                } catch {
                    sendWireResponse(message, error: ProfileError(code: "disconnected", message: "Connection ended; outcome may be unknown"))
                }
            }
            if handoff.admission.wait(timeout: .now() + .milliseconds(peer.options.writeTimeoutMilliseconds)) == .timedOut {
                try? front.close(code: 4011, reason: "Peer wire admission stalled")
            }
        case .cancel:
            guard let address = message.returnAddress else { return }
            let handoff = lock.withLock { calls[ObjectIdentifier(address)] }
            context.cancellation.cancel()
            if let handoff, handoff.withdrawal.wait(timeout: .now() + .milliseconds(peer.options.writeTimeoutMilliseconds)) == .timedOut {
                try? front.close(code: 4011, reason: "Peer wire cancellation stalled")
            }
        case .event:
            let handoff = PeerWireHandoff()
            Task { [peer, front] in
                defer { handoff.admit() }
                do {
                    try await peer.emit(name: Path.encode(path), data: message.frame.data, context: context,
                                        meta: message.frame.meta, preservingTrace: true)
                } catch { try? front.close(code: 4011, reason: "Peer wire event admission failed") }
            }
            if handoff.admission.wait(timeout: .now() + .milliseconds(peer.options.writeTimeoutMilliseconds)) == .timedOut {
                try? front.close(code: 4011, reason: "Peer wire event admission stalled")
            }
        case .response: break
        }
    }

    func send(path: [String], message: Message) throws { try front.send(path: path, message: message) }
    func receive(path: [String], receiver: Receiver) throws -> Detach { try front.receive(path: path, receiver: receiver) }
    func close(code: Int, reason: String) throws { try front.close(code: code, reason: reason) }
}
