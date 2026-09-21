# Java duplex seam

`io.nightseam.duplex` supplies a Java 21 ordered, framed, bidirectional
transport and relative-path Wire views. It has no third-party dependencies
and no dependency on the JSON runtime.

```java
Connection[] pair = Pipe.pair(1 << 20, 8);
pair[0].send(new Frame("text", "hello".getBytes(StandardCharsets.UTF_8)),
             Duration.ofSeconds(1));
Frame received = pair[1].receive(Duration.ofSeconds(1));
pair[0].close(1000, "finished");
```

`Frame` owns its bytes. `Connection.send` and `receive` take a timeout;
`null` means no deadline. A timed-out pipe send is not queued. Socket sends
that time out abort the connection because a partially published frame
cannot be retried on the same stream. Calls fail with `CloseException`
after closure; its `code()` and `reason()` carry the transport ending.
`closed()` completes once when an ending is known. Remote closure leaves
preceding queued frames readable; local close or abort releases blocked
operations immediately. Abort has code 1006. A frame over the receive
limit ends the receiver with code 1009 before delivering any of that frame.
A nonpositive byte limit disables the receive limit.

`Pipe.pair(byteLimit, capacity)` holds at most `capacity` messages in each
direction. `WebSocketTransport.dial(uri, byteLimit, protocols)` opens a
`ws` or `wss` connection. TLS uses the JDK's trust configuration and HTTPS
hostname verification. `WebSocketTransport.listen(byteLimit, protocols)`
returns a loopback listener with `uri()` and `accept(timeout)`; closing the
listener leaves previously accepted connections under their caller's ownership.
Subprotocols are optional RFC tokens, independent of the profile name.

Both WebSocket roles use native RFC 6455 framing over JDK sockets. Java's
[`java.net.http.WebSocket.sendClose`](https://docs.oracle.com/en/java/javase/21/docs/api/java.net.http/java/net/http/WebSocket.html#sendClose(int,java.lang.String))
forbids application sends of codes including 1003 and 1009, which the seam
must preserve. The native adapter validates upgrade headers, masking,
control frames, continuation order, canonical lengths and UTF-8, bounds
aggregate fragmented messages before allocation, and serializes writes.
Its incoming queue holds eight complete messages plus at most one message
being read. Listener handshakes and pending accepts are also bounded.
No WebSocket extension is negotiated.

`Message` carries a profile frame map and a local `Wire returnAddress`.
The map preserves JSON key presence and explicit null; the runtime owns its
value representation. Path composition preserves the message and return
capability identities. `Wires.at` selects an origin, `Wires.mount` consumes
one child segment, and `encodePath`/`decodePath` use canonical UTF-8 byte
lengths. Views allocate no queue and run no application callbacks on send.
Closing a selection closes its endpoint; closing a mount detaches its
registrations and leaves its borrowed children usable.

The public `io.nightseam.duplex.SeamTest` main tests both transport roles,
bounded writes, code/reason propagation, abort, raw protocol refusals,
JDK client interoperability, and relative-path composition.
