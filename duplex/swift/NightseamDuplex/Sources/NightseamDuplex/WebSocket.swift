import Foundation
import Dispatch
#if canImport(Glibc)
import Glibc
#else
import Darwin
#endif

/// A native RFC 6455 transport over TCP. TLS belongs to the host's transport
/// termination; this adapter accepts ws URLs and never silently downgrades wss.
public enum WebSocket {
    public static func listen(host: String = "127.0.0.1", port: Int = 0, limit: Int = 0,
                              subprotocols: [String] = []) throws -> WebSocketListener {
        try WebSocketListener(host: host, port: port, limit: limit, subprotocols: subprotocols)
    }
    public static func dial(url: String, limit: Int = 0, subprotocols: [String] = []) async throws -> any Connection {
        guard let address = URLComponents(string: url), address.scheme == "ws", let host = address.host else {
            throw DuplexError.transport("expected a ws URL")
        }
        try validateProtocols(subprotocols)
        let port = address.port ?? 80
        let path = (address.percentEncodedPath.isEmpty ? "/" : address.percentEncodedPath)
            + (address.percentEncodedQuery.map { "?" + $0 } ?? "")
        return try await withCheckedThrowingContinuation { continuation in
            DispatchQueue.global().async {
                do {
                    let socket = try Socket.connect(host: host, port: port)
                    do {
                        let key = Data((0..<16).map { _ in UInt8.random(in: 0...255) }).base64EncodedString()
                        var request = "GET \(path) HTTP/1.1\r\nHost: \(host):\(port)\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: \(key)\r\n"
                        if !subprotocols.isEmpty { request += "Sec-WebSocket-Protocol: \(subprotocols.joined(separator: ", "))\r\n" }
                        try socket.write(Data((request + "\r\n").utf8))
                        let response = try readHTTP(socket)
                        guard response.first.split(separator: " ").dropFirst().first == "101",
                              response.headers["upgrade"]?.lowercased() == "websocket",
                              headerContains(response.headers["connection"], "upgrade"),
                              response.headers["sec-websocket-accept"] == acceptKey(key) else {
                            throw DuplexError.protocolError("WebSocket upgrade refused")
                        }
                        let selected = response.headers["sec-websocket-protocol"] ?? ""
                        guard selected.isEmpty || subprotocols.contains(selected) else {
                            throw DuplexError.protocolError("unoffered WebSocket subprotocol")
                        }
                        continuation.resume(returning: WebSocketConnection(socket: socket, client: true, limit: limit, subprotocol: selected))
                    } catch { socket.stop(); throw error }
                } catch { continuation.resume(throwing: error) }
            }
        }
    }
}

public final class WebSocketListener: @unchecked Sendable {
    private let socket: Socket
    private let limit: Int
    private let protocols: [String]
    public let port: Int
    public let url: String
    fileprivate init(host: String, port: Int, limit: Int, subprotocols: [String]) throws {
        try validateProtocols(subprotocols)
        self.socket = try Socket.listen(host: host, port: port)
        self.port = try socket.port()
        self.url = "ws://\(host):\(self.port)/"
        self.limit = limit; self.protocols = subprotocols
    }
    public func accept() async throws -> any Connection {
        try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                DispatchQueue.global().async { [self] in
                    do {
                        let accepted = try socket.accept()
                        do {
                            let request = try readHTTP(accepted)
                            let first = request.first.split(separator: " ")
                            guard first.count == 3, first[0] == "GET", first[2] == "HTTP/1.1",
                                  request.headers["upgrade"]?.lowercased() == "websocket",
                                  headerContains(request.headers["connection"], "upgrade"),
                                  request.headers["sec-websocket-version"] == "13",
                                  let key = request.headers["sec-websocket-key"],
                                  Data(base64Encoded: key)?.count == 16 else {
                                throw DuplexError.protocolError("invalid WebSocket upgrade")
                            }
                            let offered = (request.headers["sec-websocket-protocol"] ?? "").split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }
                            let selected = protocols.first { offered.contains($0) } ?? ""
                            var response = "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: \(acceptKey(key))\r\n"
                            if !selected.isEmpty { response += "Sec-WebSocket-Protocol: \(selected)\r\n" }
                            try accepted.write(Data((response + "\r\n").utf8))
                            continuation.resume(returning: WebSocketConnection(socket: accepted, client: false, limit: limit, subprotocol: selected))
                        } catch { accepted.stop(); throw error }
                    } catch { continuation.resume(throwing: error) }
                }
            }
        } onCancel: { self.close() }
    }
    public func close() { socket.stop() }
    deinit { socket.stop() }
}

