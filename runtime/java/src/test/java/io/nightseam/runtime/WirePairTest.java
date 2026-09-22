package io.nightseam.runtime;

import static io.nightseam.runtime.WireFrames.*;

import dev.bitspark.bitwire.*;

import java.time.Duration;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

/** Native concurrency tests use barriers, never scheduling sleeps. */
public final class WirePairTest {
    public static void main(String[] args) {
        routesAndSnapshot();
        cancelReservationAndCapturedReceiver();
        completedReservationCannotBeReusedEarly();
        deadlineRetainsActiveBudget();
        boundedDataAndStalledEvent();
        responseValidationAndFailureRetire();
        forwardingIdentityOrderAndBorrowedOwnership();
    }

    private static PeerOptions options(int queue, int pending, int active, Duration timeout, Duration write) {
        return new PeerOptions(active, pending, queue, 4096, timeout, write, Map.of(), null);
    }
    private static PeerOptions options(int queue, int pending, int active) {
        return options(queue, pending, active, Duration.ofSeconds(5), Duration.ofSeconds(5));
    }
    private static void check(boolean condition, String detail) {
        if (!condition) throw new AssertionError(detail);
    }
    private static <T> T take(BlockingQueue<T> queue) {
        try {
            T result = queue.poll(3, TimeUnit.SECONDS);
            if (result == null) throw new AssertionError("wire barrier did not arrive");
            return result;
        } catch (InterruptedException interrupted) { throw new AssertionError(interrupted); }
    }
    private static void await(CountDownLatch latch) {
        try { check(latch.await(3, TimeUnit.SECONDS), "wire barrier did not arrive"); }
        catch (InterruptedException interrupted) { throw new AssertionError(interrupted); }
    }
    private static void fails(Runnable action) {
        boolean failed = false;
        try { action.run(); } catch (IllegalArgumentException | IllegalStateException expected) { failed = true; }
        check(failed, "operation should have refused");
    }
    private static Message request(String id, Sink returning) {
        var frame = new LinkedHashMap<String,Object>();
        frame.put("version", 1); frame.put("kind", "request"); frame.put("id", id); frame.put("params", null);
        return new Message(frame(frame), returning == null ? null : returning.address);
    }
    private static Message event(int value) {
        return new Message(frame(Map.of("version", 1, "kind", "event", "data", value)), null);
    }
    private static Message cancel(String id, Sink returning) {
        return new Message(frame(Map.of("version", 1, "kind", "cancel", "id", id)), returning == null ? null : returning.address);
    }
    private static Message response(Message request, Object result) {
        var frame = new LinkedHashMap<String,Object>();
        frame.put("version", 1); frame.put("kind", "response"); frame.put("id", fields(request.frame()).get("id")); frame.put("result", result);
        return new Message(frame(frame), null);
    }
    private static void answer(Message message, Object result) {
        message.returnAddress().wire().send(List.of(), response(message, result));
    }
    private static String error(Message message) {
        return (String)((Map<?,?>)fields(message.frame()).get("error")).get("code");
    }

    private static class Sink implements Wire {
        final ReturnAddress address = new ReturnAddress(this);
        final BlockingQueue<Message> messages = new ArrayBlockingQueue<>(16);
        @Override public void send(List<String> path, Message message) { check(path.isEmpty(), "return path"); messages.add(message); }
        // Intentionally value-equal sinks must still own distinct call identities.
        @Override public boolean equals(Object other) { return other instanceof Sink; }
        @Override public int hashCode() { return 1; }
    }

