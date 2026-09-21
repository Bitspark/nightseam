import Foundation

/// A family's wire descriptor and the imported families in its lexical environment.
public struct Descriptor: Sendable {
    private let schemas: [String: JSONValue]
    public let digest: String
    private var scope: [String: TypeArgument] = [:]

    public init(wire: JSONValue, digest: String = "", imported: [String: JSONValue] = [:]) throws {
        guard validDigest(digest, empty: true) else { throw ValidationError("schema.digest: expected empty or lowercase SHA-256 digest") }
        guard wire["types"]?.object != nil else { throw ValidationError("expected family descriptor with types") }
        try checkPatterns(wire)
        for name in sortedJSONKeys(imported) { try checkPatterns(imported[name]!) }
        var schemas = imported
        schemas[""] = wire
        self.schemas = schemas; self.digest = digest
    }

    public init(text: String, digest: String = "", imported: [String: JSONValue] = [:]) throws {
        try self.init(wire: JSONValue(parsing: text), digest: digest, imported: imported)
    }

    public func bind(types: [String: JSONValue] = [:], families: [String: String] = [:]) -> Descriptor {
        var bound = self
        for (name, value) in types { bound.scope[name] = .type(TypeExpression(schema: "", value: value, scope: scope)) }
        for (name, family) in families { bound.scope[name] = .family(family) }
        return bound
    }

    public func validate(expression: JSONValue, value: JSONValue, at: String = "$") throws {
        try checkPatterns(expression)
        for name in sortedJSONKeys(scope) {
            if case .type(let type) = scope[name]! { try checkPatterns(type.value) }
        }
        try validate(TypeExpression(schema: "", value: expression, scope: scope), value, at)
    }

    public func validate(expression: JSONValue, text: String, at: String = "$") throws {
        try validate(expression: expression, value: JSONValue(parsing: text), at: at)
    }

    public func fields(_ name: String) throws -> [String] {
        try fields(resolve(TypeExpression(schema: "", value: .string(name), scope: scope), "$"), "$", []).map { $0.0["name"]?.string ?? "" }
    }

    private func parameters(_ schema: String) -> [JSONValue] { schemas[schema]?["parameters"]?.array ?? [] }
    private func types(_ schema: String) -> [String: JSONValue] { schemas[schema]?["types"]?.object ?? [:] }
    private func singleFamily(_ schema: String) -> String? {
        let items = parameters(schema).filter { !($0["of"]?.string ?? "").isEmpty }
        return items.count == 1 ? items[0]["name"]?.string : nil
    }
    private func unbound(_ expression: TypeExpression, _ name: String) -> Bool {
        if let arg = expression.scope[name] { if case .unbound = arg { return true }; return false }
        return parameters(expression.schema).contains { $0["name"]?.string == name && !($0["of"]?.string ?? "").isEmpty }
    }

    /// Family parameters are needed only where the declaration actually uses them.
    private func freeParameters(_ schema: String, _ definition: JSONValue, _ seen: Set<String> = []) -> [JSONValue] {
        let identity = schema + ":" + definition.encoded()
        if seen.contains(identity) { return [] }
        var seen = seen; seen.insert(identity)
        var used: Set<String> = []
        func inherit(_ target: JSONValue?) {
            guard let target else { return }
            for parameter in freeParameters(schema, target, seen) { used.insert(parameter["name"]?.string ?? "") }
        }
        func base(_ value: JSONValue) {
            if let fillers = value["with"]?.object, value["apply"]?.string != nil { for filler in fillers.values { walk(filler) } }
            else { walk(value) }
        }
        func walk(_ value: JSONValue) {
            if let name = value.string {
                let pieces = name.split(separator: ".", maxSplits: 1, omittingEmptySubsequences: false).map(String.init)
                for parameter in parameters(schema) where parameter["name"]?.string == pieces[0] {
                    used.insert(pieces[0]); return
                }
                if pieces.count == 1 { inherit(types(schema)[name]); return }
                if let target = types(pieces[0])[pieces[1]], let source = singleFamily(schema) {
                    for parameter in freeParameters(pieces[0], target, seen) + (target["parameters"]?.array ?? []) where !(parameter["of"]?.string ?? "").isEmpty { used.insert(source) }
                }
                return
            }
            guard let object = value.object else { return }
            for key in ["array", "map", "nullable"] { if let inner = object[key] { walk(inner); return } }
            if let target = object["apply"]?.string {
                if !target.contains(".") { inherit(types(schema)[target]) }
                for filler in object["with"]?.object?.values ?? [:].values { walk(filler) }
                return
            }
            if let entity = object["ref"]?.string, let target = types(schema)[entity] {
                for field in target["fields"]?.array ?? [] where field["name"] == target["key"] { if let type = field["type"] { walk(type) } }
                return
            }
            if object["kind"] != nil {
                for field in object["fields"]?.array ?? [] { if let type = field["type"] { walk(type) } }
                for parent in object["extends"]?.array ?? [] { base(parent) }
                for variant in object["variants"]?.object?.values ?? [:].values { walk(variant) }
            }
        }
        if let type = definition["type"] { walk(type) }
        for field in definition["fields"]?.array ?? [] { if let type = field["type"] { walk(type) } }
        for parent in definition["extends"]?.array ?? [] { base(parent) }
        for variant in definition["variants"]?.object?.values ?? [:].values { walk(variant) }
        return parameters(schema).filter { used.contains($0["name"]?.string ?? "") }
    }