private final class WebSocketConnection: Connection, @unchecked Sendable {
    let socket: Socket
    let client: Bool
    let limit: Int
    let subprotocol: String
    let reads = DispatchQueue(label: "nightseam.websocket.read")
    let writes = DispatchQueue(label: "nightseam.websocket.write")
    let stateLock = NSLock()
    var localClosed = false
    var remoteClosed: CloseError?
    init(socket: Socket, client: Bool, limit: Int, subprotocol: String) {
        self.socket = socket; self.client = client; self.limit = limit; self.subprotocol = subprotocol
    }
    private func check() throws {
        try stateLock.withLock {
            if localClosed { throw DuplexError.closed }
            if let remoteClosed { throw remoteClosed }
        }
    }
    func send(_ frame: Frame) async throws {
        try Task.checkCancellation()
        try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, any Error>) in
                writes.async { [self] in
                    do { try check(); try writeFrame(opcode: frame.kind == .text ? 1 : 2, data: frame.data); continuation.resume() }
                    catch { continuation.resume(throwing: translate(error)) }
                }
            }
        } onCancel: { self.stopLocally() }
    }
    func receive() async throws -> Frame {
        try Task.checkCancellation()
        return try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                reads.async { [self] in
                    do { try check(); continuation.resume(returning: try readMessage()) }
                    catch { continuation.resume(throwing: translate(error)) }
                }
            }
        } onCancel: { self.stopLocally() }
    }
    func close(code: Int, reason: String) async throws {
        guard validCloseCode(code), reason.utf8.count <= 123 else {
            throw DuplexError.protocolError("invalid close code or reason")
        }
        let first = stateLock.withLock { if localClosed { return false }; localClosed = true; return true }
        guard first else { return }
        let deadline = CloseDeadline()
        DispatchQueue.global().asyncAfter(deadline: .now() + 3) { [socket] in
            if deadline.expire() { socket.stop() }
        }
        try await withTaskCancellationHandler {
            // The write queue serializes the close with data. A private reader
            // then completes the handshake even if no consumer is receiving.
            try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, any Error>) in
                writes.async { [self] in
                    do {
                        try writeFrame(opcode: 8, data: closePayload(code, reason))
                        reads.async { [self] in
                            defer { deadline.finish(); socket.stop() }
                            do {
                                while stateLock.withLock({ remoteClosed == nil }) { _ = try readMessage() }
                                continuation.resume()
                            } catch {
                                if stateLock.withLock({ remoteClosed != nil }) { continuation.resume() }
                                else { continuation.resume(throwing: deadline.expired ? DuplexError.transport("WebSocket close handshake timed out") : error) }
                            }
                        }
                    } catch { deadline.finish(); socket.stop(); continuation.resume(throwing: error) }
                }
            }
        } onCancel: { self.socket.stop() }
    }
    func abort() async { stopLocally() }
    private func stopLocally() { stateLock.withLock { localClosed = true }; socket.stop() }
    private func translate(_ error: any Error) -> any Error {
        stateLock.withLock {
            if localClosed { return DuplexError.closed }
            return remoteClosed ?? error
        }
    }
    private func writeFrame(opcode: UInt8, data: Data) throws {
        var header = Data([0x80 | opcode])
        let mask: UInt8 = client ? 0x80 : 0
        if data.count < 126 { header.append(mask | UInt8(data.count)) }
        else if data.count <= 65535 {
            header.append(mask | 126); header.append(UInt8(data.count >> 8)); header.append(UInt8(data.count & 255))
        } else {
            header.append(mask | 127)
            for shift in stride(from: 56, through: 0, by: -8) { header.append(UInt8((UInt64(data.count) >> shift) & 255)) }
        }
        if client {
            let key = (0..<4).map { _ in UInt8.random(in: 0...255) }
            header.append(contentsOf: key)
            header.append(contentsOf: data.enumerated().map { $0.element ^ key[$0.offset % 4] })
        } else { header.append(data) }
        try socket.write(header)
    }
    private func control(opcode: UInt8, data: Data) throws {
        try writes.sync { try writeFrame(opcode: opcode, data: data) }
    }
    private func fail(_ code: Int, _ reason: String) throws -> Never {
        try? control(opcode: 8, data: closePayload(code, reason))
        socket.stop()
        throw CloseError(code: code, reason: reason)
    }
    private func readMessage() throws -> Frame {
        var message = Data()
        var messageOpcode: UInt8?
        while true {
            let header = try socket.read(2)
            let final = header[0] & 0x80 != 0
            let opcode = header[0] & 0x0f
            let masked = header[1] & 0x80 != 0
            guard header[0] & 0x70 == 0, masked != client else { try fail(1002, "invalid frame header") }
            var length = UInt64(header[1] & 0x7f)
            if length == 126 {
                let extended = try socket.read(2)
                length = UInt64(extended[0]) << 8 | UInt64(extended[1])
                guard length >= 126 else { try fail(1002, "noncanonical frame length") }
            } else if length == 127 {
                let extended = try socket.read(8)
                guard extended[0] & 0x80 == 0 else { try fail(1002, "invalid frame length") }
                length = extended.reduce(0) { ($0 << 8) | UInt64($1) }
                guard length >= 65536 else { try fail(1002, "noncanonical frame length") }
            }
            let isControl = opcode >= 8
            guard !isControl || (final && length <= 125) else { try fail(1002, "invalid control frame") }
            guard [UInt8(0), 1, 2, 8, 9, 10].contains(opcode) else { try fail(1002, "unknown frame opcode") }
            if !isControl {
                guard (opcode == 0 && messageOpcode != nil) || (opcode != 0 && messageOpcode == nil) else {
                    try fail(1002, "invalid message fragmentation")
                }
                if opcode != 0 { messageOpcode = opcode }
                guard length <= UInt64(Int.max - message.count),
                      limit <= 0 || length <= UInt64(max(0, limit - message.count)) else { try fail(1009, "receive limit exceeded") }
            }
            let key = masked ? try socket.read(4) : Data()
            guard length <= UInt64(Int.max) else { try fail(1009, "receive limit exceeded") }
            var data = try socket.read(Int(length))
            if masked { for index in data.indices { data[index] ^= key[index % 4] } }
            switch opcode {
            case 8:
                guard data.count != 1 else { try fail(1002, "invalid close payload") }
                let code = data.isEmpty ? 1005 : Int(data[0]) << 8 | Int(data[1])
                guard data.isEmpty || validCloseCode(code),
                      let reason = String(data: data.dropFirst(min(2, data.count)), encoding: .utf8) else {
                    try fail(1002, "invalid close payload")
                }
                let close = CloseError(code: code, reason: reason)
                let reply = stateLock.withLock { remoteClosed = close; return !localClosed }
                if reply { try? control(opcode: 8, data: data) }
                socket.stop(); throw close
            case 9: try control(opcode: 10, data: data)
            case 10: break
            default:
                message.append(data)
                if final {
                    if messageOpcode == 1 && String(data: message, encoding: .utf8) == nil { try fail(1007, "invalid text UTF-8") }
                    return Frame(kind: messageOpcode == 1 ? .text : .binary, data: message)
                }
            }
        }
    }
    deinit { socket.stop() }
}