    private static void routesAndSnapshot() {
        try (var pair = WirePair.create()) {
            Thread sender = Thread.currentThread();
            var delivered = new ArrayBlockingQueue<String>(8);
            for (var path : List.of(List.<String>of(), List.of("a"))) {
                String label = path.isEmpty() ? "root" : "a";
                Routes.of(pair.right()).registerPrefix(path, new Receiver( (actual, message) -> {
                    check(Thread.currentThread() != sender, "application ran on sender stack");
                    check(!actual.isEmpty(), "root-relative callback path lost");
                    delivered.add(label);
                }, null));
            }
            Runnable detach = Routes.of(pair.right()).register(List.of("a", "b"), new Receiver( (path, message) -> delivered.add("exact"), null));
            fails(() -> Routes.of(pair.right()).register(List.of("a", "b"), new Receiver( (path, message) -> {}, null)));
            pair.left().send(List.of("a", "b"), event(1)); check(take(delivered).equals("exact"), "exact route");
            detach.run(); detach.run();
            pair.left().send(List.of("a", "b"), event(1)); check(take(delivered).equals("a"), "longest prefix");
            pair.left().send(List.of("other"), event(1)); check(take(delivered).equals("root"), "root namespace");
        }
        try (var pair = WirePair.create()) {
            var started = new CountDownLatch(1); var release = new CountDownLatch(1);
            var received = new ArrayBlockingQueue<Message>(4);
            Routes.of(pair.right()).register(List.of("hold"), new Receiver( (path, message) -> { started.countDown(); await(release); }, null));
            Routes.of(pair.right()).register(List.of("snapshot"), new Receiver( (path, message) -> received.add(message), null));
            pair.left().send(List.of("hold"), event(1)); await(started);
            try {
                var payload = new LinkedHashMap<String,Object>(); payload.put("null", null); payload.put("kept", "before");
                var frame = new LinkedHashMap<String,Object>(); frame.put("version", 1); frame.put("kind", "event"); frame.put("data", payload);
                pair.left().send(List.of("snapshot"), new Message(frame(frame), null));
                payload.put("kept", "after"); payload.put("absent", 1);
            } finally { release.countDown(); }
            var payload = (Map<?,?>)fields(take(received).frame()).get("data");
            check(payload.containsKey("null") && payload.get("null") == null && !payload.containsKey("absent")
                && payload.get("kept").equals("before"), "mutable data or null/absence changed after admission");
        }
    }

    private static void cancelReservationAndCapturedReceiver() {
        try (var pair = WirePair.create(options(1, 2, 2))) {
            var received = new ArrayBlockingQueue<Message>(8);
            var order = new ArrayBlockingQueue<String>(8);
            Runnable detach = Routes.of(pair.right()).register(List.of("call"), new Receiver( (path, message) -> {
                received.add(message); order.add((String)fields(message.frame()).get("kind"));
            }, null));
            var original = new Sink();
            pair.left().send(List.of("call"), request("c:1", original));
            Message accepted = take(received); check(take(order).equals("request"), "request order");
            check(accepted.returnAddress() != original.address, "root must map return capability");
            var started = new CountDownLatch(1); var release = new CountDownLatch(1);
            Routes.of(pair.right()).register(List.of("events"), new Receiver( (path, message) -> {
                int n = ((Number)fields(message.frame()).get("data")).intValue();
                if (n == 1) { started.countDown(); await(release); }
                order.add("event" + n);
            }, null));
            pair.left().send(List.of("events"), event(1)); await(started);
            try {
                pair.left().send(List.of("events"), event(2));
                pair.left().send(List.of("call"), cancel("c:1", original));
                for (int i = 0; i < 4; i++) {
                    pair.left().send(List.of("call"), cancel("c:1", original));
                    pair.left().send(List.of("call"), cancel("c:99", original));
                }
                detach.run();
                Routes.of(pair.right()).register(List.of("call"), new Receiver( (path, message) -> { throw new AssertionError("redirected cancel"); }, null));
            } finally { release.countDown(); }
            check(take(order).equals("event1") && take(order).equals("event2") && take(order).equals("cancel"), "cancel lost FIFO");
            Message cancelled = take(received);
            check(cancelled.returnAddress() == accepted.returnAddress(), "cancel changed return identity");
            answer(accepted, null); check(fields(take(original.messages).frame()).containsKey("result"), "null result was omitted");
            check(received.isEmpty(), "duplicate cancellation delivered");
        }
    }

