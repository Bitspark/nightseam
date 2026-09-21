package io.nightseam.runtime;

import io.nightseam.duplex.CloseException;
import io.nightseam.duplex.CloseInfo;
import io.nightseam.duplex.Connection;
import io.nightseam.duplex.Frame;
import io.nightseam.duplex.Pipe;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.function.Consumer;
import java.util.function.Predicate;

/** Regressions for public peer admission, cancellation, closing and observer isolation. */
public final class PeerLifecycleTest {
    private static final Duration BOUND = Duration.ofSeconds(6);
    private static final Duration WRITE_TIMEOUT = Duration.ofSeconds(12);
    private static int assertions;

    private PeerLifecycleTest() {}

    public static void main(String[] args) throws Exception {
        cancelledBeforeAdmission();
        parentCancellationReachesRemote();
        cancelledDuringAdmission();
        deadlineDuringAdmission();
        oneDeadlineIncludesAdmission();
        closeReleasesBlockedProducers();
        observersCannotBreakTraffic();
        metadataIsASnapshot();
        observersReceiveMeasurements();
        System.out.println("PeerLifecycleTest: cancellation, absolute deadlines, close, observer isolation, metadata and measurements; "
            + assertions + " assertions passed");
    }

    private static PeerOptions options(Consumer<Map<String, Object>> observer) {
        return new PeerOptions(8, 16, 1, 1 << 20, Duration.ofSeconds(20), WRITE_TIMEOUT, Map.of(), observer);
    }

    private static void cancelledBeforeAdmission() throws Exception {
        ControlledConnection transport = new ControlledConnection();
        transport.release();
        Recorder recorder = new Recorder();
        try (Peer peer = new Peer(transport, "client", options(recorder))) {
            RequestContext cancelled = RequestContext.empty();
            cancelled.cancel();
            expectError(peer.call(cancelled, "must-not-run", null, null, null).result(), "cancelled");
            expectError(async(() -> {
                peer.emit(cancelled, "must-not-arrive", null, null, WRITE_TIMEOUT);
                return null;
            }), "cancelled");
            peer.emit("barrier", null);
            transport.sent(frame -> "barrier".equals(frame.get("event")));
            check(transport.history().size() == 1, "a precancelled operation reached the transport");
            check(recorder.snapshot().stream().noneMatch(e -> "request.started".equals(e.get("type"))),
                "a precancelled call started an observed request");
        }
    }

    private static void parentCancellationReachesRemote() throws Exception {
        Connection[] pair = Pipe.pair(1 << 20, 8);
        try (Peer server = new Peer(pair[0], "server", options(null));
             Peer client = new Peer(pair[1], "client", options(null))) {
            CompletableFuture<RequestContext> started = new CompletableFuture<>();
            server.handle("wait", (context, params) -> {
                started.complete(context);
                context.cancellation().get();
                return null;
            });
            server.handle("echo", (context, params) -> params);
            RequestContext parent = RequestContext.empty();
            Peer.Call call = client.call(parent, "wait", null, null, null);
            RequestContext remote = await(started);
            parent.cancel();
            expectError(call.result(), "cancelled");
            await(remote.cancellation());
            check(remote.isCancelled(), "the parent cancellation never reached the remote handler");
            check("still-open".equals(await(client.call("echo", "still-open").result())),
                "withdrawing a call closed its carrier");
        }
    }

    private static void cancelledDuringAdmission() throws Exception {
        for (boolean request : List.of(false, true)) {
            ControlledConnection transport = new ControlledConnection();
            Recorder recorder = new Recorder();
            try (Peer peer = new Peer(transport, "client", options(recorder))) {
                fillOutput(peer, transport);
                RequestContext context = RequestContext.empty();
                CompletableFuture<Object> producer = async(() -> {
                    if (request) return await(peer.call(context, "withdrawn", null, null, null).result());
                    peer.emit(context, "withdrawn", null, null, WRITE_TIMEOUT);
                    return null;
                });
                recorder.first("backpressure");
                context.cancel();
                expectError(producer, "cancelled");
                check(!peer.closed().isDone(), "cancelling a blocked producer closed the carrier");
                transport.release();
                peer.emit("barrier", null);
                transport.sent(frame -> "barrier".equals(frame.get("event")));
                check(transport.history().stream().map(PeerLifecycleTest::decode)
                    .noneMatch(frame -> "withdrawn".equals(frame.get("method")) || "withdrawn".equals(frame.get("event"))),
                    "a withdrawn unadmitted frame was published later");
            }
        }
    }