private final class CloseDeadline: @unchecked Sendable {
    let lock = NSLock()
    var done = false
    var timedOut = false
    var expired: Bool { lock.withLock { timedOut } }
    func expire() -> Bool {
        lock.withLock { guard !done else { return false }; timedOut = true; return true }
    }
    func finish() { lock.withLock { done = true } }
}

private func closePayload(_ code: Int, _ reason: String) -> Data {
    var data = Data([UInt8(code >> 8), UInt8(code & 255)]); data.append(contentsOf: reason.utf8); return data
}
private func validCloseCode(_ code: Int) -> Bool {
    (1000...1014).contains(code) && ![1004, 1005, 1006].contains(code) || (3000...4999).contains(code)
}
private func validateProtocols(_ protocols: [String]) throws {
    // The profile's registered spelling includes '/', as do the Go and
    // TypeScript transports. Reject delimiters and header injection while
    // preserving that established subprotocol spelling.
    let separators = Set("()<>@,;:\\\"[]?={} \t".utf8)
    guard protocols.allSatisfy({ !$0.isEmpty && $0.utf8.allSatisfy { $0 > 32 && $0 < 127 && !separators.contains($0) } }) else {
        throw DuplexError.protocolError("invalid WebSocket subprotocol token")
    }
}
private func headerContains(_ value: String?, _ token: String) -> Bool {
    value?.split(separator: ",").contains { $0.trimmingCharacters(in: .whitespaces).lowercased() == token } ?? false
}
private func readHTTP(_ socket: Socket) throws -> (first: String, headers: [String: String]) {
    var bytes = Data()
    while !bytes.suffix(4).elementsEqual([13, 10, 13, 10]) {
        guard bytes.count < 16384 else { throw DuplexError.protocolError("HTTP upgrade exceeds limit") }
        bytes.append(try socket.read(1))
    }
    guard let text = String(data: bytes, encoding: .utf8) else { throw DuplexError.protocolError("invalid HTTP upgrade") }
    let lines = text.components(separatedBy: "\r\n")
    var headers: [String: String] = [:]
    for line in lines.dropFirst() where !line.isEmpty {
        guard let colon = line.firstIndex(of: ":") else { throw DuplexError.protocolError("invalid HTTP header") }
        let key = line[..<colon].lowercased()
        let value = line[line.index(after: colon)...].trimmingCharacters(in: .whitespaces)
        if let previous = headers[key] { headers[key] = previous + ", " + value } else { headers[key] = value }
    }
    return (lines[0], headers)
}

