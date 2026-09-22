package io.nightseam.runtime;

import static io.nightseam.runtime.WireFrames.*;

import dev.bitspark.bitwire.*;

import io.nightseam.duplex.Connection;
import io.nightseam.duplex.Frame;
import io.nightseam.duplex.Pipe;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.function.Consumer;

public final class PeerWireTest {
    private static final Duration WITHIN = Duration.ofSeconds(3);

    public static void main(String[] args) throws Exception {
        outgoingValidationSnapshotAndAsyncReturn();
        liveCancellationKeepsFIFOAndItsReservedSlot();
        completionRetainsCancellationReservation();
        duplicateAndIdentity();
        dataOverflowIsBounded();
        incomingRoutesResponseAndCancellation();
    }

    private static void check(boolean condition, String detail) { if (!condition) throw new AssertionError(detail); }
    private static void await(CountDownLatch latch) {
        try { check(latch.await(3, TimeUnit.SECONDS), "peer wire barrier did not arrive"); }
        catch (InterruptedException interrupted) { throw new AssertionError(interrupted); }
    }
    private static <T> T take(BlockingQueue<T> queue) {
        try {
            T value = queue.poll(3, TimeUnit.SECONDS);
            if (value == null) throw new AssertionError("peer wire barrier did not arrive");
            return value;
        } catch (InterruptedException interrupted) { throw new AssertionError(interrupted); }
    }
    private static void fails(Runnable action) {
        boolean refused = false;
        try { action.run(); } catch (IllegalArgumentException | IllegalStateException expected) { refused = true; }
        check(refused, "operation should refuse");
    }
    private static Message request(String id, Sink address) { return new Message(frame(Peer.map("version",1,"kind","request","id",id,"params",null)), address == null ? null : address.address); }
    private static Message event(int value) { return new Message(frame(Peer.map("version",1,"kind","event","data",value)), null); }
    private static Message cancel(String id, Sink address) { return new Message(frame(Peer.map("version",1,"kind","cancel","id",id)), address == null ? null : address.address); }
    private static void answer(Message request, Object result) {
        request.returnAddress().wire().send(List.of(), new Message(frame(Peer.map("version",1,"kind","response","id",fields(request.frame()).get("id"),"result",result)), null));
    }
    private static String error(Message message) { return (String)Json.object(fields(message.frame()).get("error")).get("code"); }
    private static void rawSend(Connection connection, Map<String,Object> frame) throws Exception {
        connection.send(new Frame("text", Json.stringify(frame).getBytes(StandardCharsets.UTF_8)), WITHIN);
    }
    private static Map<String,Object> rawReceive(Connection connection) throws Exception {
        return Json.object(Json.parse(connection.receive(WITHIN).data()));
    }
    private static void rawReply(Connection connection, Map<String,Object> request, Object result) throws Exception {
        rawSend(connection, Peer.map("version",1,"kind","response","id",request.get("id"),"result",result));
    }
    private static final class Fixture implements AutoCloseable {
        final Connection remote;
        final Peer peer;
        Fixture(int queue, int pending, Consumer<Map<String,Object>> observer) {
            this(queue, pending, observer, "client");
        }
        Fixture(int queue, int pending, Consumer<Map<String,Object>> observer, String role) {
            Connection[] pipe = Pipe.pair(1 << 20, 32);
            remote = pipe[1];
            peer = new Peer(pipe[0], role, new PeerOptions(8,pending,queue,4096,WITHIN,WITHIN,Map.of(),observer));
        }
        @Override public void close() { peer.close(); remote.abort(); }
    }
    private static class Sink implements Wire {
        final ReturnAddress address = new ReturnAddress(this);
        final BlockingQueue<Message> replies = new ArrayBlockingQueue<>(16);
        final BlockingQueue<Thread> threads = new ArrayBlockingQueue<>(16);
        @Override public void send(List<String> path, Message message) {
            check(path.isEmpty(), "return capability received operation path");
            threads.add(Thread.currentThread()); replies.add(message);
        }
        @Override public boolean equals(Object other) { return other instanceof Sink; }
        @Override public int hashCode() { return 1; }
    }