    private func named(_ expression: TypeExpression, _ name: String, _ at: String) throws -> ResolvedType {
        var expression = expression
        var name = name
        let pieces = name.split(separator: ".", maxSplits: 1, omittingEmptySubsequences: false).map(String.init)
        if pieces.count == 2 {
            let family = pieces[0]
            guard let first = family.utf8.first else { throw expected(at, "known family") }
            let caller = expression
            let schema: String
            if (65...90).contains(first) {
                guard case .family(let bound) = expression.scope[family] else { throw expected(at, "a binding of the parameter " + family) }
                schema = bound
            } else {
                guard schemas[family] != nil else { throw expected(at, "known family") }
                schema = family
            }
            expression = TypeExpression(schema: schema, value: .string(pieces[1]), scope: [:])
            if (97...122).contains(first), let source = singleFamily(caller.schema), let target = types(schema)[pieces[1]] {
                for parameter in freeParameters(schema, target) + (target["parameters"]?.array ?? []) where !(parameter["of"]?.string ?? "").isEmpty {
                    let name = parameter["name"]?.string ?? ""
                    if let argument = caller.scope[source] { expression.scope[name] = argument }
                    else if unbound(caller, source) { expression.scope[name] = .unbound }
                }
            }
            name = pieces[1]
        }
        guard let definition = types(expression.schema)[name] else { throw expected(at, "known type") }
        expression.value = .string(name)
        return ResolvedType(expression: expression, definition: definition, name: name, identity: expression.schema + ":" + name)
    }

    private func resolve(_ expression: TypeExpression, _ at: String, inheritance: Bool = false) throws -> ResolvedType {
        var expression = expression
        var aliases = expression.aliases
        var inheritance = inheritance
        while true {
            var result: ResolvedType
            if let name = expression.value.string {
                if let argument = expression.scope[name] {
                    guard case .type(let captured) = argument else { throw expected(at, "type argument") }
                    expression = captured; aliases = captured.aliases; continue
                }
                if let dot = name.firstIndex(of: "."), unbound(expression, String(name[..<dot])) {
                    expression.value = .string("json"); return ResolvedType(expression: expression)
                }
                if ["json", "string", "boolean", "number", "integer", "timestamp"].contains(name) { return ResolvedType(expression: expression) }
                result = try named(expression, name, at)
            } else if let object = expression.value.object {
                if let reference = object["apply"]?.string {
                    result = try named(expression, reference, at)
                    let definition = result.definition!
                    guard let fillers = object["with"]?.object else { throw expected(at, "application arguments") }
                    var scope = result.expression.scope
                    var parameters = definition["parameters"]?.array ?? []
                    if inheritance || reference.contains(".") { parameters = freeParameters(result.expression.schema, definition) + parameters }
                    var allowed: Set<String> = []
                    for parameter in parameters {
                        let name = parameter["name"]?.string ?? ""
                        allowed.insert(name)
                        guard let filler = fillers[name] else { throw expected(at, "an argument for " + name) }
                        if (parameter["of"]?.string ?? "").isEmpty {
                            var captured = expression.child(filler); captured.aliases = aliases
                            scope[name] = .type(captured)
                        } else {
                            guard let family = filler.string else { throw expected(at, "family argument for " + name) }
                            if unbound(expression, family) { scope[name] = .unbound; continue }
                            var schema = schemas[family] == nil ? nil : family
                            if let argument = expression.scope[family] {
                                if case .family(let bound) = argument { schema = bound } else { schema = nil }
                            }
                            guard let schema else { throw expected(at, "known family argument for " + name) }
                            scope[name] = .family(schema)
                        }
                    }
                    for name in sortedJSONKeys(fillers) where !allowed.contains(name) { throw expected(at, "known parameter " + name) }
                    result.expression.scope = scope
                } else if let kind = object["kind"]?.string {
                    result = ResolvedType(expression: expression, definition: expression.value, name: kind, identity: expression.schema + ":" + expression.value.encoded())
                } else { return ResolvedType(expression: expression) }
            } else { throw expected(at, "type expression") }
            inheritance = false
            if result.definition?["kind"]?.string == "alias" {
                guard !aliases.contains(result.identity) else { throw expected(at, "acyclic type expression") }
                aliases.insert(result.identity)
                expression = result.expression
                expression.value = result.definition?["type"] ?? .null
                continue
            }
            return result
        }
    }