    private static void deadlineDuringAdmission() throws Exception {
        ControlledConnection transport = new ControlledConnection();
        Recorder recorder = new Recorder();
        try (Peer peer = new Peer(transport, "client", options(recorder))) {
            fillOutput(peer, transport);
            CompletableFuture<Object> producer = async(() -> await(peer.call(RequestContext.empty(), "expired", null,
                Duration.ofMillis(350), null).result()));
            Map<String, Object> pressure = recorder.first("backpressure");
            expectError(producer, "request_timeout");
            check(numeric(pressure, "deadline") == WRITE_TIMEOUT.toNanos(), "backpressure lost its measured write deadline");
            check(!peer.closed().isDone(), "a call's admission deadline closed the carrier");
            transport.release();
            peer.emit("barrier", null);
            transport.sent(frame -> "barrier".equals(frame.get("event")));
            check(transport.history().stream().map(PeerLifecycleTest::decode).noneMatch(frame -> "expired".equals(frame.get("method"))),
                "an expired unadmitted request reached the transport");
        }
    }

    private static void oneDeadlineIncludesAdmission() throws Exception {
        ControlledConnection transport = new ControlledConnection();
        Recorder recorder = new Recorder();
        try (Peer peer = new Peer(transport, "client", options(recorder))) {
            fillOutput(peer, transport);
            CompletableFuture<Peer.Call> producer = async(() -> peer.call(RequestContext.empty(), "late-reply", null,
                Duration.ofSeconds(3), null));
            recorder.first("backpressure");
            // The late response distinguishes one absolute deadline from restarting the
            // three-second timer after admission, with a one-second margin on both sides.
            Thread.sleep(Duration.ofSeconds(2));
            transport.release();
            Peer.Call call = await(producer);
            Frame sent = transport.sent(frame -> "late-reply".equals(frame.get("method")));
            Thread.sleep(Duration.ofSeconds(2));
            transport.deliver(Peer.map("version", 1, "kind", "response", "id", decode(sent).get("id"), "result", "too-late"));
            expectError(call.result(), "request_timeout");
            check(!peer.closed().isDone(), "a late response closed a healthy peer");
        }
    }

    @SuppressWarnings("try") // Closing before leaving the resource scope is the behavior under test.
    private static void closeReleasesBlockedProducers() throws Exception {
        for (boolean request : List.of(false, true)) {
            ControlledConnection transport = new ControlledConnection();
            Recorder recorder = new Recorder();
            try (Peer peer = new Peer(transport, "client", options(recorder))) {
                fillOutput(peer, transport);
                CompletableFuture<Object> producer = async(() -> {
                    if (request) return await(peer.call("blocked", null).result());
                    peer.emit("blocked", null);
                    return null;
                });
                recorder.first("backpressure");
                peer.close();
                await(peer.closed());
                // BOUND is half the configured write timeout. This must be the close
                // wakeup, rather than waiting until the original queue offer expires.
                expectError(producer, "disconnected");
            }
        }
    }

    @SuppressWarnings("try") // Observe normal closure before the final resource cleanup.
    private static void observersCannotBreakTraffic() throws Exception {
        Connection[] pair = Pipe.pair(1 << 20, 8);
        AtomicInteger observed = new AtomicInteger();
        Consumer<Map<String, Object>> failingObserver = event -> {
            observed.incrementAndGet();
            throw new IllegalStateException("diagnostic consumer failed");
        };
        try (Peer server = new Peer(pair[0], "server", options(failingObserver));
             Peer client = new Peer(pair[1], "client", options(failingObserver))) {
            server.handle("echo", (context, params) -> params);
            check("first".equals(await(client.call("echo", "first").result())), "observer failure stranded the first result");
            check("second".equals(await(client.call("echo", "second").result())), "observer failure ended the connection");
            check(!client.closed().isDone() && !server.closed().isDone(), "observer failure closed a peer");
            client.close();
            await(client.closed());
            check(observed.get() > 4, "the throwing observer was not exercised through request completion");
        }
    }

