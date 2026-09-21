import Foundation
import XCTest
@testable import NightseamDuplex

final class DuplexTests: XCTestCase, @unchecked Sendable {
    func testPipePreservesOrderBytesAndClose() async throws {
        let (left, right) = Pipe.pair()
        let frames = [Frame(kind: .text, data: Data("zero".utf8)), Frame(kind: .binary, data: Data([0, 255, 1]))]
        for frame in frames { try await left.send(frame) }
        try await left.close(code: 1008, reason: "policy")
        for frame in frames { let received = try await right.receive(); XCTAssertEqual(received, frame) }
        do { _ = try await right.receive(); XCTFail("expected close") }
        catch { XCTAssertEqual(error as? CloseError, CloseError(code: 1008, reason: "policy")) }
        do { try await left.send(frames[0]); XCTFail("local close sends") }
        catch { XCTAssertEqual(error as? DuplexError, .closed) }
    }

    func testBoundedPipeCancellationDoesNotPublishOrEndCarrier() async throws {
        let (left, right) = Pipe.pair(capacity: 1)
        let first = Frame(kind: .text, data: Data("first".utf8))
        try await left.send(first)
        let blocked = Task { try await left.send(Frame(kind: .text, data: Data("cancelled".utf8))) }
        try await Task.sleep(for: .milliseconds(30))
        blocked.cancel()
        do { try await blocked.value; XCTFail("unbounded send") } catch is CancellationError {} catch { XCTFail("\(error)") }
        let received = try await right.receive(); XCTAssertEqual(received, first)
        let next = Frame(kind: .binary, data: Data([4]))
        try await left.send(next)
        let second = try await right.receive(); XCTAssertEqual(second, next)
        await right.abort()
        do { _ = try await left.receive(); XCTFail("expected abort") }
        catch { XCTAssertEqual(error as? CloseError, CloseError(code: 1006)) }
    }

    func testPipeLimitEndsReceiver() async throws {
        let (left, right) = Pipe.pair(limit: 2)
        try await left.send(Frame(kind: .text, data: Data("big".utf8)))
        do { _ = try await right.receive(); XCTFail("oversize delivered") }
        catch { XCTAssertEqual(error as? DuplexError, .tooLarge) }
        do { _ = try await right.receive(); XCTFail("oversize did not end receiver") }
        catch { XCTAssertEqual(error as? DuplexError, .closed) }
    }

    func testCanonicalPathsRetainScalarIdentity() throws {
        let path = ["", "a/b", "a.b", "é", "e\u{301}", "🍋"]
        XCTAssertEqual(Path.encode(path), "0:3:a/b3:a.b2:é3:e\u{301}4:🍋")
        let decoded = try Path.decode(Path.encode(path))
        XCTAssertEqual(decoded.map { Data($0.utf8) }, path.map { Data($0.utf8) })
        XCTAssertNotEqual(Path.key(["é"]), Path.key(["e\u{301}"]))
        XCTAssertFalse(Path.hasPrefix(["é"], ["e\u{301}"]))
        for invalid in ["00:", "01:a", "1:é", ":", "1", "-1:x", "9999999999999999999999999999:x", "3:ab"] {
            XCTAssertThrowsError(try Path.decode(invalid), invalid)
        }
    }

    func testViewsPreserveOriginAndReturnIdentity() throws {
        let root = RecordingWire()
        let view = at(at(root, path: ["outer"]), path: [""])
        let address = ReturnAddress(wire: root)
        let message = Message(frame: ProfileFrame(kind: .event, data: Data("null".utf8)), returnAddress: address)
        try view.send(path: ["op"], message: message)
        XCTAssertEqual(root.lastPath, ["outer", "", "op"])
        XCTAssertTrue(root.lastMessage?.returnAddress === address)
        let capture = RecordingWire()
        let detach = try view.receive(path: ["op"], receiver: Receiver(message: { path, message in
            try? capture.send(path: path, message: message)
        }))
        root.deliver(path: ["outer", "", "op"], message: message)
        XCTAssertEqual(capture.lastPath, ["op"])
        detach(); detach()
        XCTAssertEqual(root.registrationCount, 0)
        try view.close(code: 1000, reason: "done")
        XCTAssertTrue(root.closed)
    }