    private func inherited(_ expression: TypeExpression, _ value: JSONValue, _ at: String) throws -> ResolvedType {
        if let name = value.string {
            let result = try named(expression, name, at)
            if !(result.definition?["parameters"]?.array ?? []).isEmpty || !freeParameters(result.expression.schema, result.definition!).isEmpty {
                throw expected(at, "explicit application of generic base " + name)
            }
        }
        return try resolve(expression.child(value), at, inheritance: true)
    }

    private func fields(_ resolved: ResolvedType, _ at: String, _ seen: Set<String>) throws -> [(JSONValue, TypeExpression)] {
        guard let definition = resolved.definition else { throw expected(at, "record") }
        guard !seen.contains(resolved.identity) else { throw expected(at, "acyclic inheritance") }
        var seen = seen; seen.insert(resolved.identity)
        var fields: [(JSONValue, TypeExpression)] = []
        for parent in definition["extends"]?.array ?? [] {
            fields += try self.fields(inherited(resolved.expression, parent, at), at, seen)
        }
        for field in definition["fields"]?.array ?? [] { fields.append((field, resolved.expression.child(field["type"] ?? .null))) }
        return fields
    }

    private func variants(_ resolved: ResolvedType, _ at: String, _ seen: Set<String>) throws -> [String: TypeExpression] {
        guard let definition = resolved.definition, definition["kind"]?.string == "union" else { throw expected(at, "union") }
        guard !seen.contains(resolved.identity) else { throw expected(at, "acyclic inheritance") }
        var seen = seen; seen.insert(resolved.identity)
        var variants: [String: TypeExpression] = [:]
        for parent in definition["extends"]?.array ?? [] {
            variants.merge(try self.variants(inherited(resolved.expression, parent, at), at, seen)) { _, new in new }
        }
        for (name, variant) in definition["variants"]?.object ?? [:] { variants[name] = resolved.expression.child(variant) }
        return variants
    }

