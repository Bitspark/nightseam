import Foundation

/// Check the physical profile envelope, optionally enforcing the receiving role's ID namespace.
public func validateFrame(text: String, role: String? = nil) throws -> JSONValue {
    var parser = JSONParser(text)
    let value = try parser.parse()
    guard let fields = value.object else { throw ValidationError("duplex frame must be an object") }
    if let duplicate = parser.duplicateRootMember { throw ValidationError("duplicate duplex field \(quoteJSON(duplicate))") }
    guard fields["version"] == .number("1") else { throw ValidationError("unsupported duplex frame version") }
    let invalid = ValidationError("invalid duplex frame shape")
    var allowed: Set<String> = ["version", "kind", "traceparent", "tracestate"]
    let kind = fields["kind"]?.string ?? ""
    func nonempty(_ key: String) -> Bool { !(fields[key]?.string ?? "").isEmpty }
    switch kind {
    case "request":
        allowed.formUnion(["id", "method", "params", "meta"])
        guard nonempty("id"), nonempty("method"), fields["params"] != nil else { throw invalid }
    case "response":
        allowed.formUnion(["id", "result", "error"])
        guard nonempty("id"), (fields["result"] != nil) != (fields["error"] != nil) else { throw invalid }
        if let error = fields["error"] {
            guard let details = error.object, !(details["code"]?.string ?? "").isEmpty,
                  !(details["message"]?.string ?? "").isEmpty,
                  Set(details.keys).isSubset(of: ["code", "message", "data"]) else { throw invalid }
        }
    case "event":
        allowed.formUnion(["event", "data", "meta"])
        guard nonempty("event"), fields["data"] != nil else { throw invalid }
    case "cancel":
        allowed.insert("id")
        guard nonempty("id") else { throw invalid }
    default: throw invalid
    }
    guard Set(fields.keys).isSubset(of: allowed) else { throw invalid }
    if let trace = fields["traceparent"] { guard let text = trace.string, validTraceparent(text) else { throw invalid } }
    if let state = fields["tracestate"], state.string == nil && state != .null { throw invalid }
    if let meta = fields["meta"] {
        guard let map = meta.scalarObject else { throw invalid }
        for key in map.keys where key.hasPrefix("nightseam.") || map[key]?.string == nil { throw invalid }
    }
    if let id = fields["id"]?.string, let role {
        let local = role == "server" ? "s:" : "c:"
        let remote = role == "server" ? "c:" : "s:"
        guard validID(id, prefix: kind == "response" ? local : remote) else { throw ValidationError("invalid duplex request identifier") }
    }
    return value
}

func validID(_ id: String, prefix: String) -> Bool {
    guard id.hasPrefix(prefix) else { return false }
    let number = id.dropFirst(prefix.count)
    return !number.isEmpty && number.first != "0" && number.utf8.count <= 20 && number.utf8.allSatisfy { (48...57).contains($0) }
}

func validTraceparent(_ value: String) -> Bool {
    let bytes = Array(value.utf8)
    guard bytes.count == 55 else { return false }
    for (index, byte) in bytes.enumerated() {
        if [2, 35, 52].contains(index) { if byte != 45 { return false } }
        else if !(48...57).contains(byte) && !(97...102).contains(byte) { return false }
    }
    return true
}
