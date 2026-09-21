import Foundation

/// A JSON value retaining a number's original decimal token rather than rounding it.
public indirect enum JSONValue: Sendable, Equatable {
    case null
    case bool(Bool)
    case number(String)
    case string(String)
    case array([JSONValue])
    case object([String: JSONValue])
    /// Used when distinct scalar keys would collide under Swift String's canonical equivalence.
    case members([JSONMember])

    public init(parsing text: String) throws {
        var parser = JSONParser(text)
        self = try parser.parse()
    }

    public init(data: Data) throws {
        guard let text = String(data: data, encoding: .utf8) else {
            throw ValidationError("expected UTF-8 JSON")
        }
        try self.init(parsing: text)
    }

    /// A Swift dictionary projection. Use objectMembers to retain canonically equivalent keys.
    public var object: [String: JSONValue]? {
        switch self {
        case .object(let value): return value
        case .members(let members): return members.reduce(into: [:]) { $0[$1.name] = $1.value }
        default: return nil
        }
    }
    public var objectMembers: [JSONMember]? {
        switch self {
        case .object(let value): return sortedJSONKeys(value).map { JSONMember(name: $0, value: value[$0]!) }
        case .members(let members): return members
        default: return nil
        }
    }
    var scalarObject: ScalarObject? { objectMembers.map(ScalarObject.init) }
    public var array: [JSONValue]? { if case .array(let v) = self { return v }; return nil }
    public var string: String? { if case .string(let v) = self { return v }; return nil }
    public var bool: Bool? { if case .bool(let v) = self { return v }; return nil }
    public var number: String? { if case .number(let v) = self { return v }; return nil }
    public subscript(_ key: String) -> JSONValue? { scalarObject?[key] }

    public func encoded() -> String {
        switch self {
        case .null: return "null"
        case .bool(let v): return v ? "true" : "false"
        case .number(let v): return v
        case .string(let v): return quoteJSON(v)
        case .array(let v): return "[" + v.map { $0.encoded() }.joined(separator: ",") + "]"
        case .object(let v):
            return "{" + sortedJSONKeys(v).map { quoteJSON($0) + ":" + v[$0]!.encoded() }.joined(separator: ",") + "}"
        case .members(let members):
            return "{" + members.map { quoteJSON($0.name) + ":" + $0.value.encoded() }.joined(separator: ",") + "}"
        }
    }
    public var data: Data { Data(encoded().utf8) }

    public static func == (left: JSONValue, right: JSONValue) -> Bool {
        switch (left, right) {
        case (.null, .null): return true
        case (.bool(let a), .bool(let b)): return a == b
        case (.number(let a), .number(let b)): return a == b
        case (.string(let a), .string(let b)): return a.utf8.elementsEqual(b.utf8)
        case (.array(let a), .array(let b)): return a == b
        default:
            guard let a = left.scalarObject, let b = right.scalarObject else { return false }
            return a.storage == b.storage
        }
    }
}

public struct JSONMember: Sendable, Equatable {
    public let name: String
    public let value: JSONValue
    public init(name: String, value: JSONValue) { self.name = name; self.value = value }
    public static func == (left: JSONMember, right: JSONMember) -> Bool { left.name.utf8.elementsEqual(right.name.utf8) && left.value == right.value }
}

struct ScalarObject {
    var storage: [Data: JSONMember]
    init(_ members: [JSONMember]) { storage = members.reduce(into: [:]) { $0[Data($1.name.utf8)] = $1 } }
    var keys: [String] { storage.values.map(\.name) }
    var isEmpty: Bool { storage.isEmpty }
    var count: Int { storage.count }
    subscript(_ key: String) -> JSONValue? { storage[Data(key.utf8)]?.value }
}

func sortedJSONKeys(_ values: ScalarObject) -> [String] { values.keys.sorted { $0.utf16.lexicographicallyPrecedes($1.utf16) } }

public struct ValidationError: Error, Sendable, CustomStringConvertible, LocalizedError {
    public let message: String
    public init(_ message: String) { self.message = message }
    public var description: String { message }
    public var errorDescription: String? { message }
}

func sortedJSONKeys<T>(_ values: [String: T]) -> [String] {
    values.keys.sorted { $0.utf16.lexicographicallyPrecedes($1.utf16) }
}

func quoteJSON(_ text: String) -> String {
    var result = "\""
    for scalar in text.unicodeScalars {
        switch scalar.value {
        case 34: result += "\\\""
        case 92: result += "\\\\"
        case 8: result += "\\b"
        case 9: result += "\\t"
        case 10: result += "\\n"
        case 12: result += "\\f"
        case 13: result += "\\r"
        case 0...31, 38, 60, 62, 0x2028, 0x2029:
            result += String(format: "\\u%04x", scalar.value)
        default: result.unicodeScalars.append(scalar)
        }
    }
    return result + "\""
}

/// Read a top-level member's original JSON spelling, including its internal whitespace.
public func rawMember(_ name: String, in text: String) throws -> String? {
    var parser = JSONParser(text)
    _ = try parser.parse()
    guard let range = parser.members[Data(name.utf8)] else { return nil }
    return String(decoding: parser.bytes[range], as: UTF8.self)
}