    private func validate(_ expression: TypeExpression, _ value: JSONValue, _ at: String) throws {
        let resolved = try resolve(expression, at)
        let expression = resolved.expression
        if let definition = resolved.definition {
            switch definition["kind"]?.string {
            case "callable":
                let contract = definition["contract"]?.string ?? ""
                guard let object = value.scalarObject else { throw expected(at, "a live reference to " + contract) }
                guard !(object["binding"]?.string ?? "").isEmpty else { throw ValidationError(at + ".binding: a live reference names the binding it refers to") }
                guard let carried = object["contract"]?.string else { throw ValidationError(at + ".contract: a live reference carries the declaration it implements") }
                guard carried.utf8.elementsEqual(contract.utf8) else { throw ValidationError(at + ".contract: the reference carries " + carried + " where " + contract + " is expected") }
                if let member = object["digest"] {
                    guard let carried = member.string, validDigest(carried) else { throw expected(at + ".digest", "lowercase SHA-256 digest") }
                    if expression.schema.isEmpty && !digest.isEmpty && carried != digest {
                        throw ValidationError(at + ".digest: the reference to " + contract + " carries declaration digest " + carried + " where " + digest + " is expected")
                    }
                }
                for key in sortedJSONKeys(object) where !["binding", "contract", "digest"].contains(key) { throw ValidationError(at + "." + key + ": unknown field") }
            case "enum":
                guard let actual = value.string, (definition["values"]?.array ?? []).contains(where: { $0.string?.utf8.elementsEqual(actual.utf8) == true }) else { throw expected(at, resolved.name) }
            case "record", "entity":
                guard let object = value.scalarObject else { throw expected(at, resolved.name + " object") }
                var allowed: Set<Data> = []
                for (field, type) in try fields(resolved, at, []) {
                    let name = field["name"]?.string ?? ""
                    allowed.insert(Data(name.utf8))
                    let location = at + "." + name
                    guard let member = object[name] else {
                        if field["required"]?.bool == true { throw ValidationError(location + ": required field missing") }
                        continue
                    }
                    if member == .null {
                        if field["nullable"]?.bool == true { continue }
                        if try resolve(type, location).expression.value["nullable"] == nil { throw ValidationError(location + ": null is not permitted") }
                    }
                    try validate(type, member, location)
                    if member != .null { try constrain(field, member, location) }
                }
                for key in sortedJSONKeys(object) where !allowed.contains(Data(key.utf8)) {
                    if definition["open"]?.bool != true { throw ValidationError(at + "." + key + ": unknown field") }
                    try validateJSON(object[key]!, at + "." + key)
                }
            case "union":
                guard let object = value.scalarObject else { throw expected(at, resolved.name + " object") }
                let tag = definition["tag"]?.string ?? ""
                guard let member = object[tag] else { throw ValidationError(at + "." + tag + ": required field missing") }
                guard let name = member.string else { throw expected(at + "." + tag, "known variant") }
                guard let variant = try variants(resolved, at, [])[name] else { throw expected(at + "." + tag, "known variant") }
                let key = definition["value"]?.string ?? "value"
                let empty = variant.value.object?.count == 1 && variant.value["empty"]?.bool == true
                if !empty {
                    guard let wrapped = object[key] else { throw ValidationError(at + "." + key + ": required field missing") }
                    try validate(variant, wrapped, at + "." + key)
                }
                for name in sortedJSONKeys(object) where !name.utf8.elementsEqual(tag.utf8) && (empty || !name.utf8.elementsEqual(key.utf8)) { throw ValidationError(at + "." + name + ": unknown field") }
            default: throw expected(at, "supported type")
            }
            return
        }
        if let object = expression.value.object {
            if let inner = object["nullable"] { if value != .null { try validate(expression.child(inner), value, at) }; return }
            if let literal = object["literal"] {
                guard let text = literal.string, !text.isEmpty else { throw expected(at, "nonempty string literal") }
                guard value.string?.utf8.elementsEqual(text.utf8) == true else { throw expected(at, "literal " + quoteJSON(text)) }
                return
            }
            if let element = object["array"] {
                guard let items = value.array else { throw expected(at, "array") }
                for (index, item) in items.enumerated() { try validate(expression.child(element), item, at + "[\(index)]") }
                return
            }
            if let element = object["map"] {
                guard let items = value.scalarObject else { throw expected(at, "object") }
                for key in sortedJSONKeys(items) { try validate(expression.child(element), items[key]!, at + "." + key) }
                return
            }
            if let entity = object["ref"]?.string {
                let target: ResolvedType
                do { target = try named(expression, entity, at) } catch { throw expected(at, "known entity") }
                let result = try resolve(target.expression, at)
                for (field, type) in try fields(result, at, []) where field["name"] == target.definition?["key"] { try validate(type, value, at); return }
                throw expected(at, "entity with a key")
            }
            if object["empty"] != nil {
                guard value.object?.isEmpty == true else { throw expected(at, "empty object") }; return
            }
            throw expected(at, "supported type expression")
        }
        switch expression.value.string ?? "" {
        case "json": try validateJSON(value, at)
        case "string": if value.string == nil { throw expected(at, "string") }
        case "boolean": if value.bool == nil { throw expected(at, "boolean") }
        case "number", "integer":
            let name = expression.value.string!
            guard let token = value.number else { throw expected(at, name) }
            guard let number = Double(token), number.isFinite else { throw expected(at, "finite number") }
            if name == "integer" && !safeInteger(token) { throw expected(at, "JavaScript-safe integer") }
        case "timestamp":
            guard let text = value.string else { throw expected(at, "timestamp") }
            guard validTimestamp(text) else { throw expected(at, "RFC3339 timestamp") }
        default: throw expected(at, "known type")
        }
    }
}

