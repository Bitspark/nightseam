import Foundation
import XCTest
import NightseamDuplex
@testable import NightseamRuntime

final class ValidationTests: XCTestCase {
    func testMetadataRetainsScalarKeysAndValues() throws {
        let metadata: Metadata = ["é": "composed", "e\u{301}": "decomposed", "nightseam.secret": "reserved"]
        XCTAssertEqual(metadata.entries.count, 3)
        XCTAssertEqual(metadata["é"], "composed")
        XCTAssertEqual(metadata["e\u{301}"], "decomposed")
        let filtered = metadata.filter { !$0.key.hasPrefix("nightseam.") }
        XCTAssertEqual(filtered.entries.count, 2)
        XCTAssertNil(filtered["nightseam.secret"])
        XCTAssertEqual(filtered, Metadata([("e\u{301}", "decomposed"), ("é", "composed")]))
        XCTAssertNotEqual(Metadata([("key", "é")]), Metadata([("key", "e\u{301}")]))
        XCTAssertEqual(Metadata([("key", "first"), ("key", "last")])["key"], "last")
        let encoded = JSONValue.members(filtered.entries.map { JSONMember(name: $0.key, value: .string($0.value)) }).encoded()
        let decoded = try JSONValue(parsing: encoded)
        let roundTrip = Metadata(try XCTUnwrap(decoded.objectMembers).map { ($0.name, $0.value.string!) })
        XCTAssertEqual(roundTrip, filtered)
        XCTAssertEqual(ProfileFrame(kind: .event, meta: filtered).meta, filtered)
    }
    private func table(_ name: String) throws -> JSONValue {
        var root = URL(fileURLWithPath: #filePath)
        for _ in 0..<6 { root.deleteLastPathComponent() }
        return try JSONValue(data: Data(contentsOf: root.appendingPathComponent("conformance/tables/\(name).json")))
    }
    private func patternDescriptor(_ pattern: String) throws -> Descriptor {
        try Descriptor(wire: .object(["types": .object(["Probe": .object([
            "kind": .string("alias"), "type": .object(["array": .object(["nullable": .object([
                "kind": .string("record"), "fields": .array([.object([
                    "name": .string("text"), "type": .string("string"), "required": .bool(true), "pattern": .string(pattern)
                ])])
            ])])])
        ])])]))
    }

    func testEverySharedValidatorRowAndDiagnostic() throws {
        let table = try table("validator")
        let descriptor = try Descriptor(wire: XCTUnwrap(table["wire"]), imported: table["imported"]?.object ?? [:])
        let rows = try XCTUnwrap(table["cases"]?.array)
        XCTAssertGreaterThan(rows.count, 150)
        for (index, row) in rows.enumerated() {
            var types: [String: JSONValue] = [:], families: [String: String] = [:]
            for (name, slot) in row["slots"]?.object ?? [:] {
                if let family = slot["family"]?.string { families[name] = family }
                else { types[name] = slot["type"] }
            }
            let bound = descriptor.bind(types: types, families: families)
            var failure: Error?
            do { try bound.validate(expression: XCTUnwrap(row["expression"]), value: XCTUnwrap(row["value"])) }
            catch { failure = error }
            XCTAssertEqual(failure == nil, row["valid"]?.bool, "row \(index): \(row.encoded()); got \(String(describing: failure))")
            if let message = row["message"]?.string { XCTAssertEqual(failure.map(String.init(describing:)), message, "row \(index)") }
        }
        XCTAssertEqual(try descriptor.fields("Payload"), ["text", "count", "note", "tag", "when"])
        XCTAssertThrowsError(try descriptor.validate(expression: .string("Status"), text: #""on" "off""#))
    }

    func testEverySharedPatternAndValue() throws {
        let table = try table("validator")
        for row in try XCTUnwrap(table["patterns"]?.array) {
            var valid = true
            do { _ = try patternDescriptor(XCTUnwrap(row["pattern"]?.string)) } catch { valid = false }
            XCTAssertEqual(valid, row["valid"]?.bool, row.encoded())
        }
        for row in try XCTUnwrap(table["patternValues"]?.array) {
            let descriptor = try patternDescriptor(XCTUnwrap(row["pattern"]?.string))
            var valid = true
            do { try descriptor.validate(expression: .string("Probe"), value: .array([.object(["text": try XCTUnwrap(row["value"])])])) }
            catch { valid = false }
            XCTAssertEqual(valid, row["valid"]?.bool, row.encoded())
        }
    }

    func testEverySharedGenericEquivalence() throws {
        let table = try table("validator")
        let descriptor = try Descriptor(wire: XCTUnwrap(table["wire"]), imported: table["imported"]?.object ?? [:])
        for row in try XCTUnwrap(table["equivalence"]?.array) {
            for value in row["values"]?.array ?? [] {
                let generic = Result { try descriptor.validate(expression: XCTUnwrap(row["generic"]), value: value) }
                let bound = Result { try descriptor.validate(expression: XCTUnwrap(row["bound"]), value: value) }
                if case .success = generic { if case .failure(let failure) = bound { XCTFail("\(row.encoded()) on \(value): \(failure)") } }
                else if case .success = bound { XCTFail("\(row.encoded()) on \(value): \(generic)") }
            }
        }
    }

    func testEverySharedFrameForItsReceivingRole() throws {
        let rows = try XCTUnwrap(table("frames")["rows"]?.array)
        XCTAssertGreaterThan(rows.count, 70)
        for row in rows {
            let roles = row["to"]?.string == "either" ? ["server", "client"] : [try XCTUnwrap(row["to"]?.string)]
            for role in roles {
                var valid = true
                do { _ = try validateFrame(text: XCTUnwrap(row["frame"]?.string), role: role) } catch { valid = false }
                XCTAssertEqual(valid, row["valid"]?.bool, "\(row["name"]?.string ?? "") to \(role)")
            }
        }
    }

    func testLosslessRawMembersAndUnicode() throws {
        let source = #"{ "payload" : {"a": 9007199254740993, "a":1e400,"emoji":"\uD83D\uDE00"}, "null":null }"#
        XCTAssertEqual(try rawMember("payload", in: source), #"{"a": 9007199254740993, "a":1e400,"emoji":"\uD83D\uDE00"}"#)
        XCTAssertEqual(try rawMember("null", in: source), "null")
        XCTAssertNil(try rawMember("absent", in: source))
        for text in [#""\uD800""#, #"{"x":"\uDFFF","x":"ok"}"#, #"{"\uD800":1}"#, "01", "1.", "1e", "[1,]", "{\"x\":1,}"] {
            XCTAssertThrowsError(try JSONValue(parsing: text), text)
        }
        let descriptor = try Descriptor(wire: .object(["types": .object([:])]))
        for token in ["9007199254740991", "90071992547409910e-1", "1.000e3", "0e99999999999999999999"] {
            try descriptor.validate(expression: .string("integer"), text: token)
        }
        for token in ["9007199254740991.1", "9007199254740992", "1.00000000000000001", "1e-999999999999999999"] {
            XCTAssertThrowsError(try descriptor.validate(expression: .string("integer"), text: token), token)
        }
        XCTAssertThrowsError(try descriptor.validate(expression: .object(["literal": .string("é")]), value: .string("e\u{301}")))
        let distinct = try JSONValue(parsing: #"{"é":1,"e\u0301":"bad"}"#)
        XCTAssertEqual(distinct.objectMembers?.count, 2)
        XCTAssertEqual(distinct["é"], .number("1"))
        XCTAssertEqual(distinct["e\u{301}"], .string("bad"))
        XCTAssertEqual(try JSONValue(parsing: distinct.encoded()), distinct)
        XCTAssertThrowsError(try descriptor.validate(expression: .object(["map": .string("integer")]), value: distinct))
        XCTAssertThrowsError(try validateFrame(text: #"{"version":1,"kind":"event","event":"test","data":null,"meta":{"é":1,"e\u0301":"ok"}}"#, role: "server"))
        let optional = try JSONValue(parsing: #"{"kind":"record","fields":[{"name":"é","type":"integer"}]}"#)
        XCTAssertThrowsError(try descriptor.validate(expression: optional, value: .object(["e\u{301}": .number("1")])))
    }
}