    private static void outgoingValidationSnapshotAndAsyncReturn() throws Exception {
        for (String role : List.of("client", "server")) try (var fixture = new Fixture(8, 4, null, role)) {
            Endpoint wire = fixture.peer.wire(); var sink = new Sink();
            var frame = new LinkedHashMap<>(fields(request("s:1", sink).frame()));
            var payload = new LinkedHashMap<String,Object>(); payload.put("kept", 1); payload.put("null", null);
            frame.put("params", payload);
            wire.send(List.of("a/b", "💡"), new Message(frame(frame), sink.address)); payload.put("later", 2);
            Map<String,Object> sent = rawReceive(fixture.remote);
            check(sent.get("method").equals("3:a/b4:💡"), "canonical physical path");
            check(!Json.object(sent.get("params")).containsKey("later") && Json.object(sent.get("params")).containsKey("null"), "admission snapshot");
            rawReply(fixture.remote, sent, 7);
            Message reply = take(sink.replies);
            check(fields(reply.frame()).get("id").equals("s:1"), "logical return identifier changed");
            Thread callback = take(sink.threads);
            check(callback != Thread.currentThread() && !callback.getName().equals("nightseam-reader"), "return callback ran on caller/reader stack");
            var malformed = new LinkedHashMap<>(fields(request("c:1", sink).frame())); malformed.remove("params");
            fails(() -> wire.send(List.of("call"), new Message(frame(malformed), sink.address)));
            fails(() -> wire.send(List.of("call"), request("c:01", sink)));
            fails(() -> wire.send(List.of("call"), request("c:1", null)));
            fails(() -> wire.send(List.of("call"), new Message(frame(Peer.map("version",1,"kind","response","id","c:1","result",1)), null)));
        }
    }

    private static void completionRetainsCancellationReservation() throws Exception {
        var gate = new CountDownLatch(1); var release = new CountDownLatch(1); var completed = new CountDownLatch(1);
        Consumer<Map<String,Object>> observer = observation -> {
            if (observation.get("type").equals("event.emitted")) { gate.countDown(); await(release); }
            if (observation.get("type").equals("request.ended") && Boolean.FALSE.equals(observation.get("incoming"))) completed.countDown();
        };
        try (var fixture = new Fixture(1, 1, observer)) {
            Endpoint wire = fixture.peer.wire(); var first = new Sink(); var second = new Sink();
            wire.send(List.of("call"), request("c:1", first)); Map<String,Object> physical = rawReceive(fixture.remote);
            wire.send(List.of("event"), event(1)); await(gate);
            try {
                wire.send(List.of("call"), cancel("c:1", first));
                for (int n = 0; n < 4; n++) {
                    wire.send(List.of("call"), cancel("c:1", first));
                    wire.send(List.of("call"), cancel("c:99", first));
                }
                rawReply(fixture.remote, physical, 7); await(completed);
                wire.send(List.of("call"), request("c:2", second));
            } finally { release.countDown(); }
            check(rawReceive(fixture.remote).get("kind").equals("event"), "stale control escaped to carrier");
            check(((Number)fields(take(first.replies).frame()).get("result")).intValue() == 7, "first completion");
            check(error(take(second.replies)).equals("busy"), "queued control released reservation early");
            wire.send(List.of("call"), request("c:1", first));
            Map<String,Object> reused = rawReceive(fixture.remote);
            check(reused.get("kind").equals("request"), "stale cancel changed reused identity");
            rawReply(fixture.remote, reused, 9); take(first.replies);
        } finally { release.countDown(); }
    }