private indirect enum TypeArgument: Sendable { case type(TypeExpression), family(String), unbound }
private struct TypeExpression: Sendable {
    var schema: String
    var value: JSONValue
    var scope: [String: TypeArgument]
    var aliases: Set<String> = []
    func child(_ value: JSONValue) -> TypeExpression { TypeExpression(schema: schema, value: value, scope: scope) }
}
private struct ResolvedType {
    var expression: TypeExpression
    var definition: JSONValue? = nil
    var name: String = ""
    var identity: String = ""
}

public func validate(descriptor: Descriptor, expression: JSONValue, value: JSONValue) throws {
    try descriptor.validate(expression: expression, value: value)
}
private func expected(_ at: String, _ want: String) -> ValidationError { ValidationError(at + ": expected " + want) }
private func validDigest(_ digest: String, empty: Bool = false) -> Bool {
    (empty && digest.isEmpty) || (digest.utf8.count == 64 && digest.utf8.allSatisfy { (48...57).contains($0) || (97...102).contains($0) })
}
private func validateJSON(_ value: JSONValue, _ at: String) throws {
    switch value {
    case .number(let token): if Double(token)?.isFinite != true { throw expected(at, "finite JSON number") }
    case .array(let values): for (index, item) in values.enumerated() { try validateJSON(item, at + "[\(index)]") }
    case .object(let values): for key in sortedJSONKeys(values) { try validateJSON(values[key]!, at + "." + key) }
    case .members(let values): for member in values.sorted(by: { $0.name.utf16.lexicographicallyPrecedes($1.name.utf16) }) { try validateJSON(member.value, at + "." + member.name) }
    default: break
    }
}

private func safeInteger(_ raw: String) -> Bool {
    var text = raw.hasPrefix("-") ? String(raw.dropFirst()) : raw
    var exponent = "0"
    if let at = text.firstIndex(where: { $0 == "e" || $0 == "E" }) { exponent = String(text[text.index(after: at)...]); text = String(text[..<at]) }
    var fraction = 0
    if let at = text.firstIndex(of: ".") { fraction = text.distance(from: text.index(after: at), to: text.endIndex); text.remove(at: at) }
    text = String(text.drop(while: { $0 == "0" }))
    if text.isEmpty { return true }
    guard let power = Int(exponent), power <= raw.count + 16, power >= -raw.count - 16 else { return false }
    let scale = power - fraction
    if scale < 0 {
        guard -scale <= text.count else { return false }
        guard text.suffix(-scale).allSatisfy({ $0 == "0" }) else { return false }
        text = String(text.dropLast(-scale))
    } else {
        guard text.count + scale <= 16 else { return false }
        text += String(repeating: "0", count: scale)
    }
    return text.count <= 16 && (UInt64(text) ?? UInt64.max) <= 9_007_199_254_740_991
}

private func validTimestamp(_ text: String) -> Bool {
    let pattern = #"^([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})(?:[.,][0-9]+)?(Z|[+-][0-9]{2}:[0-9]{2})$"#
    guard let regex = try? NSRegularExpression(pattern: pattern), let match = regex.firstMatch(in: text, range: NSRange(text.startIndex..., in: text)), match.range.length == text.utf16.count else { return false }
    func number(_ group: Int) -> Int { Int((text as NSString).substring(with: match.range(at: group))) ?? -1 }
    let year = number(1), month = number(2), day = number(3)
    guard (1...12).contains(month), (0...23).contains(number(4)), (0...59).contains(number(5)), (0...59).contains(number(6)) else { return false }
    let leap = year % 4 == 0 && (year % 100 != 0 || year % 400 == 0)
    let days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31]
    guard (1...days[month - 1]).contains(day) else { return false }
    let zone = (text as NSString).substring(with: match.range(at: 7))
    if zone != "Z" {
        let parts = zone.dropFirst().split(separator: ":")
        guard let hours = Int(parts[0]), let minutes = Int(parts[1]), hours <= 24, minutes <= 60 else { return false }
    }
    return true
}