    private static void completedReservationCannotBeReusedEarly() {
        try (var pair = WirePair.create(options(1, 1, 1))) {
            var delivered = new ArrayBlockingQueue<Message>(4);
            Routes.of(pair.right()).register(List.of("call"), new Receiver( (path, message) -> delivered.add(message), null));
            var first = new Sink(); var second = new Sink();
            pair.left().send(List.of("call"), request("c:1", first));
            Message accepted = take(delivered);
            var started = new CountDownLatch(1); var release = new CountDownLatch(1);
            Routes.of(pair.right()).register(List.of("hold"), new Receiver( (path, message) -> { started.countDown(); await(release); }, null));
            pair.left().send(List.of("hold"), event(1)); await(started);
            try {
                pair.left().send(List.of("call"), cancel("c:1", first));
                answer(accepted, 7);
                check(((Number)fields(take(first.messages).frame()).get("result")).intValue() == 7, "first reply");
                pair.left().send(List.of("call"), request("c:2", second));
            } finally { release.countDown(); }
            check(error(take(second.messages)).equals("busy"), "queued cancellation reservation was replenished early");
            pair.left().send(List.of("call"), request("c:1", first));
            Message reused = take(delivered);
            check(fields(reused.frame()).get("kind").equals("request"), "completed cancellation reached receiver");
            answer(reused, 9); take(first.messages);
        }
        try (var pair = WirePair.create(options(4, 2, 2))) {
            var delivered = new ArrayBlockingQueue<Message>(4);
            Routes.of(pair.right()).register(List.of("call"), new Receiver( (path, message) -> delivered.add(message), null));
            var first = new Sink(); var equalButDifferent = new Sink();
            pair.left().send(List.of("call"), request("c:1", first));
            pair.left().send(List.of("call"), request("c:1", equalButDifferent));
            answer(take(delivered), 1); answer(take(delivered), 2);
            take(first.messages); take(equalButDifferent.messages);
        }
    }

    private static void deadlineRetainsActiveBudget() {
        try (var pair = WirePair.create(options(8, 4, 1, Duration.ofMillis(30), Duration.ofSeconds(5)))) {
            var delivered = new ArrayBlockingQueue<Message>(8);
            Routes.of(pair.right()).register(List.of("call"), new Receiver( (path, message) -> delivered.add(message), null));
            var first = new Sink(); var second = new Sink();
            pair.left().send(List.of("call"), request("c:1", first)); Message accepted = take(delivered);
            check(error(take(first.messages)).equals("cancelled"), "deadline response");
            check(fields(take(delivered).frame()).get("kind").equals("cancel"), "deadline cancellation");
            pair.left().send(List.of("call"), request("c:2", second));
            check(error(take(second.messages)).equals("busy"), "deadline released active work budget");
            fails(() -> answer(accepted, 1));
            pair.left().send(List.of("call"), request("c:3", second));
            Message next = take(delivered); answer(next, 3);
            check(((Number)fields(take(second.messages).frame()).get("result")).intValue() == 3, "actual completion did not release budget");
            check(first.messages.isEmpty(), "deadline answered twice");
        }
    }

    private static void boundedDataAndStalledEvent() {
        for (boolean overflow : List.of(true, false)) {
            var observed = new ArrayBlockingQueue<Map<String,Object>>(8);
            var configured = options(1, 1, 1, Duration.ofSeconds(5),
                overflow ? Duration.ofSeconds(5) : Duration.ofMillis(30));
            try (var pair = WirePair.create(new PeerOptions(configured.maxConcurrentHandlers(),
                    configured.maxPendingRequests(), configured.queueCapacity(), configured.maxFrameBytes(),
                    configured.requestTimeout(), configured.writeTimeout(), configured.families(), observed::add))) {
                var started = new CountDownLatch(1); var release = new CountDownLatch(1);
                var closed = new ArrayBlockingQueue<Integer>(4);
                Routes.of(pair.right()).register(List.of("hold"), new Receiver( (path, message) -> { started.countDown(); await(release); },
                    (code, reason) -> closed.add(code)));
                pair.left().send(List.of("hold"), event(1)); await(started);
                try {
                    if (overflow) {
                        pair.left().send(List.of("hold"), event(2));
                        fails(() -> pair.left().send(List.of("hold"), event(3)));
                    }
                    check(take(closed) == 4011, "stalled/overflow carrier close code");
                    var pressure = take(observed);
                    check(pressure.get("type").equals("backpressure") && pressure.get("stalled").equals(true)
                        && pressure.get("deadline").equals(configured.writeTimeout().toNanos()), "missing backpressure observation");
                    check(take(observed).get("type").equals("connection.closed"), "pressure must precede closure");
                    fails(() -> pair.left().send(List.of("hold"), event(4)));
                } finally { release.countDown(); }
            }
        }
    }

