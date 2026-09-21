package io.nightseam.duplex;

import java.util.Map;
import java.util.Objects;

/** A profile frame and its local, identity-preserving return capability. */
public record Message(Map<String, Object> frame, Wire returnAddress) {
    public Message { Objects.requireNonNull(frame, "frame"); }
}
