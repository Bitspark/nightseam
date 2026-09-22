# Swift duplex

`NightseamDuplex` is a Swift 6 package with strict concurrency checking. Its
package directory is `NightseamDuplex/`. It supports Linux and Apple platforms
(macOS 13, iOS/tvOS 16, watchOS 9 or later).

```swift
import Foundation
import NightseamDuplex

let (near, far) = Pipe.pair(limit: 65_536)
try await near.send(Frame(kind: .text, data: Data("hello".utf8)))
let frame = try await far.receive()
try await near.close(code: 1000, reason: "done")
```

`Connection` is `Sendable` and has asynchronous `send`, `receive`, `close`, and
`abort`. One sender and one receiver may run concurrently. The pipe holds eight
frames per direction by default; further sends suspend. Cancelling a blocked
pipe operation removes it without closing the pair. A remote close becomes a
`CloseError` with its code and reason; abort is code 1006.

`WebSocket.listen` returns a listener exposing `url`, `port`, asynchronous
`accept`, and synchronous `close`. `WebSocket.dial(url:limit:subprotocols:)`
connects to it or another RFC 6455 peer. Both directions carry text and binary
messages, negotiate subprotocols, and enforce receive limits before delivering
an oversized message. The adapter uses native TCP sockets on Linux and Apple
platforms, accepts `ws` URLs, and rejects `wss`; the host supplies TLS termination.
Cancelling an active socket read or write closes that connection. Socket close
waits for the remote acknowledgement, bounded to three seconds; abort immediately
releases pending reads and writes.

Import shared access types and `Metadata` from the upstream `Bitwire` SwiftPM
module, version 0.2.0. `Wire` is the synchronous send admission interface. `Message` carries a profile frame
and an optional local `ReturnAddress`; JSON payload fields use `Data?` so absent
and explicit null remain distinct. `at` and `mount` create routing views.
`at` grants send access only. `Dispatcher` owns one Bitwire `Endpoint`
attachment and provides receiving selections. Closing a selection or mount
releases attachments and leaves borrowed children usable. `Path.key` returns UTF-8
bytes, preserving distinct Unicode scalar spellings that Swift `String` equality
would otherwise consider equal. Use the tuple-array overload of `mount` when
children have canonically equivalent but distinct keys.

Run the package tests with:

```sh
swift test --package-path duplex/swift/NightseamDuplex
```
