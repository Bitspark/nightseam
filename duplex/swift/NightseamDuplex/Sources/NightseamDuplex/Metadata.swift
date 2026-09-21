import Foundation

/// Flat request metadata whose keys and values retain their Unicode scalar spelling.
/// Swift dictionaries can be imported for convenience; entries and dictionary
/// literals also represent keys that Swift String equality would conflate.
public struct Metadata: Sendable, Equatable, ExpressibleByDictionaryLiteral {
    private var storage: [Data: (key: String, value: String)]

    public init(_ entries: [(String, String)]) {
        storage = [:]
        for (key, value) in entries { storage[Data(key.utf8)] = (key, value) }
    }

    public init(_ map: [String: String]) {
        self.init(map.map { ($0.key, $0.value) })
    }

    public init(dictionaryLiteral elements: (String, String)...) {
        self.init(elements)
    }

    public var entries: [(key: String, value: String)] {
        storage.values.sorted { $0.key.utf16.lexicographicallyPrecedes($1.key.utf16) }
    }

    public var isEmpty: Bool { storage.isEmpty }

    public subscript(_ key: String) -> String? { storage[Data(key.utf8)]?.value }

    public func filter(_ isIncluded: ((key: String, value: String)) throws -> Bool) rethrows -> Metadata {
        try Metadata(entries.filter(isIncluded))
    }

    public static func == (left: Metadata, right: Metadata) -> Bool {
        guard left.storage.count == right.storage.count else { return false }
        for (key, member) in left.storage {
            guard let other = right.storage[key], member.value.utf8.elementsEqual(other.value.utf8) else { return false }
        }
        return true
    }
}
