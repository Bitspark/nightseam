import Foundation
import Testing
import NightseamDuplex
@testable import NightseamRuntime

@Suite struct SerialTests {
    @Test func completedRequestSerialsFollowTheSharedTable() async throws {
        var root = URL(fileURLWithPath: #filePath)
        for _ in 0..<6 { root.deleteLastPathComponent() }
        let table = try JSONValue(data: Data(contentsOf: root.appendingPathComponent("conformance/tables/serials.json")))
        for row in table["rows"]!.array! {
            let (raw, transport) = Pipe.pair()
            let peer = try Peer(connection: transport, role: "server")
            try await peer.handle(method: "echo") { _, _, data in data }
            await peer.start()
            do {
                let before = row["before"]!.string!
                try await raw.send(Frame(kind: .text, data: Data(before.utf8)))
                if try JSONValue(parsing: before)["kind"]?.string == "request" {
                    _ = try await withTimeout(milliseconds: 1_000) { try await raw.receive() }
                }
                try await raw.send(Frame(kind: .text, data: Data(row["frame"]!.string!.utf8)))
                if row["valid"]?.bool == true {
                    _ = try await withTimeout(milliseconds: 1_000) { try await raw.receive() }
                } else {
                    let close = try await withTimeout(milliseconds: 1_000) { try await peer.awaitClose() }
                    #expect(close.code == 4011)
                }
            } catch { await peer.close(); throw error }
            await peer.close()
        }
    }

    @Test func concurrentRequestsCannotOvertakeEarlierReservations() async throws {
        let (raw, transport) = Pipe.pair(capacity: 1)
        var options = PeerOptions(); options.queueCapacity = 1
        let peer = try Peer(connection: transport, role: "client", options: options)
        await peer.start()
        let calls = (0..<24).map { _ in Task { try await peer.call(method: "probe", params: nil) } }
        do {
            try await Task.sleep(for: .milliseconds(30))
            var previous: UInt64 = 0
            for _ in calls {
                let frame = try await withTimeout(milliseconds: 1_000) { try await raw.receive() }
                let value = try JSONValue(data: frame.data)
                let serial = UInt64(value["id"]!.string!.dropFirst(2))!
                #expect(serial > previous)
                previous = serial
            }
        } catch {
            await peer.close()
            for call in calls { _ = try? await call.value }
            throw error
        }
        await peer.close()
        for call in calls { _ = try? await call.value }
    }
}