    func testMountBorrowsChildrenAndDistinguishesUnicodeKeys() throws {
        let composed = RecordingWire(), decomposed = RecordingWire()
        let mounted = mount([("é", composed as any Wire), ("e\u{301}", decomposed as any Wire)])
        let message = Message(frame: ProfileFrame(kind: .event))
        try mounted.send(path: ["é", "a"], message: message)
        try mounted.send(path: ["e\u{301}", "b"], message: message)
        XCTAssertEqual(composed.lastPath, ["a"]); XCTAssertEqual(decomposed.lastPath, ["b"])
        XCTAssertThrowsError(try mounted.send(path: [], message: message))
        let closures = Counter()
        _ = try mounted.receive(path: [], receiver: Receiver(namespace: true, message: { _, _ in }, closed: { _, _ in closures.increment() }))
        XCTAssertEqual(composed.registrationCount, 1)
        try mounted.close(code: 1000, reason: "view done")
        XCTAssertEqual(closures.value, 1)
        XCTAssertEqual(composed.registrationCount, 0); XCTAssertEqual(decomposed.registrationCount, 0)
        XCTAssertFalse(composed.closed); XCTAssertFalse(decomposed.closed)
        try composed.send(path: [], message: message)
    }

    func testWebSocketBothDirectionsSizesAndSubprotocol() async throws {
        let listener = try WebSocket.listen(subprotocols: ["nightseam.duplex/1"])
        defer { listener.close() }
        async let accepted = listener.accept()
        let client = try await WebSocket.dial(url: listener.url, subprotocols: ["other", "nightseam.duplex/1"])
        let server = try await accepted
        XCTAssertEqual(client.subprotocol, "nightseam.duplex/1")
        XCTAssertEqual(server.subprotocol, "nightseam.duplex/1")
        for size in [0, 5, 125, 126, 65535, 65536] {
            let frame = Frame(kind: .binary, data: Data(repeating: 241, count: size))
            try await client.send(frame)
            let inbound = try await server.receive(); XCTAssertEqual(inbound, frame)
            try await server.send(frame)
            let outbound = try await client.receive(); XCTAssertEqual(outbound, frame)
        }
        async let closing: Void = server.close(code: 4011, reason: "profile ended")
        do { _ = try await client.receive(); XCTFail("close lost") }
        catch { XCTAssertEqual(error as? CloseError, CloseError(code: 4011, reason: "profile ended")) }
        try await closing
        do { _ = try await server.receive(); XCTFail("local close lost") }
        catch { XCTAssertEqual(error as? DuplexError, .closed) }
    }

    func testWebSocketReceiveLimitAndAbort() async throws {
        let listener = try WebSocket.listen(limit: 4)
        defer { listener.close() }
        async let accepted = listener.accept()
        let client = try await WebSocket.dial(url: listener.url)
        let server = try await accepted
        try await client.send(Frame(kind: .text, data: Data("12345".utf8)))
        do { _ = try await server.receive(); XCTFail("oversize delivered") }
        catch { XCTAssertEqual((error as? CloseError)?.code, 1009) }
        do { _ = try await client.receive(); XCTFail("oversize close absent") }
        catch { XCTAssertEqual((error as? CloseError)?.code, 1009) }
    }

    func testWebSocketAbortReleasesPendingReceive() async throws {
        let listener = try WebSocket.listen()
        defer { listener.close() }
        async let accepted = listener.accept()
        let client = try await WebSocket.dial(url: listener.url)
        let server = try await accepted
        let blocked = Task { try await server.receive() }
        await client.abort()
        do { _ = try await blocked.value; XCTFail("abort did not release read") }
        catch { XCTAssertEqual((error as? CloseError)?.code, 1006) }
        await server.abort()
    }
}

private final class Counter: @unchecked Sendable {
    let lock = NSLock(); private var count = 0
    func increment() { lock.withLock { count += 1 } }
    var value: Int { lock.withLock { count } }
}

private final class RecordingWire: Wire, @unchecked Sendable {
    let lock = NSLock()
    private var path: [String] = []
    private var message: Message?
    private var receivers: [UUID: Receiver] = [:]
    private var ended = false
    var lastPath: [String] { lock.withLock { path } }
    var lastMessage: Message? { lock.withLock { message } }
    var registrationCount: Int { lock.withLock { receivers.count } }
    var closed: Bool { lock.withLock { ended } }
    func send(path: [String], message: Message) throws { lock.withLock { self.path = path; self.message = message } }
    func receive(path: [String], receiver: Receiver) throws -> Detach {
        let id = UUID(); lock.withLock { receivers[id] = receiver }
        return { _ = self.lock.withLock { self.receivers.removeValue(forKey: id) } }
    }
    func close(code: Int, reason: String) throws { lock.withLock { ended = true } }
    func deliver(path: [String], message: Message) {
        let receivers = lock.withLock { Array(self.receivers.values) }
        for receiver in receivers { receiver.message(path, message) }
    }
}
