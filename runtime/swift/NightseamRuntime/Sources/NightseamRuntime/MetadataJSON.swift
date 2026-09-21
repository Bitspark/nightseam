import NightseamDuplex

extension Metadata {
    public init(jsonValue: JSONValue) throws {
        guard let members = jsonValue.objectMembers else { throw ValidationError("metadata is an object of strings") }
        try self.init(members.map { member in
            guard let value = member.value.string else { throw ValidationError("metadata is an object of strings") }
            return (member.name, value)
        })
    }
    public var jsonValue: JSONValue {
        .members(entries.map { JSONMember(name: $0.0, value: .string($0.1)) })
    }
}
