package io.nightseam.duplex;

/** The WebSocket registry's close code and reason, on every transport. */
public record CloseInfo(int code, String reason) {
    public CloseInfo { reason = reason == null ? "" : reason; }
}
