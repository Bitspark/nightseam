package io.nightseam.runtime;

import java.math.BigDecimal;
import java.util.HashSet;
import java.util.Map;
import java.util.Set;

/** The closed profile vocabulary, kept separate from schema validation. */
public final class Envelope {
    private Envelope() {}
    public static Map<String,Object> decode(byte[] encoded, String role) {
        var frame = Json.object(Json.parseEnvelope(encoded));
        if (!(frame.get("version") instanceof BigDecimal version) || !version.toString().equals("1")) invalid();
        var allowed = new HashSet<>(Set.of("version","kind","traceparent","tracestate"));
        String kind = text(frame,"kind");
        switch (kind) {
            case "request" -> {
                allowed.addAll(Set.of("id","method","params","meta"));
                text(frame,"method"); required(frame,"params");
            }
            case "response" -> {
                allowed.addAll(Set.of("id","result","error"));
                if (frame.containsKey("result") == frame.containsKey("error")) invalid();
                if (frame.containsKey("error")) {
                    var error = Json.object(frame.get("error"));
                    if (!Set.of("code","message","data").containsAll(error.keySet())) invalid();
                    text(error,"code"); text(error,"message");
                }
            }
            case "event" -> {
                allowed.addAll(Set.of("event","data","meta")); text(frame,"event"); required(frame,"data");
            }
            case "cancel" -> allowed.add("id");
            default -> invalid();
        }
        if (!allowed.containsAll(frame.keySet())) invalid();
        if (!kind.equals("event")) {
            String own = role.equals("client") ? "c:" : "s:";
            String prefix = kind.equals("response") ? own : own.equals("c:") ? "s:" : "c:";
            if (!text(frame,"id").matches(prefix+"[1-9][0-9]{0,19}")) invalid();
        }
        if (frame.containsKey("traceparent") && (!(frame.get("traceparent") instanceof String p) || !validTraceparent(p))) invalid();
        if (frame.containsKey("tracestate") && !(frame.get("tracestate") instanceof String)) invalid();
        if (frame.containsKey("meta")) {
            for (var entry : Json.object(frame.get("meta")).entrySet())
                if (entry.getKey().startsWith("nightseam.") || !(entry.getValue() instanceof String)) invalid();
        }
        return frame;
    }
    public static boolean validTraceparent(String value) {
        return value.matches("[0-9a-f]{2}-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}");
    }
    private static String text(Map<String,Object> value,String key) {
        if (!(value.get(key) instanceof String s) || s.isEmpty()) { invalid(); return ""; }
        return s;
    }
    private static void required(Map<String,Object> value,String key) { if (!value.containsKey(key)) invalid(); }
    private static void invalid() { throw new IllegalArgumentException("invalid duplex frame"); }
}