    private static void liveCancellationKeepsFIFOAndItsReservedSlot() throws Exception {
        var gate = new CountDownLatch(1); var release = new CountDownLatch(1);
        var firstRead = new CountDownLatch(1); var secondRead = new CountDownLatch(1);
        var eventNumber = new AtomicInteger();
        Consumer<Map<String,Object>> observer = observation -> {
            if (observation.get("type").equals("event.emitted")) {
                int number = eventNumber.incrementAndGet();
                if (number == 1) { gate.countDown(); await(release); }
                if (number == 2) await(firstRead);
            }
            if (observation.get("type").equals("request.ended") && Boolean.FALSE.equals(observation.get("incoming"))) await(secondRead);
        };
        try (var fixture = new Fixture(1, 1, observer)) {
            Endpoint wire = fixture.peer.wire(); var sink = new Sink();
            wire.send(List.of("call"), request("c:1", sink)); rawReceive(fixture.remote);
            wire.send(List.of("event"), event(1)); await(gate);
            try {
                wire.send(List.of("event"), event(2)); // Sole data slot is full.
                wire.send(List.of("call"), cancel("c:1", sink));
                for (int n = 0; n < 4; n++) {
                    wire.send(List.of("call"), cancel("c:1", sink));
                    wire.send(List.of("call"), cancel("c:99", sink));
                }
                release.countDown();
                Map<String,Object> first = rawReceive(fixture.remote); firstRead.countDown();
                Map<String,Object> second = rawReceive(fixture.remote); secondRead.countDown();
                Map<String,Object> control = rawReceive(fixture.remote);
                check(first.get("kind").equals("event") && ((Number)first.get("data")).intValue() == 1
                    && second.get("kind").equals("event") && ((Number)second.get("data")).intValue() == 2
                    && control.get("kind").equals("cancel"), "cancellation changed physical FIFO");
                check(error(take(sink.replies)).equals("cancelled"), "live cancellation did not settle caller");
                wire.send(List.of("call"), cancel("c:1", sink));
                wire.send(List.of("event"), event(3));
                check(rawReceive(fixture.remote).get("kind").equals("event"), "duplicate/stale cancel reached physical carrier");
            } finally { release.countDown(); firstRead.countDown(); secondRead.countDown(); }
        } finally { release.countDown(); firstRead.countDown(); secondRead.countDown(); }
    }

    private static void duplicateAndIdentity() throws Exception {
        try (var fixture = new Fixture(8, 2, null)) {
            Endpoint wire = fixture.peer.wire(); var first = new Sink(); var equalButDistinct = new Sink();
            wire.send(List.of("call"), request("c:1", first)); Map<String,Object> physical = rawReceive(fixture.remote);
            wire.send(List.of("call"), request("c:1", first));
            check(error(take(first.replies)).equals("invalid_message"), "duplicate active request admitted");
            wire.send(List.of("call"), request("c:1", equalButDistinct)); Map<String,Object> other = rawReceive(fixture.remote);
            rawReply(fixture.remote, physical, 1); rawReply(fixture.remote, other, 2);
            check(((Number)fields(take(first.replies).frame()).get("result")).intValue() == 1, "first identity");
            check(((Number)fields(take(equalButDistinct.replies).frame()).get("result")).intValue() == 2, "return equality replaced identity");
        }
    }

    private static void dataOverflowIsBounded() {
        var gate = new CountDownLatch(1); var release = new CountDownLatch(1); var closed = new CountDownLatch(1);
        Consumer<Map<String,Object>> observer = observation -> {
            if (observation.get("type").equals("event.emitted")) { gate.countDown(); await(release); }
        };
        try (var fixture = new Fixture(1, 1, observer)) {
            Endpoint wire = fixture.peer.wire();
            Routes.of(wire).register(List.of("inbound"), new Receiver( (path,message) -> {}, (code,reason) -> closed.countDown()));
            wire.send(List.of("event"), event(1)); await(gate);
            try {
                wire.send(List.of("event"), event(2));
                fails(() -> wire.send(List.of("event"), event(3)));
                await(closed);
                fails(() -> Routes.of(wire).register(List.of("later"), new Receiver( (path,message) -> {}, null)));
            } finally { release.countDown(); }
        } finally { release.countDown(); }
    }