    private static void metadataIsASnapshot() {
        Map<String, String> supplied = new LinkedHashMap<>(Map.of("tenant", "original"));
        Map<String, Object> frame = Peer.map("id", "c:1", "meta", supplied);
        RequestContext context = new RequestContext(frame);
        supplied.put("tenant", "changed outside context");
        frame.put("id", "c:2");
        check(context.id().equals("c:1"), "context retained its caller's mutable frame");
        check(context.metadata().equals(Map.of("tenant", "original")), "context retained its caller's mutable metadata");
        Map<String, String> read = context.metadata();
        read.put("tenant", "changed through accessor");
        read.put("new", "value");
        check(context.metadata().equals(Map.of("tenant", "original")), "metadata accessor changed stored carriage");
        check(RequestContext.empty().metadata() == null, "absent metadata became a present map");
    }

    private static void observersReceiveMeasurements() throws Exception {
        ControlledConnection transport = new ControlledConnection();
        transport.release();
        Recorder recorder = new Recorder();
        try (Peer peer = new Peer(transport, "client", options(recorder))) {
            String eventData = "😀";
            peer.emit("sample", eventData);
            Frame sentEvent = transport.sent(frame -> "sample".equals(frame.get("event")));
            check(numeric(recorder.first("event.emitted"), "bytes") == Json.stringify(eventData).getBytes(StandardCharsets.UTF_8).length,
                "event bytes were not measured from UTF-8 payload bytes");
            check(numeric(recorder.first("frame.sent"), "bytes") == sentEvent.data().length,
                "frame bytes were not measured from the actual encoded frame");
            CompletableFuture<Object> delivered = new CompletableFuture<>();
            peer.onEvent("incoming", (context, data) -> delivered.complete(data));
            Frame incoming = transport.deliver(Peer.map("version", 1, "kind", "event", "event", "incoming", "data", eventData));
            await(delivered);
            check(numeric(recorder.first("frame.received"), "bytes") == incoming.data().length,
                "received frame bytes were not measured");
            check(numeric(recorder.first("event.delivered"), "bytes") == Json.stringify(eventData).getBytes(StandardCharsets.UTF_8).length,
                "delivered event bytes were not measured");
            Peer.Call call = peer.call("measured", null);
            Frame request = transport.sent(frame -> "measured".equals(frame.get("method")));
            transport.deliver(Peer.map("version", 1, "kind", "response", "id", decode(request).get("id"), "result", null));
            check(await(call.result()) == null, "measured request lost its explicit null result");
            Map<String, Object> ended = recorder.first("request.ended");
            check(numeric(ended, "duration") >= 0, "request duration was not a measured nonnegative interval");
            check(numeric(ended, "at") > 0, "observer timestamp was not measured");
        }
    }

    private static void fillOutput(Peer peer, ControlledConnection transport) throws Exception {
        peer.emit("first", null);
        check(transport.firstSend.await(BOUND.toNanos(), TimeUnit.NANOSECONDS), "writer never reached the controlled transport");
        peer.emit("second", null);
    }

    @FunctionalInterface private interface Task<T> { T run() throws Exception; }

    private static <T> CompletableFuture<T> async(Task<T> task) {
        CompletableFuture<T> result = new CompletableFuture<>();
        Thread.ofVirtual().name("peer-lifecycle-producer").start(() -> {
            try { result.complete(task.run()); } catch (Throwable failure) { result.completeExceptionally(failure); }
        });
        return result;
    }

    private static <T> T await(CompletableFuture<T> future) throws Exception {
        return future.get(BOUND.toNanos(), TimeUnit.NANOSECONDS);
    }

