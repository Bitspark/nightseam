import Bitwire
import Foundation
import NightseamDuplex
import NightseamRuntime

// A consumer append/follow composition of Wire. Sequence numbers are retained
// by this testee's store and never added to the profile's envelope.
private struct RecordedEntry: Sendable {
    let path: [String]
    let message: Message
    let sequence: Int
}

private struct RecordedFailure: Error, Sendable, CustomStringConvertible {
    let description: String
    init(_ description: String) { self.description = description }
}

private final class RecordedMailbox<Value: Sendable>: @unchecked Sendable {
    private let condition = NSCondition()
    private var values: [Value] = []
    var count: Int { condition.withLock { values.count } }
    func put(_ value: Value) {
        condition.lock(); values.append(value); condition.signal(); condition.unlock()
    }
    func take(until deadline: Date) throws -> Value {
        condition.lock(); defer { condition.unlock() }
        while values.isEmpty {
            guard condition.wait(until: deadline) else { throw RecordedFailure("Recorded wire witness timed out") }
        }
        return values.removeFirst()
    }
}

private final class RecordedFollower: @unchecked Sendable {
    let target: any Endpoint
    let bound: Int
    let sent = RecordedMailbox<Int>()
    let paused = RecordedMailbox<Bool>()
    let done = RecordedMailbox<Bool>()
    private let condition = NSCondition()
    private var live: [RecordedEntry] = []
    private var stopped = false
    private var resumed = false
    init(target: any Endpoint, bound: Int) { self.target = target; self.bound = bound }
    var queued: Int { condition.withLock { live.count } }
    func stop() { condition.lock(); stopped = true; condition.broadcast(); condition.unlock() }
    func resume() { condition.lock(); resumed = true; condition.broadcast(); condition.unlock() }
    func enqueue(_ entry: RecordedEntry) -> Bool {
        condition.lock(); defer { condition.unlock() }
        guard !stopped else { return false }
        guard live.count < bound else { stopped = true; condition.broadcast(); return false }
        live.append(entry); condition.signal(); return true
    }
    func run(history: [RecordedEntry], pause: Bool) {
        defer {
            try? target.close(code: 1008, reason: "Recorded handoff ended")
            done.put(true)
        }
        func send(_ entry: RecordedEntry) -> Bool {
            guard !condition.withLock({ stopped }) else { return false }
            do { try target.send(path: entry.path, message: entry.message) }
            catch { return false }
            sent.put(entry.sequence); return true
        }
        for (index, entry) in history.enumerated() {
            guard send(entry) else { return }
            if index == 0 && pause {
                paused.put(true)
                condition.lock()
                while !resumed && !stopped { condition.wait() }
                let stop = stopped
                condition.unlock()
                if stop { return }
            }
        }
        while true {
            condition.lock()
            while live.isEmpty && !stopped { condition.wait() }
            if stopped { condition.unlock(); return }
            let entry = live.removeFirst()
            condition.unlock()
            guard send(entry) else { return }
        }
    }
    func awaitSent(_ sequence: Int, until deadline: Date) throws {
        while try sent.take(until: deadline) != sequence {}
    }
}

private final class RecordedStore: Wire, @unchecked Sendable {
    private let lock = NSLock()
    private var entries: [RecordedEntry] = []
    private var followers: [UUID: RecordedFollower] = [:]
    var head: Int { lock.withLock { entries.count } }
    func send(path: [String], message: Message) throws {
        lock.withLock {
            let entry = RecordedEntry(path: path, message: message, sequence: entries.count + 1)
            entries.append(entry)
            let overflowed = followers.compactMap { id, follower in follower.enqueue(entry) ? nil : id }
            for id in overflowed { followers.removeValue(forKey: id) }
        }
    }
    func close(code: Int, reason: String) throws {
        let detached = lock.withLock { let copy = Array(followers.values); followers.removeAll(); return copy }
        for follower in detached { follower.stop() }
    }
    func attach(after: Int, target: any Endpoint, bound: Int, pause: Bool) -> (Int, RecordedFollower) {
        let follower = RecordedFollower(target: target, bound: bound)
        // Taking the head and installing the live follower share append's lock.
        let (head, history) = lock.withLock {
            let head = entries.count
            let history = Array(entries[after..<head])
            followers[UUID()] = follower
            return (head, history)
        }
        DispatchQueue.global().async { follower.run(history: history, pause: pause) }
        return (head, follower)
    }
}

