# Haskell duplex

`nightseam-duplex` provides ordered, whole text and binary frames, a bounded
in-memory pipe, a WebSocket adapter, and relative-path Wire views. It requires
GHC 9.6 or later. The Stack project in `conformance/haskell` pins GHC 9.6.7
and its dependency snapshot.

`Connection` operations run in `IO` and may be bounded with
`System.Timeout.timeout`. `CloseError` carries the close code and reason.
The pipe holds eight frames in each direction and paces further sends until
the receiver drains. WebSocket subprotocol negotiation is explicit and defaults
to offering and selecting none.

Import shared contracts from `Bitwire` (`bitspark-bitwire-0.2.0`). `at`
selects send-only access from an existing `Wire`; `mount` borrows `Endpoint`
children. `Nightseam.Duplex.Dispatcher` provides explicit path registration
and receiving selections above one endpoint attachment. Paths are lists of opaque
Unicode scalar strings; `encodePath` and `decodePath` implement canonical
UTF-8 byte-length encoding. Views create no transport, queue, or peer.
Closing a mount detaches its registrations and preserves its borrowed children.

From the repository root:

```sh
stack --stack-yaml conformance/haskell/stack.yaml test nightseam-duplex
```
