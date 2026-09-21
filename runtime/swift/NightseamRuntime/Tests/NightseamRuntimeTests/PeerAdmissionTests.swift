import Foundation
import Testing
import NightseamDuplex
@testable import NightseamRuntime

/// An explicitly held writer makes output admission deterministic. Incoming
/// frames use the real bounded pipe, independently of that outgoing gate.
private final class AdmissionConnection: Connection, @unchecked Sendable {
    private let lock = NSLock()
    private let inbound: any Connection
    private let feeder: any Connection
    private var ended = false
    private var frames: [Frame] = []
    let sendEntered = AsyncResult<Void>()
    let releaseWriter = AsyncResult<Void>()
    let thirdPublished = AsyncResult<Void>()
    let aborted = AsyncResult<Void>()

    init() { (inbound, feeder) = Pipe.pair() }

    var isClosed: Bool { lock.withLock { ended } }
    var published: [Frame] { lock.withLock { frames } }

    func send(_ frame: Frame) async throws {
        await sendEntered.resolve(.success(()))
        try await releaseWriter.value()
        let count = try lock.withLock {
            guard !ended else { throw DuplexError.closed }
            frames.append(frame); return frames.count
        }
        if count >= 3 { await thirdPublished.resolve(.success(())) }
    }

    func receive() async throws -> Frame { try await inbound.receive() }

    func close(code: Int, reason: String) async throws {
        lock.withLock { ended = true }
        await releaseWriter.resolve(.failure(DuplexError.closed))
        await inbound.abort(); await feeder.abort()
    }

    func abort() async {
        try? await close(code: 1006, reason: "")
        await aborted.resolve(.success(()))
    }

    func inject(_ text: String) async throws {
        try await feeder.send(Frame(kind: .text, data: Data(text.utf8)))
    }
}

private func admissionOptions() -> PeerOptions {
    var options = PeerOptions()
    options.queueCapacity = 1
    options.maxConcurrentHandlers = 1
    options.writeTimeoutMilliseconds = 2_000
    options.requestTimeoutMilliseconds = 5_000
    return options
}

private func fillAdmissionQueue(_ peer: Peer, _ connection: AdmissionConnection) async throws {
    try await peer.emit(name: "first", data: nil)
    try await withTimeout(milliseconds: 1_000) { try await connection.sendEntered.value() }
    try await peer.emit(name: "second", data: nil)
}

private func assertAdmissionCarrierUsable(_ peer: Peer, _ connection: AdmissionConnection) async throws {
    #expect(!connection.isClosed)
    await connection.releaseWriter.resolve(.success(()))
    try await peer.emit(name: "after", data: nil)
    try await withTimeout(milliseconds: 1_000) { try await connection.thirdPublished.value() }
    let frames = try connection.published.map { try JSONValue(parsing: String(decoding: $0.data, as: UTF8.self)) }
    #expect(frames.count == 3)
    #expect(frames.allSatisfy { $0["kind"]?.string == "event" })
    #expect(frames.map { $0["event"]?.string } == ["first", "second", "after"])
}

@Suite struct PeerAdmissionTests {
    @Test func unadmittedCallDeadlineDoesNotPublishOrEndCarrier() async throws {
        let connection = AdmissionConnection()
        let peer = try Peer(connection: connection, role: "client", options: admissionOptions())
        await peer.start()
        do {
            try await fillAdmissionQueue(peer, connection)
            let started = ContinuousClock.now
            do {
                _ = try await withTimeout(milliseconds: 700) {
                    try await peer.call(method: "must-not-publish", params: nil, timeoutMilliseconds: 75)
                }
                Issue.record("Unadmitted call ignored its deadline")
            } catch let error as PublicError { #expect(error.code == "request_timeout") }
            #expect(started.duration(to: .now) < .milliseconds(700))
            try await assertAdmissionCarrierUsable(peer, connection)
        } catch { await peer.close(); throw error }
        await peer.close()
    }

    @Test func unadmittedContextCancellationDoesNotPublishOrEndCarrier() async throws {
        let connection = AdmissionConnection()
        let peer = try Peer(connection: connection, role: "client", options: admissionOptions())
        let context = RequestContext()
        await peer.start()
        do {
            try await fillAdmissionQueue(peer, connection)
            let started = AsyncResult<Void>()
            let outcome = AsyncResult<Data?>()
            let call = Task {
                await started.resolve(.success(()))
                do { await outcome.resolve(.success(try await peer.call(method: "must-not-publish", params: nil, context: context))) }
                catch { await outcome.resolve(.failure(error)) }
            }
            try await started.value()
            // The queue stays full throughout this window; cancellation is
            // exercised while admission is suspended, after the initial check.
            try await Task.sleep(for: .milliseconds(50))
            context.cancellation.cancel()
            do {
                _ = try await withTimeout(milliseconds: 700) { try await outcome.value() }
                Issue.record("Unadmitted call ignored context cancellation")
            } catch let error as PublicError { #expect(error.code == "cancelled") }
            catch is CancellationError {} // cancellation may retain its local cause
            call.cancel()
            await call.value
            try await assertAdmissionCarrierUsable(peer, connection)
        } catch { await peer.close(); throw error }
        await peer.close()
    }

    @Test(arguments: [false, true])
    func requestRejectionDoesNotWaitForTheWriter(busy: Bool) async throws {
        let connection = AdmissionConnection()
        let peer = try Peer(connection: connection, role: "server", options: admissionOptions())
        let handlerEntered = AsyncResult<Void>()
        if busy {
            try await peer.handle(method: "hold") { context, _, _ in
                await handlerEntered.resolve(.success(()))
                await context.cancellation.wait()
                throw CancellationError()
            }
        }
        await peer.start()
        do {
            if busy {
                try await connection.inject(#"{"version":1,"kind":"request","id":"c:1","method":"hold","params":null}"#)
                try await withTimeout(milliseconds: 1_000) { try await handlerEntered.value() }
            }
            try await fillAdmissionQueue(peer, connection)
            let started = ContinuousClock.now
            let method = busy ? "hold" : "unknown"
            try await connection.inject("{\"version\":1,\"kind\":\"request\",\"id\":\"c:2\",\"method\":\"\(method)\",\"params\":null}")
            let closing = try await withTimeout(milliseconds: 700) { try await peer.awaitClose() }
            #expect(closing.code == 1006)
            #expect(started.duration(to: .now) < .milliseconds(700))
            try await withTimeout(milliseconds: 700) { try await connection.aborted.value() }
            #expect(connection.published.isEmpty)
        } catch { await peer.close(); throw error }
        await peer.close()
    }
}
