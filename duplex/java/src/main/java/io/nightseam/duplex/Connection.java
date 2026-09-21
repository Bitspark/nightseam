package io.nightseam.duplex;

import java.io.IOException;
import java.time.Duration;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.TimeoutException;

/** A bounded, ordered framed duplex transport, independent of the JSON profile. */
public interface Connection {
    /** Waits for transport admission within timeout; never grows an unbounded queue.
     * A remote ending during a socket write may fail with IOException. */
    void send(Frame frame, Duration timeout) throws IOException, InterruptedException, TimeoutException;
    Frame receive(Duration timeout) throws IOException, InterruptedException, TimeoutException;
    void close(int code, String reason);
    void abort();
    CompletableFuture<CloseInfo> closed();
    default String subprotocol() { return ""; }
}