private final class Socket: @unchecked Sendable {
    let fd: Int32
    let lock = NSLock()
    var stopped = false
    init(_ fd: Int32) {
        self.fd = fd
        #if canImport(Darwin)
        var enabled: Int32 = 1
        _ = setsockopt(fd, SOL_SOCKET, SO_NOSIGPIPE, &enabled, socklen_t(MemoryLayout<Int32>.size))
        #endif
    }
    static func make() throws -> Socket {
        #if canImport(Glibc)
        let fd = Glibc.socket(AF_INET, Int32(SOCK_STREAM.rawValue), 0)
        #else
        let fd = Darwin.socket(AF_INET, SOCK_STREAM, 0)
        #endif
        guard fd >= 0 else { throw failure() }; return Socket(fd)
    }
    static func address(host: String, port: Int) throws -> sockaddr_in {
        guard (0...65535).contains(port) else { throw DuplexError.transport("invalid TCP port") }
        var address = sockaddr_in()
        address.sin_family = sa_family_t(AF_INET); address.sin_port = UInt16(port).bigEndian
        if host == "localhost" { address.sin_addr.s_addr = inet_addr("127.0.0.1") }
        else if inet_pton(AF_INET, host, &address.sin_addr) != 1 {
            var hints = addrinfo(); hints.ai_family = AF_INET
            var result: UnsafeMutablePointer<addrinfo>?
            let status = getaddrinfo(host, nil, &hints, &result)
            guard status == 0, let result else { throw DuplexError.transport("cannot resolve host \(host)") }
            defer { freeaddrinfo(result) }
            address.sin_addr = result.pointee.ai_addr.withMemoryRebound(to: sockaddr_in.self, capacity: 1) { $0.pointee.sin_addr }
        }
        return address
    }
    static func connect(host: String, port: Int) throws -> Socket {
        let socket = try make(); var address = try address(host: host, port: port)
        let result = withUnsafePointer(to: &address) { pointer in
            pointer.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                #if canImport(Glibc)
                Glibc.connect(socket.fd, $0, socklen_t(MemoryLayout<sockaddr_in>.size))
                #else
                Darwin.connect(socket.fd, $0, socklen_t(MemoryLayout<sockaddr_in>.size))
                #endif
            }
        }
        guard result == 0 else { throw failure() }; return socket
    }
    static func listen(host: String, port: Int) throws -> Socket {
        let socket = try make(); var address = try address(host: host, port: port)
        var yes: Int32 = 1
        _ = setsockopt(socket.fd, SOL_SOCKET, SO_REUSEADDR, &yes, socklen_t(MemoryLayout<Int32>.size))
        let bound = withUnsafePointer(to: &address) { pointer in
            pointer.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                bind(socket.fd, $0, socklen_t(MemoryLayout<sockaddr_in>.size))
            }
        }
        guard bound == 0 else { throw failure() }
        #if canImport(Glibc)
        let listening = Glibc.listen(socket.fd, 128)
        #else
        let listening = Darwin.listen(socket.fd, 128)
        #endif
        guard listening == 0 else { throw failure() }; return socket
    }
    func accept() throws -> Socket {
        while true {
            #if canImport(Glibc)
            let accepted = Glibc.accept(fd, nil, nil)
            #else
            let accepted = Darwin.accept(fd, nil, nil)
            #endif
            if accepted >= 0 { return Socket(accepted) }
            if errno != EINTR { throw Self.failure() }
        }
    }
    func port() throws -> Int {
        var address = sockaddr_in(); var length = socklen_t(MemoryLayout<sockaddr_in>.size)
        let result = withUnsafeMutablePointer(to: &address) { pointer in
            pointer.withMemoryRebound(to: sockaddr.self, capacity: 1) { getsockname(fd, $0, &length) }
        }
        guard result == 0 else { throw Self.failure() }; return Int(UInt16(bigEndian: address.sin_port))
    }
    func read(_ count: Int) throws -> Data {
        // Grow in bounded chunks: an untrusted 64-bit length never causes an
        // allocation before that many bytes have actually arrived.
        var data = Data()
        var chunk = [UInt8](repeating: 0, count: min(count, 65536))
        while data.count < count {
            let amount = min(chunk.count, count - data.count)
            let read = chunk.withUnsafeMutableBytes { recv(fd, $0.baseAddress, amount, 0) }
            if read < 0 && errno == EINTR { continue }
            guard read > 0 else { throw CloseError(code: 1006) }
            data.append(contentsOf: chunk.prefix(read))
        }
        return data
    }
    func write(_ data: Data) throws {
        try data.withUnsafeBytes { bytes in
            var position = 0
            while position < bytes.count {
                #if canImport(Glibc)
                let sent = Glibc.send(fd, bytes.baseAddress!.advanced(by: position), bytes.count - position, Int32(MSG_NOSIGNAL))
                #else
                let sent = Darwin.send(fd, bytes.baseAddress!.advanced(by: position), bytes.count - position, 0)
                #endif
                if sent < 0 && errno == EINTR { continue }
                guard sent > 0 else { throw Self.failure() }; position += sent
            }
        }
    }
    func stop() {
        lock.withLock {
            guard !stopped else { return }; stopped = true
            _ = shutdown(fd, Int32(SHUT_RDWR))
        }
    }
    deinit {
        #if canImport(Glibc)
        _ = Glibc.close(fd)
        #else
        _ = Darwin.close(fd)
        #endif
    }
    static func failure() -> DuplexError { .transport(String(cString: strerror(errno))) }
}

