import Bitwire
import Foundation
import Testing
import NightseamDuplex
@testable import NightseamRuntime

private func pair(_ options: PeerOptions = .init()) async throws -> (Peer, Peer) {
    let connection = Pipe.pair()
    let a = try Peer(connection: connection.0, role: "client", options: options)
    let b = try Peer(connection: connection.1, role: "server", options: options)
    return (a, b)
}

@Test func peerPreservesRawPayloadAndSeparatePresence() async throws {
    let (a, b) = try await pair()
    try await b.handle(method: "echo") { _, _, data in
        return data
    }
    await a.start(); await b.start()
    let bytes = Data("{\"n\":1e3,\"precise\":9007199254740993}".utf8)
    #expect(try await a.call(method: "echo", params: bytes) == bytes)
    #expect(try await a.call(method: "echo", params: nil) == Data("null".utf8))
    #expect(try await a.call(method: "echo", params: Data("{}".utf8)) == Data("{}".utf8))
    #expect(try await a.call(method: "echo", params: Data("{\"value\":null}".utf8)) == Data("{\"value\":null}".utf8))
    await a.close(); await b.close()
}

@Test func reverseCallInheritsTraceButNoMetadata() async throws {
    let (a, b) = try await pair()
    let original = AsyncResult<RequestContext>()
    let reverse = AsyncResult<RequestContext>()
    try await a.handle(method: "back") { context, _, bytes in
        await reverse.resolve(.success(context)); return bytes
    }
    try await b.handle(method: "forward") { context, peer, bytes in
        await original.resolve(.success(context))
        return try await peer.call(method: "back", params: bytes, context: context)
    }
    await a.start(); await b.start()
    #expect(try await a.call(method: "forward", params: Data("7".utf8), meta: ["tenant": "one", "nightseam.private": "drop"]) == Data("7".utf8))
    let first = try await original.value(); let second = try await reverse.value()
    #expect(first.meta == ["tenant": "one"])
    #expect(second.meta == nil)
    #expect(first.traceparent?.split(separator: "-")[1] == second.traceparent?.split(separator: "-")[1])
    #expect(first.traceparent != second.traceparent)
    await a.close(); await b.close()
}

