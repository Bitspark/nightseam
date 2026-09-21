package io.nightseam.runtime;

import java.util.LinkedHashMap;
import java.util.Map;

/** An explicitly public refusal; other handler failures are never disclosed. */
public final class PublicError extends RuntimeException {
    private static final long serialVersionUID=1L;
    private final String code;
    private final Object data;
    private final boolean hasData;

    public PublicError(String code, String message) { this(code, message, null, false); }
    public PublicError(String code, String message, Object data) { this(code, message, data, true); }
    private PublicError(String code, String message, Object data, boolean hasData) {
        super(message);
        if (code == null || code.isEmpty() || message == null || message.isEmpty())
            throw new IllegalArgumentException("a public error requires a code and message");
        this.code = code; this.data = data; this.hasData = hasData;
    }
    public String code() { return code; }
    public Object data() { return data; }
    public boolean hasData() { return hasData; }
    public Map<String,Object> value() {
        var result = new LinkedHashMap<String,Object>();
        result.put("code", code); result.put("message", getMessage());
        if (hasData) result.put("data", data);
        return result;
    }
    public static PublicError from(Map<String,Object> value) {
        return value.containsKey("data") ? new PublicError((String)value.get("code"), (String)value.get("message"), value.get("data"))
            : new PublicError((String)value.get("code"), (String)value.get("message"));
    }
}