// RFC 6455's fixed SHA-1 challenge (not used for authentication).
private func acceptKey(_ key: String) -> String {
    var bytes = Array((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").utf8)
    let bits = UInt64(bytes.count) * 8
    bytes.append(0x80)
    while bytes.count % 64 != 56 { bytes.append(0) }
    for shift in stride(from: 56, through: 0, by: -8) { bytes.append(UInt8((bits >> shift) & 255)) }
    var hash: [UInt32] = [0x67452301, 0xefcdab89, 0x98badcfe, 0x10325476, 0xc3d2e1f0]
    func rotate(_ value: UInt32, _ bits: UInt32) -> UInt32 { (value << bits) | (value >> (32 - bits)) }
    for block in stride(from: 0, to: bytes.count, by: 64) {
        var words = [UInt32](repeating: 0, count: 80)
        for index in 0..<16 {
            let start = block + index * 4
            words[index] = bytes[start..<start+4].reduce(0) { ($0 << 8) | UInt32($1) }
        }
        for index in 16..<80 { words[index] = rotate(words[index-3] ^ words[index-8] ^ words[index-14] ^ words[index-16], 1) }
        var a = hash[0], b = hash[1], c = hash[2], d = hash[3], e = hash[4]
        for index in 0..<80 {
            let f: UInt32; let k: UInt32
            switch index {
            case 0..<20: f = (b & c) | (~b & d); k = 0x5a827999
            case 20..<40: f = b ^ c ^ d; k = 0x6ed9eba1
            case 40..<60: f = (b & c) | (b & d) | (c & d); k = 0x8f1bbcdc
            default: f = b ^ c ^ d; k = 0xca62c1d6
            }
            let next = rotate(a, 5) &+ f &+ e &+ k &+ words[index]
            e = d; d = c; c = rotate(b, 30); b = a; a = next
        }
        hash[0] &+= a; hash[1] &+= b; hash[2] &+= c; hash[3] &+= d; hash[4] &+= e
    }
    return Data(hash.flatMap { word in [24, 16, 8, 0].map { UInt8((word >> $0) & 255) } }).base64EncodedString()
}