    private static void incomingRoutesResponseAndCancellation() throws Exception {
        for (String role : List.of("client", "server")) try (var fixture = new Fixture(8, 8, null, role)) {
            String firstId = role.equals("client") ? "s:1" : "c:1";
            String secondId = role.equals("client") ? "s:2" : "c:2";
            Endpoint wire = fixture.peer.wire(); var received = new ArrayBlockingQueue<Message>(16);
            var paths = new ArrayBlockingQueue<List<String>>(16); var kinds = new ArrayBlockingQueue<String>(16);
            Receiver exact = new Receiver( (path,message) -> { paths.add(path); kinds.add("exact"); received.add(message); }, null);
            Routes.of(wire).registerPrefix(List.of(), new Receiver( (path,message) -> { kinds.add("root"); received.add(message); }, null));
            Routes.of(wire).registerPrefix(List.of("a"), new Receiver( (path,message) -> { kinds.add("namespace"); received.add(message); }, null));
            Runnable detach = Routes.of(wire).register(List.of("a"), exact);
            String name = io.nightseam.duplex.Wires.encodePath(List.of("a"));
            fails(() -> fixture.peer.handle(name, (context,params) -> params));
            fails(() -> fixture.peer.onEvent(name, (context,data) -> {}));
            rawSend(fixture.remote, Peer.map("version",1,"kind","request","id",firstId,"method",name,"params",null));
            Message call = take(received); check(take(kinds).equals("exact"), "exact did not outrank namespace");
            check(take(paths).equals(List.of("a")), "callback path changed");
            Message response = new Message(frame(Peer.map("version",1,"kind","response","id",firstId,"result",null)), null);
            fails(() -> call.returnAddress().wire().send(List.of("a"), response));
            fails(() -> call.returnAddress().wire().send(List.of(), new Message(frame(Peer.map("version",1,"kind","response","id",secondId,"result",1)), null)));
            fails(() -> call.returnAddress().wire().send(List.of(), new Message(frame(Peer.map("version",1,"kind","response","id",firstId)), null)));
            call.returnAddress().wire().send(List.of(), response);
            check(rawReceive(fixture.remote).containsKey("result"), "null response omitted");
            fails(() -> call.returnAddress().wire().send(List.of(), response));
            detach.run(); detach.run();
            rawSend(fixture.remote, Peer.map("version",1,"kind","request","id",secondId,"method",name,"params",null));
            Message held = take(received); check(take(kinds).equals("namespace"), "namespace at same path unavailable");
            Routes.of(wire).register(List.of("a"), exact);
            rawSend(fixture.remote, Peer.map("version",1,"kind","cancel","id",secondId));
            Message cancellation = take(received);
            check(take(kinds).equals("namespace") && fields(cancellation.frame()).get("kind").equals("cancel"), "cancellation redirected after replacement");
            check(cancellation.returnAddress() == held.returnAddress(), "cancellation changed return identity");
            check(Json.object(rawReceive(fixture.remote).get("error")).get("code").equals("cancelled"), "cancel response");
            String rawName = io.nightseam.duplex.Wires.encodePath(List.of("raw"));
            fails(() -> fixture.peer.handle(rawName, (context,params) -> "raw"));
            Routes.of(wire).register(List.of("raw"), new Receiver((path,message) -> answer(message, "raw"), null));
            Routes.of(wire).registerPrefix(List.of("raw"), new Receiver( (path,message) -> { kinds.add("raw-space"); received.add(message); }, null));
            String remotePrefix = role.equals("client") ? "s:" : "c:";
            rawSend(fixture.remote, Peer.map("version",1,"kind","request","id",remotePrefix+"3","method",rawName,"params",null));
            check(rawReceive(fixture.remote).get("result").equals("raw"), "namespace displaced exact raw handler");
            rawSend(fixture.remote, Peer.map("version",1,"kind","request","id",remotePrefix+"4","method",
                io.nightseam.duplex.Wires.encodePath(List.of("raw", "child")),"params",null));
            Message descendant = take(received); check(take(kinds).equals("raw-space"), "raw exact route suppressed namespace descendants");
            answer(descendant, "namespace"); rawReceive(fixture.remote);
            fails(() -> Routes.of(wire).register(List.of("raw"), exact));
            Routes.of(wire).register(List.of(), exact);
            Routes.of(wire).register(List.of("null"), new Receiver(null, null));
        }
    }
}