@Test func cancellationKeepsIgnoringHandlerBudgetUntilReturn() async throws {
    var options = PeerOptions(); options.maxConcurrentHandlers = 1
    let (a, b) = try await pair(options)
    let started = AsyncResult<RequestContext>(); let released = AsyncResult<Void>()
    try await b.handle(method: "hold") { context, _, _ in
        await started.resolve(.success(context)); try await released.value(); return Data("1".utf8)
    }
    try await b.handle(method: "echo") { _, _, data in data }
    await a.start(); await b.start()
    let task = Task { try await a.call(method: "hold", params: nil) }
    let context = try await started.value(); task.cancel()
    do { _ = try await task.value; Issue.record("Cancelled call succeeded") }
    catch let error as PublicError { #expect(error.code == "cancelled") }
    let limit = ContinuousClock.now.advanced(by: .seconds(2))
    while !context.cancellation.isCancelled, ContinuousClock.now < limit { try await Task.sleep(for: .milliseconds(1)) }
    #expect(context.cancellation.isCancelled)
    do { _ = try await a.call(method: "echo", params: nil); Issue.record("Ignoring handler released its slot") }
    catch let error as PublicError { #expect(error.code == "busy") }
    await released.resolve(.success(()))
    var recovered = false
    while ContinuousClock.now < limit {
        do { _ = try await a.call(method: "echo", params: nil); recovered = true; break }
        catch let error as PublicError where error.code == "busy" { try await Task.sleep(for: .milliseconds(1)) }
    }
    #expect(recovered)
    await a.close(); await b.close()
}

@Test func pendingLimitAndDeadlineLeaveConnectionUsable() async throws {
    var options = PeerOptions(); options.maxPendingRequests = 1
    let (a, b) = try await pair(options)
    let started = AsyncResult<Void>()
    try await b.handle(method: "wait") { context, _, _ in
        await started.resolve(.success(())); await context.cancellation.wait(); throw CancellationError()
    }
    try await b.handle(method: "echo") { _, _, data in data }
    await a.start(); await b.start()
    let task = Task { try await a.call(method: "wait", params: nil, timeoutMilliseconds: 100) }
    try await started.value()
    do { _ = try await a.call(method: "echo", params: nil); Issue.record("Pending limit was exceeded") }
    catch let error as PublicError { #expect(error.code == "busy") }
    do { _ = try await task.value; Issue.record("Deadline did not expire") }
    catch let error as PublicError { #expect(error.code == "request_timeout") }
    #expect(try await a.call(method: "echo", params: Data("null".utf8)) == Data("null".utf8))
    await a.close(); await b.close()
}

@Test func peerNamesRetainUnicodeSpelling() async throws {
    let (a, b) = try await pair()
    try await b.handle(method: "\u{e9}") { _, _, _ in Data("1".utf8) }
    try await b.handle(method: "e\u{301}") { _, _, _ in Data("2".utf8) }
    await a.start(); await b.start()
    #expect(try await a.call(method: "\u{e9}", params: nil) == Data("1".utf8))
    #expect(try await a.call(method: "e\u{301}", params: nil) == Data("2".utf8))
    await a.close(); await b.close()
}

@Test func metadataRetainsScalarDistinctKeys() async throws {
    let (a, b) = try await pair()
    let seen = AsyncResult<Metadata?>()
    try await b.handle(method: "meta") { context, _, _ in
        await seen.resolve(.success(context.meta)); return nil
    }
    await a.start(); await b.start()
    let metadata: Metadata = ["\u{e9}": "composed", "e\u{301}": "decomposed"]
    _ = try await a.call(method: "meta", params: nil, meta: metadata)
    let received = try await seen.value()
    #expect(received?["\u{e9}"] == "composed")
    #expect(received?["e\u{301}"] == "decomposed")
    #expect(received?.entries.count == 2)
    await a.close(); await b.close()
}

@Test func publicErrorsCrossWhilePrivateErrorsRemainPrivate() async throws {
    struct PrivateFailure: Error {}
    let (a, b) = try await pair()
    try await b.handle(method: "public") { _, _, _ in throw PublicError(code: "denied", message: "Denied", data: Data("1e3".utf8)) }
    try await b.handle(method: "private") { _, _, _ in throw PrivateFailure() }
    try await b.handle(method: "empty-code") { _, _, _ in throw PublicError(code: "", message: "invalid") }
    try await b.handle(method: "empty-message") { _, _, _ in throw PublicError(code: "invalid", message: "") }
    await a.start(); await b.start()
    do { _ = try await a.call(method: "public", params: nil); Issue.record("Public error succeeded") }
    catch let error as PublicError { #expect(error.code == "denied"); #expect(error.data == Data("1e3".utf8)) }
    do { _ = try await a.call(method: "private", params: nil); Issue.record("Private error succeeded") }
    catch let error as PublicError { #expect(error.code == "internal"); #expect(error.message == "Internal error") }
    for method in ["empty-code", "empty-message"] {
        do { _ = try await a.call(method: method, params: nil); Issue.record("Malformed public error succeeded") }
        catch let error as PublicError { #expect(error.code == "internal"); #expect(error.message == "Internal error") }
    }
    await a.close(); await b.close()
}

@Test func stalledInboundConsumerAbortsTheCarrier() async throws {
    var options = PeerOptions(); options.queueCapacity = 1; options.writeTimeoutMilliseconds = 30
    let (a, b) = try await pair(options)
    let entered = AsyncResult<Void>()
    try await b.onEvent(name: "block") { _, peer, _ in
        await entered.resolve(.success(())); _ = try await peer.awaitClose()
    }
    await a.start(); await b.start()
    try await a.emit(name: "block", data: nil)
    try await entered.value()
    try await a.emit(name: "block", data: nil)
    try await a.emit(name: "block", data: nil)
    let local = try await withTimeout(milliseconds: 2000) { try await b.awaitClose() }
    let remote = try await withTimeout(milliseconds: 2000) { try await a.awaitClose() }
    #expect(local.code == 1006)
    #expect(remote.code == 1006)
    await a.close(); await b.close()
}
