package io.nightseam.duplex;

import java.util.Objects;

/** One ordered, whole transport message. Bytes are owned by this frame. */
public record Frame(String kind, byte[] data) {
    public Frame {
        if (!"text".equals(kind) && !"binary".equals(kind)) {
            throw new IllegalArgumentException("duplex frame of no kind");
        }
        data = Objects.requireNonNull(data, "data").clone();
    }

    @Override public byte[] data() { return data.clone(); }
}