    private static void responseValidationAndFailureRetire() {
        try (var pair = WirePair.create(options(8, 1, 1))) {
            var delivered = new ArrayBlockingQueue<Message>(4);
            Routes.of(pair.right()).register(List.of("call"), new Receiver( (path, message) -> delivered.add(message), null));
            var sink = new Sink();
            pair.left().send(List.of("call"), request("c:1", sink)); Message accepted = take(delivered);
            fails(() -> answer(accepted, "x".repeat(5000)));
            answer(accepted, 1); take(sink.messages);
            Sink failed = new Sink() { @Override public void send(List<String> path, Message message) { throw new IllegalStateException("return failed"); } };
            pair.left().send(List.of("call"), request("c:2", failed)); Message withFailure = take(delivered);
            fails(() -> answer(withFailure, 2));
            pair.left().send(List.of("call"), request("c:3", sink));
            answer(take(delivered), 3); take(sink.messages);
            var malformed = new LinkedHashMap<>(fields(request("c:4", sink).frame())); malformed.remove("params");
            fails(() -> pair.left().send(List.of("call"), new Message(frame(malformed), sink.address)));
            fails(() -> pair.left().send(List.of("call"), request("c:01", sink)));
            fails(() -> pair.left().send(List.of("call"), response(accepted, 1)));
        }
    }

    private static void forwardingIdentityOrderAndBorrowedOwnership() {
        final class RecordingWire implements Endpoint {
            Receiver receiver;
            final List<Message> sent = new ArrayList<>();
            @Override public void send(List<String> path, Message message) { check(path.equals(List.of("a", "b")), "forward changed path"); sent.add(message); }
                    @Override public Runnable receive(Receiver value) {
                if (receiver != null) throw new IllegalStateException("duplicate");
                receiver = value;
                return () -> receiver = null;
            }
            @Override public void close(int code, String reason) { throw new AssertionError("forward closed borrowed root"); }
        }
        var left = new RecordingWire(); var right = new RecordingWire(); var sink = new Sink();
        Runnable detach = Wires.forward(left, right);
        Message first = request("c:1", sink); Message second = cancel("c:1", sink);
        left.receiver.message().accept(List.of("a", "b"), first);
        left.receiver.message().accept(List.of("a", "b"), second);
        check(right.sent.get(0) == first && right.sent.get(1) == second
            && right.sent.get(0).returnAddress() == sink.address, "forward changed order/message/return identity");
        detach.run(); detach.run(); check(left.receiver == null && right.receiver == null, "forward detach");
        try (var a = WirePair.create(); var b = WirePair.create()) {
            Runnable stop = Wires.forward(a.right(), b.left());
            var received = new ArrayBlockingQueue<Message>(4);
            Routes.of(b.right()).register(List.of("operation"), new Receiver( (path, message) -> received.add(message), null));
            a.left().send(List.of("operation"), request("c:1", sink));
            answer(take(received), 1); take(sink.messages);
            stop.run();
            var seen = new ArrayBlockingQueue<Integer>(2);
            Routes.of(a.right()).register(List.of("still-open"), new Receiver( (path, message) -> seen.add(1), null));
            a.left().send(List.of("still-open"), event(1)); check(take(seen) == 1, "forward detach closed carrier");
        }
    }
}
