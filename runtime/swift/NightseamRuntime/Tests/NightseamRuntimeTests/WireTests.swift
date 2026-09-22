import Bitwire
import Foundation
import Testing
import NightseamDuplex
@testable import NightseamRuntime

private func wireTestData(_ text: String) -> Data { Data(text.utf8) }
private func wireAwait<Value: Sendable>(_ result: AsyncResult<Value>) async throws -> Value {
    try await withTimeout(milliseconds: 2_000) { try await result.value() }
}

private final class WireTestSink: Wire, Sendable {
    let receiveMessage: @Sendable (Message) -> Void
    init(_ receiveMessage: @escaping @Sendable (Message) -> Void) { self.receiveMessage = receiveMessage }
    func send(path: [String], message: Message) throws { receiveMessage(message) }
    func close(code: Int, reason: String) throws {}
}

private final class WireTestValues<Value: Sendable>: @unchecked Sendable {
    private let lock = NSLock()
    private var values: [Value] = []
    func append(_ value: Value) { lock.withLock { values.append(value) } }
    var all: [Value] { lock.withLock { values } }
}

@Suite struct LocalWireTests {
    @Test func malformedFramesAreRefusedBeforeAdmission() async throws {
        let (a, b) = try wirePair()
        defer { try? a.close(code: 1000, reason: "Done") }
        let address = ReturnAddress(wire: WireTestSink { _ in })
        #expect(throws: WireError.invalidMessage) {
            try a.send(path: ["call"], message: Message(frame: ProfileFrame(kind: .request, id: "bad", params: wireTestData("null")), returnAddress: address))
        }
        #expect(throws: (any Error).self) {
            try a.send(path: ["call"], message: Message(frame: ProfileFrame(kind: .request, id: "c:1"), returnAddress: address))
        }
        #expect(throws: (any Error).self) {
            try a.send(path: [], message: Message(frame: ProfileFrame(kind: .event, data: wireTestData("null"))))
        }
        #expect(throws: WireError.invalidMessage) {
            try a.send(path: [], message: Message(frame: ProfileFrame(kind: .response, id: "c:1", result: wireTestData("null"))))
        }
        let bRoutes = try Dispatcher(b)
        defer { bRoutes.close() }
        _ = try handleWire(bRoutes, path: ["echo"]) { _, params in params }
        #expect(throws: DuplexError.receiverExists) { _ = try handleWire(bRoutes, path: ["echo"]) { _, params in params } }
        #expect(try await callWire(a, path: ["echo"], params: wireTestData("4")) == wireTestData("4"))
    }

    @Test func roundTripReverseAndAbsentOptionalMemberVersusNull() async throws {
        let (a, b) = try wirePair()
        defer { try? a.close(code: 1000, reason: "Done") }
        let inputs = WireTestValues<Data?>()
        let aRoutes = try Dispatcher(a)
        defer { aRoutes.close() }
        _ = try handleWire(aRoutes, path: ["reverse"]) { _, params in params }
        let bRoutes = try Dispatcher(b)
        defer { bRoutes.close() }
        _ = try handleWire(bRoutes, path: ["call"]) { _, params in
            inputs.append(params)
            return try await callWire(b, path: ["reverse"], params: params)
        }
        let value = try await callWire(a, path: ["call"], params: wireTestData("7"))
        #expect(value == wireTestData("7"))
        let absent = try await callWire(a, path: ["call"], params: wireTestData("{}"))
        #expect(absent == wireTestData("{}"))
        let null = try await callWire(a, path: ["call"], params: wireTestData("{\"value\":null}"))
        #expect(null == wireTestData("{\"value\":null}"))
        #expect(inputs.all == [wireTestData("7"), wireTestData("{}"), wireTestData("{\"value\":null}")])
    }

    @Test func exactNamespaceAndOpaqueUnicodeRoutes() async throws {
        let (a, b) = try wirePair()
        defer { try? a.close(code: 1000, reason: "Done") }
        let bRoutes = try Dispatcher(b)
        defer { bRoutes.close() }
        for (path, label) in [([], "root"), (["a"], "prefix")] {
            _ = try bRoutes.registerPrefix(path: path, receiver: Receiver( message: { _, message in
                sendWireResponse(message, result: wireTestData("\"\(label)\""))
            }))
        }
        let detach = try handleWire(bRoutes, path: ["a", "b"]) { _, _ in wireTestData("\"exact\"") }
        #expect(try await callWire(a, path: ["a", "b"]) == wireTestData("\"exact\""))
        detach(); detach()
        #expect(try await callWire(a, path: ["a", "b"]) == wireTestData("\"prefix\""))
        #expect(try await callWire(a, path: ["other"]) == wireTestData("\"root\""))
        _ = try handleWire(bRoutes, path: ["é"]) { _, _ in wireTestData("1") }
        _ = try handleWire(bRoutes, path: ["e\u{301}"]) { _, _ in wireTestData("2") }
        #expect(try await callWire(a, path: ["é"]) == wireTestData("1"))
        #expect(try await callWire(a, path: ["e\u{301}"]) == wireTestData("2"))
        _ = try handleWire(bRoutes, path: [""]) { _, _ in wireTestData("3") }
        _ = try handleWire(bRoutes, path: ["a/b"]) { _, _ in wireTestData("4") }
        #expect(try await callWire(a, path: [""]) == wireTestData("3"))
        #expect(try await callWire(a, path: ["a/b"]) == wireTestData("4"))
    }

    @Test func fullDataQueueReservesExactlyOneCancelAndPreservesOrder() async throws {
        let (a, b) = try wirePair(options: .init(queueCapacity: 1, maxPendingRequests: 1))
        let entered = AsyncResult<Bool>(), requestSeen = AsyncResult<Bool>(), cancelled = AsyncResult<Bool>(), drained = AsyncResult<Bool>()
        let release = DispatchSemaphore(value: 0)
        defer { release.signal(); try? a.close(code: 1000, reason: "Done") }
        let order = WireTestValues<Int>(), cancelCount = WireTestValues<Int>()
        let bRoutes = try Dispatcher(b)
        defer { bRoutes.close() }
        _ = try bRoutes.register(path: ["hold"], receiver: Receiver(message: { _, message in
            if message.frame.kind == .request { Task { await requestSeen.resolve(.success(true)) } }
            if message.frame.kind == .cancel {
                cancelCount.append(1)
                sendWireResponse(message, error: ProfileError(code: "cancelled", message: "Request cancelled"))
                Task { await cancelled.resolve(.success(true)) }
            }
        }))
        _ = try bRoutes.register(path: ["event"], receiver: Receiver(message: { _, message in
            let value = Int(String(data: message.frame.data!, encoding: .utf8)!)!
            order.append(value)
            if value == 1 { Task { await entered.resolve(.success(true)) }; _ = release.wait(timeout: .now() + 2) }
            if value == 2 { Task { await drained.resolve(.success(true)) } }
        }))
        let address = ReturnAddress(wire: WireTestSink { _ in })
        try a.send(path: ["hold"], message: Message(frame: ProfileFrame(kind: .request, id: "c:1", params: wireTestData("null")), returnAddress: address))
        _ = try await wireAwait(requestSeen)
        try emitWire(a, path: ["event"], data: wireTestData("1"))
        _ = try await wireAwait(entered)
        try emitWire(a, path: ["event"], data: wireTestData("2"))
        for _ in 0..<100 {
            try a.send(path: ["hold"], message: Message(frame: ProfileFrame(kind: .cancel, id: "c:1"), returnAddress: address))
        }
        release.signal()
        _ = try await wireAwait(cancelled)
        _ = try await wireAwait(drained)
        #expect(order.all == [1, 2])
        #expect(cancelCount.all == [1])
    }

    @Test func overflowClosesTheStalledCarrierAndLeavesIndependentPairAlive() async throws {
        let (a, b) = try wirePair(options: .init(queueCapacity: 1))
        let (independentA, independentB) = try wirePair()
        let entered = AsyncResult<Bool>(), closed = AsyncResult<Int>()
        let release = DispatchSemaphore(value: 0)
        defer { release.signal(); try? a.close(code: 1000, reason: "Done"); try? independentA.close(code: 1000, reason: "Done") }
        let bRoutes = try Dispatcher(b)
        defer { bRoutes.close() }
        _ = try bRoutes.register(path: ["event"], receiver: Receiver(message: { _, _ in
            Task { await entered.resolve(.success(true)) }
            _ = release.wait(timeout: .now() + 2)
        }, closed: { code, _ in Task { await closed.resolve(.success(code)) } }))
        try emitWire(a, path: ["event"])
        _ = try await wireAwait(entered)
        try emitWire(a, path: ["event"])
        #expect(throws: WireError.backpressure) { try emitWire(a, path: ["event"]) }
        #expect(try await wireAwait(closed) == 4011)
        let independentBRoutes = try Dispatcher(independentB)
        defer { independentBRoutes.close() }
        _ = try handleWire(independentBRoutes, path: ["echo"]) { _, params in params }
        #expect(try await callWire(independentA, path: ["echo"], params: wireTestData("5")) == wireTestData("5"))
    }

    @Test func deadlineKeepsNoncooperativeHandlerBudget() async throws {
        let (a, b) = try wirePair(options: .init(maxConcurrentHandlers: 1, requestTimeoutMilliseconds: 30))
        let release = AsyncResult<Bool>(), started = AsyncResult<Bool>(), cancelled = AsyncResult<Bool>()
        defer { Task { await release.resolve(.success(true)) }; try? a.close(code: 1000, reason: "Done") }
        let bRoutes = try Dispatcher(b)
        defer { bRoutes.close() }
        _ = try handleWire(bRoutes, path: ["hold"]) { context, _ in
            await started.resolve(.success(true))
            await context.cancellation.wait()
            await cancelled.resolve(.success(true))
            _ = try await release.value()
            return wireTestData("1")
        }
        let first = Task { try await callWire(a, path: ["hold"]) }
        _ = try await wireAwait(started)
        do { _ = try await first.value; Issue.record("Deadline unexpectedly succeeded") }
        catch let error as PublicError { #expect(error.code == "cancelled") }
        _ = try await wireAwait(cancelled)
        do { _ = try await callWire(a, path: ["hold"]); Issue.record("Timed-out handler released its active slot") }
        catch let error as PublicError { #expect(error.code == "busy") }
        await release.resolve(.success(true))
    }

    @Test func pendingBudgetIsHeldUntilTheActualResponse() async throws {
        let (a, b) = try wirePair(options: .init(maxPendingRequests: 1))
        let entered = AsyncResult<Bool>(), release = AsyncResult<Bool>()
        defer { Task { await release.resolve(.success(true)) }; try? a.close(code: 1000, reason: "Done") }
        let bRoutes = try Dispatcher(b)
        defer { bRoutes.close() }
        _ = try handleWire(bRoutes, path: ["hold"]) { _, _ in
            await entered.resolve(.success(true)); _ = try await release.value(); return wireTestData("1")
        }
        _ = try handleWire(bRoutes, path: ["next"]) { _, _ in wireTestData("2") }
        let first = Task { try await callWire(a, path: ["hold"]) }
        _ = try await wireAwait(entered)
        do { _ = try await callWire(a, path: ["next"]); Issue.record("Pending limit was not enforced") }
        catch let error as PublicError { #expect(error.code == "busy") }
        await release.resolve(.success(true))
        #expect(try await first.value == wireTestData("1"))
        #expect(try await callWire(a, path: ["next"]) == wireTestData("2"))
    }

    @Test func detachedHandlerRetainsCancellationAndReturn() async throws {
        let (a, b) = try wirePair()
        defer { try? a.close(code: 1000, reason: "Done") }
        let entered = AsyncResult<Bool>(), cancelled = AsyncResult<Bool>()
        let bRoutes = try Dispatcher(b)
        defer { bRoutes.close() }
        let detach = try handleWire(bRoutes, path: ["hold"]) { context, _ in
            await entered.resolve(.success(true))
            await context.cancellation.wait()
            await cancelled.resolve(.success(true))
            throw CancellationError()
        }
        let call = Task { try await callWire(a, path: ["hold"]) }
        _ = try await wireAwait(entered)
        detach(); call.cancel()
        do { _ = try await call.value; Issue.record("Cancelled call unexpectedly succeeded") }
        catch is CancellationError {}
        _ = try await wireAwait(cancelled)
    }

    @Test func forwardingAndDetachPreserveBorrowedOrigins() async throws {
        let (a, bridgeA) = try wirePair(), (bridgeB, b) = try wirePair()
        defer { try? a.close(code: 1000, reason: "Done"); try? b.close(code: 1000, reason: "Done") }
        let detach = try forwardWire(bridgeA, bridgeB)
        let bRoutes = try Dispatcher(b)
        defer { bRoutes.close() }
        _ = try handleWire(bRoutes, path: ["forwarded"]) { _, params in params }
        #expect(try await callWire(a, path: ["forwarded"], params: wireTestData("9")) == wireTestData("9"))
        detach(); detach()
        let bridgeARoutes = try Dispatcher(bridgeA)
        defer { bridgeARoutes.close() }
        _ = try handleWire(bridgeARoutes, path: ["probe"]) { _, _ in wireTestData("7") }
        let bridgeBRoutes = try Dispatcher(bridgeB)
        defer { bridgeBRoutes.close() }
        _ = try handleWire(bridgeBRoutes, path: ["probe"]) { _, _ in wireTestData("8") }
        #expect(try await callWire(a, path: ["probe"]) == wireTestData("7"))
        #expect(try await callWire(b, path: ["probe"]) == wireTestData("8"))
    }

    @Test func oversizeResponseFallsBackAndStalledEventHasDeadline() async throws {
        let (a, b) = try wirePair(options: .init(writeTimeoutMilliseconds: 30, maxFrameBytes: 512))
        defer { try? a.close(code: 1000, reason: "Done") }
        let bRoutes = try Dispatcher(b)
        defer { bRoutes.close() }
        _ = try handleWire(bRoutes, path: ["large"]) { _, _ in wireTestData("\"" + String(repeating: "x", count: 2_048) + "\"") }
        do { _ = try await callWire(a, path: ["large"]); Issue.record("Oversized reply unexpectedly succeeded") }
        catch let error as PublicError { #expect(error.code == "internal") }
        let closed = AsyncResult<Int>(), release = DispatchSemaphore(value: 0)
        defer { release.signal() }
        _ = try bRoutes.register(path: ["blocked"], receiver: Receiver(message: { _, _ in
            _ = release.wait(timeout: .now() + 2)
        }, closed: { code, _ in Task { await closed.resolve(.success(code)) } }))
        try emitWire(a, path: ["blocked"])
        #expect(try await wireAwait(closed) == 4011)
    }

    @Test func physicalPeerWirePreservesRelativePathsContextOrderAndCancellation() async throws {
        let connections = Pipe.pair()
        let a = try Peer(connection: connections.0, role: "client")
        let b = try Peer(connection: connections.1, role: "server")
        let aRootRoutes = try Dispatcher(try await a.wire())
        let aWire = aRootRoutes.select(path: ["outer", "a/b"])
        let bRootRoutes = try Dispatcher(try await b.wire())
        let bWire = bRootRoutes.select(path: ["outer", "a/b"])
        let contexts = WireTestValues<RequestContext>(), eventData = WireTestValues<Data?>()
        let eventDone = AsyncResult<Bool>()
        let trace = "00-11111111111111111111111111111111-2222222222222222-01"
        let aWireRoutes = try Dispatcher(aWire)
        defer { aWireRoutes.close() }
        _ = try handleWire(aWireRoutes, path: ["reverse"]) { _, params in params }
        let bWireRoutes = try Dispatcher(bWire)
        defer { bWireRoutes.close() }
        _ = try handleWire(bWireRoutes, path: ["echo"]) { context, params in
            contexts.append(context)
            return try await callWire(bWire, path: ["reverse"], params: params, context: context)
        }
        _ = try bWireRoutes.register(path: ["event"], receiver: Receiver(message: { _, message in
            eventData.append(message.frame.data)
            if eventData.all.count == 3 { Task { await eventDone.resolve(.success(true)) } }
        }))
        await a.start(); await b.start()
        let payload = wireTestData("{\"number\":1e3,\"large\":9007199254740993}")
        let result = try await callWire(aWire, path: ["echo"], params: payload,
            context: RequestContext(traceparent: trace, tracestate: "vendor=value"), meta: ["tenant": "one"])
        #expect(result == payload)
        #expect(contexts.all.first?.traceparent == trace)
        #expect(contexts.all.first?.tracestate == "vendor=value")
        #expect(contexts.all.first?.meta == ["tenant": "one"])
        for data in [wireTestData("{}"), wireTestData("{\"value\":null}"), wireTestData("3")] { try emitWire(aWire, path: ["event"], data: data) }
        _ = try await wireAwait(eventDone)
        #expect(eventData.all == [wireTestData("{}"), wireTestData("{\"value\":null}"), wireTestData("3")])
        let entered = AsyncResult<Bool>(), cancelled = AsyncResult<Bool>()
        _ = try handleWire(bWireRoutes, path: ["hold"]) { context, _ in
            await entered.resolve(.success(true)); await context.cancellation.wait()
            await cancelled.resolve(.success(true)); throw CancellationError()
        }
        let pending = Task { try await callWire(aWire, path: ["hold"]) }
        _ = try await wireAwait(entered)
        pending.cancel()
        do { _ = try await pending.value; Issue.record("Cancelled peer Wire call succeeded") }
        catch is CancellationError {}
        _ = try await wireAwait(cancelled)
        await a.close(); await b.close()
    }
}
