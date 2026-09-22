package io.nightseam.runtime;

import static io.nightseam.runtime.WireFrames.*;

import dev.bitspark.bitwire.*;

import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Objects;

/** Runtime composition over existing origins; views do not allocate peers. */
public final class Wires {
    private Wires() {}

    /** Join both directions, preserving the message's opaque return capability. */
    public static Runnable forward(Endpoint inbound, Endpoint outbound) {
        Objects.requireNonNull(inbound, "inbound");
        Objects.requireNonNull(outbound, "outbound");
        final class Forwarding implements Runnable {
            private final List<Runnable> detaches = new ArrayList<>();
            private boolean ended;

            @Override public void run() {
                List<Runnable> held;
                synchronized (this) {
                    if (ended) return;
                    ended = true;
                    held = List.copyOf(detaches);
                    detaches.clear();
                }
                held.forEach(Runnable::run);
            }

            void attach(Endpoint source, Wire destination) {
                Runnable detach = source.receive(new Receiver( (path, message) -> {
                    try {
                        destination.send(path, message);
                    } catch (RuntimeException failure) {
                        run();
                        if ("request".equals(fields(message.frame()).get("kind"))) {
                            refuse(message, "disconnected", "Connection ended; outcome may be unknown");
                        }
                    }
                }, (code, reason) -> run()));
                synchronized (this) {
                    if (!ended) {
                        detaches.add(detach);
                        return;
                    }
                }
                detach.run();
                throw new IllegalStateException("wire forwarding ended during registration");
            }
        }
        var forwarding = new Forwarding();
        try {
            forwarding.attach(inbound, outbound);
            forwarding.attach(outbound, inbound);
        } catch (RuntimeException failure) {
            forwarding.run();
            throw failure;
        }
        return forwarding;
    }

    // Validate with exactly the physical profile decoder. Encoding and decoding
    // also snapshot mutable payloads without losing null/absence or decimals.
    static Map<String,Object> validateFrame(List<String> path, ProfileFrame value, int limit) {
        Map<String,Object> frame = fields(value);
        String name = io.nightseam.duplex.Wires.encodePath(path);
        var physical = new LinkedHashMap<String,Object>(frame);
        String kind = Objects.toString(frame.get("kind"), "");
        if (frame.containsKey("method") || frame.containsKey("event"))
            throw new IllegalArgumentException("wire paths are separate from frame values");
        if (kind.equals("request")) physical.put("method", name);
        if (kind.equals("event")) physical.put("event", name);
        byte[] encoded = Json.stringify(physical).getBytes(StandardCharsets.UTF_8);
        if (encoded.length > limit) throw new IllegalArgumentException("wire frame exceeds carrier limit");
        boolean clientId = Objects.toString(frame.get("id"), "").startsWith("c:");
        String role = clientId == kind.equals("response") ? "client" : "server";
        var validated = new LinkedHashMap<>(Envelope.decode(encoded, role));
        validated.remove("method");
        validated.remove("event");
        return validated;
    }

    static void refuse(Message request, String code, String detail) {
        if (request.returnAddress() == null) return;
        var frame = new LinkedHashMap<String,Object>();
        frame.put("version", 1);
        frame.put("kind", "response");
        frame.put("id", fields(request.frame()).get("id"));
        frame.put("error", Map.of("code", code, "message", detail));
        for (String field : List.of("traceparent", "tracestate")) {
            if (fields(request.frame()).containsKey(field)) frame.put(field, fields(request.frame()).get(field));
        }
        try {
            request.returnAddress().wire().send(List.of(), new Message(frame(frame), null));
        } catch (Throwable ignored) {
            // A withdrawn caller cannot receive a terminal response.
        }
    }
}