private func constrain(_ field: JSONValue, _ value: JSONValue, _ at: String) throws {
    if let number = value.number.flatMap(Double.init) {
        if let min = field["min"]?.number, let lower = Double(min), number < lower { throw expected(at, "at least " + min) }
        if let max = field["max"]?.number, let upper = Double(max), number > upper { throw expected(at, "at most " + max) }
    } else if let text = value.string {
        if let min = field["min"]?.string, text < min { throw expected(at, "at or after " + min) }
        if let max = field["max"]?.string, text > max { throw expected(at, "at or before " + max) }
    }
    if let length = field["length"], let count = value.string?.unicodeScalars.count ?? value.array?.count {
        if let min = length["min"]?.number.flatMap(Int.init), count < min { throw expected(at, "a length of at least \(min)") }
        if let max = length["max"]?.number.flatMap(Int.init), count > max { throw expected(at, "a length of at most \(max)") }
    }
    if let pattern = field["pattern"]?.string, !pattern.isEmpty, let text = value.string {
        let regex = try NightseamPattern(pattern)
        if !regex.matches(text) { throw expected(at, "a match of " + pattern) }
    }
}

private func checkPatterns(_ value: JSONValue) throws {
    if let object = value.object {
        if let pattern = object["pattern"]?.string {
            do { _ = try NightseamPattern(pattern) } catch { throw ValidationError("pattern " + quoteJSON(pattern) + ": outside Nightseam dialect") }
        }
        for key in sortedJSONKeys(object) { try checkPatterns(object[key]!) }
    } else if let values = value.array { for value in values { try checkPatterns(value) } }
}

// This code-point parser and memoized matcher implement the shared dialect,
// including counted repetitions too large for a host regex engine to compile.
private struct PatternNode {
    var kind: UInt32
    var ranges: [ClosedRange<UInt32>] = []
    var children: [Int] = []
    var minimum: UInt64 = 0
    var maximum: UInt64 = 0
    var unlimited = false
    var width: UInt64 = 0
}
private let patternDigits: [ClosedRange<UInt32>] = [48...57]
private let patternWords: [ClosedRange<UInt32>] = [48...57, 65...90, 95...95, 97...122]
private let patternSpaces: [ClosedRange<UInt32>] = [9...13, 32...32, 160...160, 0x1680...0x1680, 0x2000...0x200a, 0x2028...0x2029, 0x202f...0x202f, 0x205f...0x205f, 0x3000...0x3000, 0xfeff...0xfeff]

private func normalizedRanges(_ ranges: [ClosedRange<UInt32>]) -> [ClosedRange<UInt32>] {
    var result: [ClosedRange<UInt32>] = []
    for range in ranges.sorted(by: { $0.lowerBound < $1.lowerBound }) {
        if let last = result.last, range.lowerBound <= last.upperBound + 1 {
            result[result.count - 1] = last.lowerBound...max(last.upperBound, range.upperBound)
        } else { result.append(range) }
    }
    return result
}
private func complementRanges(_ ranges: [ClosedRange<UInt32>]) -> [ClosedRange<UInt32>] {
    var result: [ClosedRange<UInt32>] = []
    var next: UInt32 = 0
    for range in normalizedRanges(ranges) {
        if next < range.lowerBound { result.append(next...(range.lowerBound - 1)) }
        next = range.upperBound + 1
    }
    if next <= 0x10ffff { result.append(next...0x10ffff) }
    return result
}

