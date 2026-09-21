package io.nightseam.duplex;

import java.util.List;

/** Relative access to an origin; the root owns asynchronous dispatch and bounds. */
public interface Wire {
    void send(List<String> path, Message message);
    Runnable receive(List<String> path, Receiver receiver);
    void close(int code, String reason);
}
