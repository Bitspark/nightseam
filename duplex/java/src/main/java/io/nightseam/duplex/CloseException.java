package io.nightseam.duplex;

import java.io.IOException;

/** A transport ending with its explicit code, or 1006 for an abrupt ending. */
public final class CloseException extends IOException {
    private static final long serialVersionUID = 1L;
    private final int code;
    private final String reason;

    public CloseException(int code, String reason) {
        super("duplex connection closed (" + code + "): " + (reason == null ? "" : reason));
        this.code = code;
        this.reason = reason == null ? "" : reason;
    }

    public int code() { return code; }
    public String reason() { return reason; }
}
