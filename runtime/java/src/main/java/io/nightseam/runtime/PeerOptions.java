package io.nightseam.runtime;

import java.time.Duration;
import java.util.Map;
import java.util.function.Consumer;

/** Per-connection resource bounds, with the same defaults as the reference peer. */
public record PeerOptions(int maxConcurrentHandlers, int maxPendingRequests, int queueCapacity,
    int maxFrameBytes, Duration requestTimeout, Duration writeTimeout,
    Map<String,String> families, Consumer<Map<String,Object>> observer) {
    public PeerOptions {
        if (maxConcurrentHandlers < 1 || maxPendingRequests < 1 || queueCapacity < 1 || maxFrameBytes < 1
            || requestTimeout.isNegative() || requestTimeout.isZero() || writeTimeout.isNegative() || writeTimeout.isZero())
            throw new IllegalArgumentException("peer bounds must be positive");
        families = Map.copyOf(families);
    }
    public static PeerOptions defaults() {
        return new PeerOptions(64,128,128,1<<20,Duration.ofSeconds(30),Duration.ofSeconds(10),Map.of(),null);
    }
}