private struct NightseamPattern {
    let nodes: [PatternNode]
    let root: Int
    init(_ source: String) throws {
        var parser = PatternParser(source: Array(source.unicodeScalars.map(\.value)))
        root = try parser.disjunction(group: false)
        nodes = parser.nodes
    }
    func matches(_ value: String) -> Bool {
        var state = PatternMatcher(nodes: nodes, input: Array(value.unicodeScalars.map(\.value)))
        for position in 0...state.input.count { if !state.ends(root, position).isEmpty { return true } }
        return false
    }
}
private struct PatternParser {
    var source: [UInt32]
    var position = 0
    var nodes: [PatternNode] = []
    var current: UInt32? { position < source.count ? source[position] : nil }
    func fail() -> ValidationError { ValidationError("outside Nightseam pattern dialect at scalar \(position)") }
    mutating func take(_ value: UInt32) -> Bool { if current == value { position += 1; return true }; return false }
    mutating func add(_ node: PatternNode) -> Int { nodes.append(node); return nodes.count - 1 }
    mutating func set(_ ranges: [ClosedRange<UInt32>]) -> Int { add(PatternNode(kind: 99, ranges: normalizedRanges(ranges), width: 1)) }
    mutating func join(_ kind: UInt32, _ children: [Int]) -> Int {
        if children.count == 1 { return children[0] }
        var width: UInt64 = kind == 124 ? UInt64.max : 0
        for child in children {
            if kind == 124 { width = min(width, nodes[child].width) }
            else { let sum = width.addingReportingOverflow(nodes[child].width); width = sum.overflow ? .max : sum.partialValue }
        }
        return add(PatternNode(kind: kind, children: children, width: width))
    }
    mutating func digits() -> String? {
        let start = position
        while let value = current, (48...57).contains(value) { position += 1 }
        guard position > start else { return nil }
        let raw = String(String.UnicodeScalarView(source[start..<position].compactMap(Unicode.Scalar.init)))
        let trimmed = String(raw.drop(while: { $0 == "0" }))
        return trimmed.isEmpty ? "0" : trimmed
    }
    mutating func disjunction(group: Bool) throws -> Int {
        var branches: [Int] = [], sequence: [Int] = []
        while let current {
            if current == 41 {
                guard group else { throw fail() }
                position += 1; branches.append(join(115, sequence)); return join(124, branches)
            }
            if take(124) { branches.append(join(115, sequence)); sequence = []; continue }
            let (atom, assertion) = try atom()
            var minimum: UInt64 = 0, maximum: UInt64 = 1
            var unlimited = false, quantified = true
            if take(42) { unlimited = true }
            else if take(43) { minimum = 1; unlimited = true }
            else if take(63) {}
            else if take(123) {
                guard let lower = digits() else { throw fail() }
                var upper = lower
                if take(44) {
                    if let value = digits() { upper = value } else { unlimited = true }
                }
                guard take(125) else { throw fail() }
                if !unlimited && (upper.count < lower.count || (upper.count == lower.count && upper < lower)) { throw fail() }
                minimum = UInt64(lower) ?? .max; maximum = UInt64(upper) ?? .max
            } else { quantified = false }
            if quantified {
                guard !assertion else { throw fail() }
                _ = take(63)
                let product = nodes[atom].width.multipliedReportingOverflow(by: minimum)
                sequence.append(add(PatternNode(kind: 114, children: [atom], minimum: minimum, maximum: maximum, unlimited: unlimited, width: product.overflow ? .max : product.partialValue)))
            } else { sequence.append(atom) }
        }
        guard !group else { throw fail() }
        branches.append(join(115, sequence)); return join(124, branches)
    }
    mutating func atom() throws -> (Int, Bool) {
        guard let value = current else { throw fail() }
        position += 1
        switch value {
        case 94, 36: return (add(PatternNode(kind: value)), true)
        case 46: return (set(complementRanges([10...10, 13...13, 0x2028...0x2029])), false)
        case 40:
            if take(63) { guard take(58) else { throw fail() } }
            return (try disjunction(group: true), false)
        case 91:
            if current == 91, position + 1 < source.count, source[position + 1] == 58 { throw fail() }
            return (set(try characterClass()), false)
        case 92:
            let (ranges, assertion) = try escape(inClass: false)
            if let assertion { return (add(PatternNode(kind: assertion)), true) }
            return (set(ranges), false)
        case 42, 43, 63, 123, 125, 93: throw fail()
        default: return (set([value...value]), false)
        }
    }
    mutating func characterClass() throws -> [ClosedRange<UInt32>] {
        let negated = take(94)
        var result: [ClosedRange<UInt32>] = []
        while let current, current != 93 {
            let left = try classAtom()
            if self.current == 45, position + 1 < source.count, source[position + 1] != 93 {
                position += 1
                let right = try classAtom()
                guard left.count == 1, right.count == 1, left[0].lowerBound == left[0].upperBound,
                      right[0].lowerBound == right[0].upperBound, left[0].lowerBound <= right[0].lowerBound else { throw fail() }
                result.append(left[0].lowerBound...right[0].lowerBound)
            } else { result += left }
        }
        guard take(93) else { throw fail() }
        return negated ? complementRanges(result) : result
    }
    mutating func classAtom() throws -> [ClosedRange<UInt32>] {
        guard let value = current else { throw fail() }
        position += 1
        if value == 92 { return try escape(inClass: true).0 }
        return [value...value]
    }
    mutating func hex(_ count: Int) throws -> UInt32 {
        var result: UInt32 = 0
        for _ in 0..<count {
            guard let value = current, let digit = hexDigit(value) else { throw fail() }
            position += 1; result = result * 16 + digit
        }
        return result
    }
    func hexDigit(_ value: UInt32) -> UInt32? {
        switch value { case 48...57: return value - 48; case 65...70: return value - 55; case 97...102: return value - 87; default: return nil }
    }
    mutating func escape(inClass: Bool) throws -> ([ClosedRange<UInt32>], UInt32?) {
        guard let value = current else { throw fail() }
        position += 1
        var scalar: UInt32
        switch value {
        case 100: return (patternDigits, nil)
        case 68: return (complementRanges(patternDigits), nil)
        case 119: return (patternWords, nil)
        case 87: return (complementRanges(patternWords), nil)
        case 115: return (patternSpaces, nil)
        case 83: return (complementRanges(patternSpaces), nil)
        case 98: if !inClass { return ([], 98) }; scalar = 8
        case 66: guard !inClass else { throw fail() }; return ([], 66)
        case 102: scalar = 12
        case 110: scalar = 10
        case 114: scalar = 13
        case 116: scalar = 9
        case 118: scalar = 11
        case 48:
            if let next = current, (48...57).contains(next) { throw fail() }
            scalar = 0
        case 99:
            guard let letter = current, (65...90).contains(letter) || (97...122).contains(letter) else { throw fail() }
            position += 1; scalar = letter & 31
        case 120: scalar = try hex(2)
        case 117:
            if take(123) {
                let start = position
                scalar = 0
                while let value = current, let digit = hexDigit(value) {
                    guard scalar <= 0x10ffff else { throw fail() }
                    scalar = scalar * 16 + digit; position += 1
                }
                guard position > start, take(125), scalar <= 0x10ffff else { throw fail() }
            } else {
                scalar = try hex(4)
                if (0xd800...0xdbff).contains(scalar), position + 1 < source.count, source[position] == 92, source[position + 1] == 117 {
                    let save = position; position += 2
                    if let trail = try? hex(4), (0xdc00...0xdfff).contains(trail) { scalar = 0x10000 + (scalar - 0xd800) * 1024 + trail - 0xdc00 }
                    else { position = save }
                }
            }
        default:
            guard [94, 36, 92, 46, 42, 43, 63, 40, 41, 91, 93, 123, 125, 124, 47].contains(value) || (inClass && value == 45) else { throw fail() }
            scalar = value
        }
        return ([scalar...scalar], nil)
    }
}
private struct PatternMatchKey: Hashable { let node: Int; let start: Int }
private struct PatternMatcher {
    let nodes: [PatternNode]
    let input: [UInt32]
    var memo: [PatternMatchKey: [Int]] = [:]
    func unique(_ values: [Int]) -> [Int] { Array(Set(values)).sorted() }
    mutating func advance(_ node: Int, _ positions: [Int]) -> [Int] {
        var result: [Int] = []
        for position in positions { result += ends(node, position) }
        return unique(result)
    }
    mutating func ends(_ index: Int, _ start: Int) -> [Int] {
        let node = nodes[index]
        if node.width > UInt64(input.count - start) { return [] }
        let key = PatternMatchKey(node: index, start: start)
        if let cached = memo[key] { return cached }
        var result: [Int] = []
        switch node.kind {
        case 99: if start < input.count && node.ranges.contains(where: { $0.contains(input[start]) }) { result = [start + 1] }
        case 94: if start == 0 { result = [start] }
        case 36: if start == input.count { result = [start] }
        case 98, 66:
            func word(_ at: Int) -> Bool { at >= 0 && at < input.count && patternWords.contains(where: { $0.contains(input[at]) }) }
            if (word(start - 1) != word(start)) == (node.kind == 98) { result = [start] }
        case 115:
            result = [start]
            for child in node.children { result = advance(child, result); if result.isEmpty { break } }
        case 124:
            for child in node.children { result += ends(child, start) }
            result = unique(result)
        case 114:
            var current = [start], count: UInt64 = 0
            while count < node.minimum && !current.isEmpty {
                let next = advance(node.children[0], current); count += 1
                if next == current { count = node.minimum; current = next; break }
                current = next
            }
            if !current.isEmpty {
                result += current
                while node.unlimited || count < node.maximum {
                    let next = advance(node.children[0], current)
                    if next.isEmpty || next == current { break }
                    result += next; current = next; count += 1
                }
                result = unique(result)
            }
        default: break
        }
        memo[key] = result; return result
    }
}
