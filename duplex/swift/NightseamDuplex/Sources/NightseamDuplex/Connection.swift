import Foundation

public enum FrameKind: Sendable { case text, binary }

public struct Frame: Sendable, Equatable {
    public var kind: FrameKind
    public var data: Data
    public init(kind: FrameKind, data: Data) { self.kind = kind; self.data = data }
}

public struct CloseError: Error, Sendable, Equatable, CustomStringConvertible {
    public var code: Int
    public var reason: String
    public init(code: Int, reason: String = "") { self.code = code; self.reason = reason }
    public var description: String { "duplex connection closed (\(code)): \(reason)" }
}

public enum DuplexError: Error, Sendable, Equatable {
    case closed
    case tooLarge
    case invalidPath
    case noRoute
    case receiverExists
    case protocolError(String)
    case transport(String)
}

/// One ordered, message-framed transport. One sender and one receiver may run
/// concurrently. Backpressure suspends the sender instead of growing a queue.
public protocol Connection: Sendable {
    var subprotocol: String { get }
    func send(_ frame: Frame) async throws
    func receive() async throws -> Frame
    func close(code: Int, reason: String) async throws
    func abort() async
}

public extension Connection { var subprotocol: String { "" } }

public enum Pipe {
    public static func pair(limit: Int = 0, capacity: Int = 8) -> (any Connection, any Connection) {
        let state = PipeState(limit: limit, capacity: max(1, capacity))
        return (PipeEnd(state: state, side: 0), PipeEnd(state: state, side: 1))
    }
}

private struct PipeEnd: Connection {
    let state: PipeState
    let side: Int
    func send(_ frame: Frame) async throws {
        let id = UUID()
        try await withTaskCancellationHandler {
            try await state.send(side, frame: frame, id: id)
        } onCancel: { Task { await state.cancel(id) } }
    }
    func receive() async throws -> Frame {
        let id = UUID()
        return try await withTaskCancellationHandler {
            try await state.receive(side, id: id)
        } onCancel: { Task { await state.cancel(id) } }
    }
    func close(code: Int, reason: String) async throws {
        await state.end(side, error: CloseError(code: code, reason: reason))
    }
    func abort() async { await state.end(side, error: CloseError(code: 1006)) }
}

private actor PipeState {
    struct Sender { let id: UUID; let frame: Frame; let continuation: CheckedContinuation<Void, any Error> }
    struct Reader { let id: UUID; let continuation: CheckedContinuation<Frame, any Error> }
    struct End {
        var frames: [Frame] = []
        var senders: [Sender] = []
        var reader: Reader?
        var closed: CloseError?
    }
    var ends = [End(), End()]
    let limit: Int
    let capacity: Int
    init(limit: Int, capacity: Int) { self.limit = limit; self.capacity = capacity }

    func send(_ side: Int, frame: Frame, id: UUID) async throws {
        try Task.checkCancellation()
        if ends[side].closed != nil { throw DuplexError.closed }
        let other = 1 - side
        if let closed = ends[other].closed { throw closed }
        if let reader = ends[other].reader {
            ends[other].reader = nil
            if limit > 0 && frame.data.count > limit {
                reader.continuation.resume(throwing: DuplexError.tooLarge)
                end(other, error: CloseError(code: 1006))
            } else { reader.continuation.resume(returning: frame) }
        } else if ends[other].frames.count < capacity {
            ends[other].frames.append(frame)
        } else {
            try await withCheckedThrowingContinuation { continuation in
                ends[other].senders.append(Sender(id: id, frame: frame, continuation: continuation))
            }
        }
    }

    func receive(_ side: Int, id: UUID) async throws -> Frame {
        try Task.checkCancellation()
        if ends[side].closed != nil { throw DuplexError.closed }
        if !ends[side].frames.isEmpty {
            let frame = ends[side].frames.removeFirst()
            if !ends[side].senders.isEmpty {
                let sender = ends[side].senders.removeFirst()
                ends[side].frames.append(sender.frame)
                sender.continuation.resume()
            }
            if limit > 0 && frame.data.count > limit {
                end(side, error: CloseError(code: 1006))
                throw DuplexError.tooLarge
            }
            return frame
        }
        if let closed = ends[1-side].closed { throw closed }
        guard ends[side].reader == nil else { throw DuplexError.protocolError("concurrent receive") }
        return try await withCheckedThrowingContinuation { continuation in
            ends[side].reader = Reader(id: id, continuation: continuation)
        }
    }

    func cancel(_ id: UUID) {
        for side in 0...1 {
            if ends[side].reader?.id == id {
                let reader = ends[side].reader!; ends[side].reader = nil
                reader.continuation.resume(throwing: CancellationError())
            }
            if let index = ends[side].senders.firstIndex(where: { $0.id == id }) {
                ends[side].senders.remove(at: index).continuation.resume(throwing: CancellationError())
            }
        }
    }

    func end(_ side: Int, error: CloseError) {
        guard ends[side].closed == nil else { return }
        ends[side].closed = error
        ends[side].frames.removeAll()
        for index in 0...1 {
            let failure: any Error = index == side ? DuplexError.closed : error
            if let reader = ends[index].reader {
                ends[index].reader = nil
                reader.continuation.resume(throwing: failure)
            }
            // Senders stored at an end are sending *to* that end.
            let sendFailure: any Error = index == side ? error : DuplexError.closed
            let senders = ends[index].senders; ends[index].senders.removeAll()
            for sender in senders { sender.continuation.resume(throwing: sendFailure) }
        }
    }
}