// A deterministic asynchronous fixture origin. Selection and mount remain the
// production implementations; forwarding sends the same opaque Message through
// a second queued origin, exercising actual callbacks and relative paths.
private final class RecordedRoot: Endpoint, @unchecked Sendable {
    private let lock = NSLock()
    private let worker = DispatchQueue(label: "nightseam.recorded.root")
    private var receivers: [Data: Receiver] = [:]
    private var queue: [RecordedEntry] = []
    private var running = false
    private var ended = false
    func send(path: [String], message: Message) throws {
        let start = try lock.withLock {
            guard !ended else { throw RecordedFailure("Fixture root closed") }
            guard queue.count < 16 else { throw RecordedFailure("Fixture output queue full") }
            queue.append(RecordedEntry(path: path, message: message, sequence: 0))
            let start = !running; running = true; return start
        }
        if start { worker.async { self.run() } }
    }
    private func run() {
        while true {
            let next: (RecordedEntry, Receiver?)? = lock.withLock {
                guard !ended, !queue.isEmpty else { running = false; return nil }
                let entry = queue.removeFirst()
                return (entry, receivers[Data()])
            }
            guard let (entry, receiver) = next else { return }
            receiver?.message(entry.path, entry.message)
        }
    }
    func receive(receiver: Receiver) throws -> Detach {
        let key = Data()
        try lock.withLock {
            guard !ended else { throw RecordedFailure("Fixture root closed") }
            guard receivers[key] == nil else { throw RecordedFailure("Fixture receiver exists") }
            receivers[key] = receiver
        }
        return { [self] in _ = lock.withLock { receivers.removeValue(forKey: key) } }
    }
    func close(code: Int, reason: String) throws {
        lock.withLock { ended = true; queue.removeAll(); receivers.removeAll() }
    }
}

private func recordedMessage(_ value: Int) -> Message {
    Message(frame: ProfileFrame(kind: .event, data: Data(String(value).utf8)))
}

private final class RecordedPresentation: Sendable {
    let root: RecordedRoot
    let end: RecordedRoot
    let wire: any Endpoint
    let rootRoutes: Dispatcher
    let routes: [Dispatcher]
    let values: RecordedMailbox<Int>
    let closed: RecordedMailbox<Int>
    let errors: RecordedMailbox<String>
    let reentered: RecordedMailbox<Bool>
    init(store: RecordedStore) throws {
        let root = RecordedRoot(), end = RecordedRoot()
        let values = RecordedMailbox<Int>(), closed = RecordedMailbox<Int>()
        let errors = RecordedMailbox<String>(), reentered = RecordedMailbox<Bool>()
        let rootRoutes = try Dispatcher(root), endRoutes = try Dispatcher(end)
        let destinationRoutes = try Dispatcher(mount(["out": endRoutes.select(path: ["destination"])]))
        let sourceRoutes = try Dispatcher(mount(["outer": mount(["in": rootRoutes.select(path: ["source"])])]))
        let destination = destinationRoutes.select(path: ["out"])
        let wire = sourceRoutes.select(path: ["outer", "in"])
        self.rootRoutes = rootRoutes; self.routes = [endRoutes, destinationRoutes, sourceRoutes]
        _ = try destination.receive(receiver: Receiver(message: { path, message in
            guard Path.key(path) == Path.key(["tick"]) else { errors.put("Destination received wrong path"); return }
            // A real consumer callback takes append's lock. If delivery occurred
            // under that lock, the fence below would fail its bounded wait.
            _ = store.head
            reentered.put(true)
            guard let data = message.frame.data, let text = String(data: data, encoding: .utf8), let value = Int(text)
            else { errors.put("Destination received invalid event data"); return }
            values.put(value)
        }))
        _ = try wire.receive(receiver: Receiver(message: { path, message in
            do { try destination.send(path: path, message: message) }
            catch { errors.put(String(describing: error)) }
        }, closed: { code, _ in _ = store.head; closed.put(code) }))
        self.root = root; self.end = end; self.wire = wire
        self.values = values; self.closed = closed; self.errors = errors; self.reentered = reentered
    }
    func close() {
        try? wire.close(code: 1000, reason: "Done")
        rootRoutes.close(); for route in routes { route.close() }
        try? root.close(code: 1000, reason: "Done")
        try? end.close(code: 1000, reason: "Done")
    }
    func collect(until deadline: Date) throws -> [Int] {
        // The fence takes the same mounted forwarding route as all data. Thus
        // an extra replay cannot hide after collecting an expected count.
        try wire.send(path: ["tick"], message: recordedMessage(0))
        var result: [Int] = []
        while true {
            if errors.count > 0 { throw RecordedFailure(try errors.take(until: deadline)) }
            let value = try values.take(until: deadline)
            if value == 0 { return result }
            result.append(value)
        }
    }
}

private func recordedArray(_ values: [Int]) -> JSONValue { .array(values.map { .number(String($0)) }) }