struct JSONParser {
    let bytes: [UInt8]
    var position = 0
    var members: [Data: Range<Int>] = [:]
    var duplicateRootMember: String?
    init(_ text: String) { bytes = Array(text.utf8) }
    mutating func whitespace() {
        while position < bytes.count && [9, 10, 13, 32].contains(bytes[position]) { position += 1 }
    }
    func failure(_ message: String = "invalid JSON") -> ValidationError {
        ValidationError("\(message) at byte \(position)")
    }
    mutating func consume(_ byte: UInt8) -> Bool {
        if position < bytes.count && bytes[position] == byte { position += 1; return true }
        return false
    }
    mutating func parse() throws -> JSONValue {
        let result = try value(depth: 0)
        whitespace()
        guard position == bytes.count else { throw ValidationError("expected exactly one JSON value") }
        return result
    }
    mutating func value(depth: Int) throws -> JSONValue {
        guard depth < 512 else { throw failure("JSON nesting exceeds limit") }
        whitespace()
        guard position < bytes.count else { throw failure() }
        switch bytes[position] {
        case 34: return .string(try string())
        case 91:
            position += 1; whitespace()
            var items: [JSONValue] = []
            if consume(93) { return .array(items) }
            while true {
                items.append(try value(depth: depth + 1)); whitespace()
                if consume(93) { return .array(items) }
                guard consume(44) else { throw failure() }
            }
        case 123:
            position += 1; whitespace()
            var fields: [Data: JSONMember] = [:]
            if consume(125) { return .object([:]) }
            while true {
                whitespace()
                let key = try string()
                whitespace()
                guard consume(58) else { throw failure() }
                whitespace(); let start = position
                let member = try value(depth: depth + 1)
                let keyBytes = Data(key.utf8)
                if depth == 0 {
                    if fields[keyBytes] != nil { duplicateRootMember = key }
                    members[keyBytes] = start..<position
                }
                fields[keyBytes] = JSONMember(name: key, value: member)
                whitespace()
                if consume(125) {
                    let members = fields.values.sorted { $0.name.utf16.lexicographicallyPrecedes($1.name.utf16) }
                    let dictionary = members.reduce(into: [String: JSONValue]()) { $0[$1.name] = $1.value }
                    return dictionary.count == members.count ? .object(dictionary) : .members(members)
                }
                guard consume(44) else { throw failure() }
            }
        case 116: try literal("true"); return .bool(true)
        case 102: try literal("false"); return .bool(false)
        case 110: try literal("null"); return .null
        default: return .number(try number())
        }
    }
    mutating func literal(_ value: String) throws {
        for byte in value.utf8 { guard consume(byte) else { throw failure() } }
    }
    mutating func number() throws -> String {
        let start = position
        _ = consume(45)
        if !consume(48) {
            guard position < bytes.count && (49...57).contains(bytes[position]) else { throw failure() }
            while position < bytes.count && (48...57).contains(bytes[position]) { position += 1 }
        }
        if consume(46) {
            let begin = position
            while position < bytes.count && (48...57).contains(bytes[position]) { position += 1 }
            guard position > begin else { throw failure() }
        }
        if consume(101) || consume(69) {
            if !consume(43) { _ = consume(45) }
            let begin = position
            while position < bytes.count && (48...57).contains(bytes[position]) { position += 1 }
            guard position > begin else { throw failure() }
        }
        return String(decoding: bytes[start..<position], as: UTF8.self)
    }
    mutating func hex4() throws -> UInt32 {
        var value: UInt32 = 0
        for _ in 0..<4 {
            guard position < bytes.count else { throw failure() }
            let byte = bytes[position]; position += 1
            let digit: UInt32
            switch byte {
            case 48...57: digit = UInt32(byte - 48)
            case 65...70: digit = UInt32(byte - 55)
            case 97...102: digit = UInt32(byte - 87)
            default: throw failure()
            }
            value = value * 16 + digit
        }
        return value
    }
    mutating func string() throws -> String {
        guard consume(34) else { throw failure() }
        var result = ""
        var start = position
        while position < bytes.count {
            let byte = bytes[position]
            if byte == 34 {
                result += String(decoding: bytes[start..<position], as: UTF8.self)
                position += 1; return result
            }
            guard byte >= 32 else { throw failure("unescaped control character") }
            if byte != 92 { position += 1; continue }
            result += String(decoding: bytes[start..<position], as: UTF8.self)
            position += 1
            guard position < bytes.count else { throw failure() }
            let escaped = bytes[position]; position += 1
            switch escaped {
            case 34: result += "\""
            case 92: result += "\\"
            case 47: result += "/"
            case 98: result += "\u{8}"
            case 102: result += "\u{c}"
            case 110: result += "\n"
            case 114: result += "\r"
            case 116: result += "\t"
            case 117:
                var scalar = try hex4()
                if (0xd800...0xdbff).contains(scalar) {
                    guard consume(92), consume(117) else { throw failure("unpaired Unicode surrogate") }
                    let trail = try hex4()
                    guard (0xdc00...0xdfff).contains(trail) else { throw failure("unpaired Unicode surrogate") }
                    scalar = 0x10000 + (scalar - 0xd800) * 1024 + trail - 0xdc00
                }
                guard let unicode = Unicode.Scalar(scalar) else { throw failure("unpaired Unicode surrogate") }
                result.unicodeScalars.append(unicode)
            default: throw failure("invalid string escape")
            }
            start = position
        }
        throw failure("unterminated JSON string")
    }
}
