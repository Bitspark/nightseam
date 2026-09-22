package io.nightseam.runtime;

import dev.bitspark.bitwire.*;

import dev.bitspark.bitwire.*;
import java.util.LinkedHashMap;
import java.util.Map;

/** Conversion at the Nightseam JSON profile boundary. Wire composition needs no conversion. */
public final class WireFrames {
    private WireFrames() {}
    public static Map<String,Object> fields(ProfileFrame frame) {
        var out = new LinkedHashMap<String,Object>();
        out.put("version", frame.version());
        out.put("kind", frame.kind().name().toLowerCase(java.util.Locale.ROOT));
        if (frame.traceparent() != null) out.put("traceparent", frame.traceparent());
        if (frame.tracestate() != null) out.put("tracestate", frame.tracestate());
        switch (frame) {
            case ProfileFrame.Request request -> {
                out.put("id", request.id()); out.put("params", Json.parse(request.params().json()));
                if (request.meta() != null) out.put("meta", request.meta());
            }
            case ProfileFrame.Event event -> {
                out.put("data", Json.parse(event.data().json()));
                if (event.meta() != null) out.put("meta", event.meta());
            }
            case ProfileFrame.Cancel cancel -> out.put("id", cancel.id());
            case ProfileFrame.Response response -> {
                out.put("id", response.id());
                if (response.result() != null) out.put("result", Json.parse(response.result().json()));
                else {
                    var error = new LinkedHashMap<String,Object>();
                    error.put("code", response.error().code()); error.put("message", response.error().message());
                    if (response.error().data() != null) error.put("data", Json.parse(response.error().data().json()));
                    out.put("error", error);
                }
            }
        }
        return out;
    }
    @SuppressWarnings("unchecked")
    public static ProfileFrame frame(Map<String,Object> fields) {
        String id = (String) fields.get("id"), parent = (String) fields.get("traceparent"), state = (String) fields.get("tracestate");
        Map<String,String> meta = (Map<String,String>) fields.get("meta");
        return switch ((String) fields.get("kind")) {
            case "request" -> new ProfileFrame.Request(id, payload(fields, "params"), meta, parent, state);
            case "event" -> new ProfileFrame.Event(payload(fields, "data"), meta, parent, state);
            case "cancel" -> new ProfileFrame.Cancel(id, parent, state);
            case "response" -> {
                ProfileError error = null;
                if (fields.containsKey("error")) {
                    var value = Json.object(fields.get("error"));
                    error = new ProfileError((String)value.get("code"), (String)value.get("message"),
                        value.containsKey("data") ? new JsonValue(Json.stringify(value.get("data"))) : null);
                }
                yield new ProfileFrame.Response(id, fields.containsKey("result") ? payload(fields, "result") : null, error, parent, state);
            }
            default -> throw new IllegalArgumentException("unknown wire frame kind");
        };
    }
    private static JsonValue payload(Map<String,Object> fields, String key) {
        if (!fields.containsKey(key)) throw new IllegalArgumentException("missing " + key);
        return new JsonValue(Json.stringify(fields.get(key)));
    }
}