private func recordedHeadCase(appendBeforeHead: Bool, deadline: Date) throws -> JSONValue {
    let store = RecordedStore()
    defer { try? store.close(code: 1000, reason: "Done") }
    let presentation = try RecordedPresentation(store: store)
    defer { presentation.close() }
    let source = at(store, path: [])
    let append: @Sendable (Int) throws -> Void = { value in
        try source.send(path: ["tick"], message: recordedMessage(value))
    }
    for value in 1...3 { try append(value) }
    if appendBeforeHead { try append(4) }
    let (head, follower) = store.attach(after: 0, target: presentation.wire, bound: 2, pause: true)
    defer { follower.stop() }
    _ = try follower.paused.take(until: deadline)
    let produced = RecordedMailbox<Result<Bool, RecordedFailure>>()
    DispatchQueue.global().async {
        do {
            for value in (appendBeforeHead ? 5 : 4)...5 { try append(value) }
            produced.put(.success(true))
        } catch { produced.put(.failure(RecordedFailure(String(describing: error)))) }
    }
    let progress = try produced.take(until: deadline).get()
    follower.resume()
    try follower.awaitSent(5, until: deadline)
    try append(6)
    try follower.awaitSent(6, until: deadline)
    let first = try presentation.collect(until: deadline)
    let second = try RecordedPresentation(store: store)
    defer { second.close() }
    let (_, late) = store.attach(after: 3, target: second.wire, bound: 2, pause: false)
    defer { late.stop() }
    try late.awaitSent(6, until: deadline)
    let after = try second.collect(until: deadline)
    return .object([
        "cut": .string(appendBeforeHead ? "append_before_head" : "head_before_append"),
        "head": .number(String(head)), "first": recordedArray(first), "after_three": recordedArray(after),
        "producer_progress": .bool(progress), "callbacks_outside_append": .bool(presentation.reentered.count > 0 && second.reentered.count > 0)
    ])
}

private func recordedStallCase(deadline: Date) throws -> JSONValue {
    let store = RecordedStore()
    defer { try? store.close(code: 1000, reason: "Done") }
    for value in 1...3 { try store.send(path: ["tick"], message: recordedMessage(value)) }
    let stalled = try RecordedPresentation(store: store), healthy = try RecordedPresentation(store: store)
    defer { stalled.close(); healthy.close() }
    let (_, slow) = store.attach(after: 0, target: stalled.wire, bound: 2, pause: true)
    defer { slow.stop() }
    _ = try slow.paused.take(until: deadline)
    let (_, fast) = store.attach(after: 3, target: healthy.wire, bound: 2, pause: false)
    defer { fast.stop() }
    for value in 4...5 {
        try store.send(path: ["tick"], message: recordedMessage(value))
        try fast.awaitSent(value, until: deadline)
    }
    let queued = slow.queued
    try store.send(path: ["tick"], message: recordedMessage(6))
    try fast.awaitSent(6, until: deadline)
    _ = try slow.done.take(until: deadline)
    let code = try stalled.closed.take(until: deadline)
    var rejected = false
    do { try stalled.wire.send(path: ["tick"], message: recordedMessage(99)) }
    catch { rejected = true }
    guard rejected else { throw RecordedFailure("Stalled carrier admitted after close") }
    let underneath = RecordedMailbox<Int>()
    _ = try stalled.rootRoutes.register(path: ["probe"], receiver: Receiver(message: { _, message in
        if let data = message.frame.data, let text = String(data: data, encoding: .utf8), let value = Int(text) { underneath.put(value) }
    }))
    try stalled.root.send(path: ["probe"], message: recordedMessage(99))
    let probe = try underneath.take(until: deadline)
    try store.send(path: ["tick"], message: recordedMessage(7))
    try fast.awaitSent(7, until: deadline)
    let values = try healthy.collect(until: deadline)
    return .object([
        "bound": .number(String(slow.bound)), "queued_at_bound": .number(String(queued)),
        "closed": .number(String(1 + stalled.closed.count)), "close_code": .number(String(code)),
        "healthy": recordedArray(values), "underneath": recordedArray([probe]),
        "head": .number(String(store.head)), "producer_progress": .bool(values.last == store.head)
    ])
}

func recordedWireWitness(withinMilliseconds: Int = 5_000) throws -> JSONValue {
    let deadline = Date().addingTimeInterval(Double(withinMilliseconds) / 1000)
    return .object([
        "cases": .array(try [false, true].map { try recordedHeadCase(appendBeforeHead: $0, deadline: deadline) }),
        "stalled": try recordedStallCase(deadline: deadline)
    ])
}