    private static void expectError(CompletableFuture<?> future, String code) throws Exception {
        try { await(future); throw new AssertionError("expected " + code + ", received success"); }
        catch (ExecutionException failure) {
            check(Peer.asError(failure).code().equals(code), "expected " + code + ", received " + Peer.asError(failure).code());
        }
    }

    private static long numeric(Map<String, Object> event, String member) {
        Object value = event.get(member);
        check(value instanceof Number, event.get("type") + "." + member + " is not numeric: " + value);
        return ((Number) value).longValue();
    }

    private static void check(boolean condition, String message) {
        assertions++;
        if (!condition) throw new AssertionError(message);
    }

    private static Map<String, Object> decode(Frame frame) { return Json.object(Json.parse(frame.data())); }

    private static final class Recorder implements Consumer<Map<String, Object>> {
        private final List<Map<String, Object>> events = Collections.synchronizedList(new ArrayList<>());
        private final ConcurrentHashMap<String, CompletableFuture<Map<String, Object>>> first = new ConcurrentHashMap<>();
        @Override public void accept(Map<String, Object> event) {
            Map<String, Object> snapshot = new LinkedHashMap<>(event);
            events.add(snapshot);
            first.computeIfAbsent((String) event.get("type"), ignored -> new CompletableFuture<>()).complete(snapshot);
        }
        Map<String, Object> first(String type) throws Exception {
            return await(first.computeIfAbsent(type, ignored -> new CompletableFuture<>()));
        }
        List<Map<String, Object>> snapshot() { synchronized (events) { return List.copyOf(events); } }
    }

    /** The first carrier write can be paused while its peer's single output slot fills. */
    private static final class ControlledConnection implements Connection {
        private final CountDownLatch gate = new CountDownLatch(1);
        final CountDownLatch firstSend = new CountDownLatch(1);
        private final ArrayBlockingQueue<Frame> outgoing = new ArrayBlockingQueue<>(32);
        private final ArrayBlockingQueue<Frame> incoming = new ArrayBlockingQueue<>(32);
        private final List<Frame> history = Collections.synchronizedList(new ArrayList<>());
        private final CompletableFuture<CloseInfo> closed = new CompletableFuture<>();

        @Override public void send(Frame frame, Duration timeout) throws CloseException, InterruptedException, TimeoutException {
            firstSend.countDown();
            if (!gate.await(timeout.toNanos(), TimeUnit.NANOSECONDS)) throw new TimeoutException("controlled write timed out");
            if (closed.isDone()) throw new CloseException(1000, "closed");
            history.add(frame);
            if (!outgoing.offer(frame)) throw new AssertionError("test's outgoing recording capacity exhausted");
        }
        @Override public Frame receive(Duration timeout) throws CloseException, InterruptedException, TimeoutException {
            if (closed.isDone()) throw new CloseException(1000, "closed");
            Frame frame = incoming.poll(timeout.toNanos(), TimeUnit.NANOSECONDS);
            if (frame == null) throw new TimeoutException("controlled receive timed out");
            return frame;
        }
        @Override public void close(int code, String reason) { closed.complete(new CloseInfo(code, reason)); release(); }
        @Override public void abort() { close(1006, ""); }
        @Override public CompletableFuture<CloseInfo> closed() { return closed; }
        void release() { gate.countDown(); }
        Frame sent(Predicate<Map<String, Object>> matches) throws Exception {
            long deadline = System.nanoTime() + BOUND.toNanos();
            for (;;) {
                Frame frame = outgoing.poll(Math.max(0, deadline - System.nanoTime()), TimeUnit.NANOSECONDS);
                if (frame == null) throw new AssertionError("expected outbound frame never reached the transport");
                if (matches.test(decode(frame))) return frame;
            }
        }
        Frame deliver(Map<String, Object> frame) {
            Frame value = new Frame("text", Json.stringify(frame).getBytes(StandardCharsets.UTF_8));
            if (!incoming.offer(value)) throw new AssertionError("test's incoming recording capacity exhausted");
            return value;
        }
        List<Frame> history() { synchronized (history) { return List.copyOf(history); } }
    }
}
