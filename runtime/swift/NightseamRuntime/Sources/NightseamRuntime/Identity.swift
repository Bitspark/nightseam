import Foundation

public struct DeclarationIdentity: Sendable {
    public let path: String
    public let digest: String?
    public init(path: String, digest: String? = nil) throws {
        guard !path.isEmpty, digest == nil || (digest!.utf8.count == 64 && digest!.utf8.allSatisfy { (48...57).contains($0) || (97...102).contains($0) }) else {
            throw PublicError(code: "contract_invalid", message: "Invalid declaration identity")
        }
        self.path = path; self.digest = digest
    }
    public var data: Data {
        var fields: [String: JSONValue] = ["path": .string(path)]
        if let digest { fields["digest"] = .string(digest) }
        return JSONValue.object(fields).data
    }
    public init(data: Data?) throws {
        guard let data, let object = try JSONValue(data: data).object,
              object.keys.allSatisfy({ ["path", "digest"].contains($0) }),
              let path = object["path"]?.string,
              object["digest"] == nil || object["digest"]?.string != nil else {
            throw PublicError(code: "contract_invalid", message: "Invalid declaration identity")
        }
        try self.init(path: path, digest: object["digest"]?.string)
    }
    public func check(_ remote: DeclarationIdentity) throws {
        guard path.utf8.elementsEqual(remote.path.utf8), digest == nil || remote.digest == nil || digest == remote.digest else {
            throw PublicError(code: "contract_mismatch", message: "Declaration identities differ")
        }
    }
}

extension Peer {
    public func installIdentity(_ identity: DeclarationIdentity) throws {
        try handle(method: "identity.check") { _, _, data in
            try identity.check(DeclarationIdentity(data: data)); return identity.data
        }
    }
    public func checkIdentity(_ identity: DeclarationIdentity) async throws {
        do {
            let response = try await call(method: "identity.check", params: identity.data)
            try identity.check(DeclarationIdentity(data: response))
        } catch let error as PublicError where error.code == "method_not_found" {}
    }
}
