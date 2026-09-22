package io.nightseam.runtime;

import static io.nightseam.runtime.WireFrames.*;

import dev.bitspark.bitwire.*;

import io.nightseam.duplex.*;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Duration;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.TimeUnit;

/** The same literal frame table as the reference, plus peer lifecycle invariants. */
public final class PeerTest {
    private static final Duration BOUND=Duration.ofSeconds(3);
    private static void check(boolean condition,String message) { if(!condition) throw new AssertionError(message); }
    public static void main(String[] args) throws Exception {
        closeOwnership();
        closeBeforeTransportNotification();
        closeDuringWrite();
        closeFromWriteObservation();
        Path root=Path.of(args[0]); int rows=0;
        var table=Json.object(Json.parse(Files.readAllBytes(root.resolve("conformance/tables/frames.json"))));
        for(Object item:Json.array(table.get("rows"))) {
            var row=Json.object(item); String role=(String)row.getOrDefault("to","server"); if(role.equals("either")) role="server";
            boolean valid=true;
            try { Envelope.decode(((String)row.get("frame")).getBytes(StandardCharsets.UTF_8),role); }
            catch(IllegalArgumentException failure) { valid=false; }
            check(valid==Boolean.TRUE.equals(row.get("valid")),"frame row "+row.get("name")); rows++;
        }
        var pair=Pipe.pair(1<<20,8);
        try(var server=new Peer(pair[0],"server",PeerOptions.defaults()); var client=new Peer(pair[1],"client",PeerOptions.defaults())) {
            server.handle("echo",(context,params)->params);
            var value=Json.object(client.call("echo",Json.parse("{\"present\":null,\"exact\":9007199254740991}")).result().get(3,TimeUnit.SECONDS));
            check(value.containsKey("present") && value.get("present")==null && !value.containsKey("absent"),"absence and null collapsed");
            check(value.get("exact").toString().equals("9007199254740991"),"numeric precision changed");
            var started=new CompletableFuture<RequestContext>();
            server.handle("wait",(context,params)-> { started.complete(context); context.cancellation().get(); return null; });
            var call=client.call(RequestContext.empty(),"wait",null,null,Map.of("tenant","example"));
            var context=started.get(3,TimeUnit.SECONDS);
            check(context.metadata().equals(Map.of("tenant","example")),"metadata not delivered");
            call.cancel(); context.cancellation().get(3,TimeUnit.SECONDS);
            try { call.result().get(3,TimeUnit.SECONDS); throw new AssertionError("cancel answered success"); }
            catch(java.util.concurrent.ExecutionException failure) { check(Peer.asError(failure).code().equals("cancelled"),"wrong cancellation"); }
            var reverseContext=new CompletableFuture<RequestContext>();
            client.handle("reverse",(incoming,params)-> { reverseContext.complete(incoming); return params; });
            server.handle("forward",(incoming,params)->server.call(incoming,"reverse",params,null,null).result().get());
            check(client.call(RequestContext.empty(),"forward","ok",null,Map.of("tenant","private")).result().get(3,TimeUnit.SECONDS).equals("ok"),"reverse call failed");
            check(reverseContext.get().metadata()==null,"received metadata leaked into reverse call");
            server.identity("sample","a".repeat(64));
            client.checkIdentity("sample","a".repeat(64),BOUND).get(3,TimeUnit.SECONDS);
            try { client.checkIdentity("sample","b".repeat(64),BOUND).get(3,TimeUnit.SECONDS); throw new AssertionError("mismatch accepted"); }
            catch(java.util.concurrent.ExecutionException failure) { check(Peer.asError(failure).code().equals("contract_mismatch"),"wrong identity error"); }
            check(client.call("echo","still-open").result().get(3,TimeUnit.SECONDS).equals("still-open"),"identity refusal closed carrier");
            server.handle("panic",(incoming,params)-> { throw new AssertionError("private handler detail"); });
            try { client.call("panic",null).result().get(3,TimeUnit.SECONDS); throw new AssertionError("handler panic answered success"); }
            catch(java.util.concurrent.ExecutionException failure) {
                var error=Peer.asError(failure);
                check(error.code().equals("internal") && !error.getMessage().contains("private"),"handler failure leaked");
            }
            check(client.call("echo","after-panic").result().get(3,TimeUnit.SECONDS).equals("after-panic"),"handler Error stranded peer");

        }
        pair=Pipe.pair(1<<20,8);
        try(var server=new Peer(pair[0],"server",PeerOptions.defaults()); var client=new Peer(pair[1],"client",PeerOptions.defaults())) { peerWireRoundTrip(server,client); }
        pair=Pipe.pair(1<<20,8);
        try(var peer=new Peer(pair[0],"server",PeerOptions.defaults())) {
            pair[1].send(new Frame("text","{\"version\":1,\"kind\":\"event\",\"event\":\"tick\",\"data\":null,\"extra\":1}".getBytes(StandardCharsets.UTF_8)),BOUND);
            check(peer.closed().get(3,TimeUnit.SECONDS).code()==4011,"malformed profile frame close");
        }
        System.out.println("Java peer: "+rows+" frame rows, cancellation, presence, reverse context, identity and relative paths passed");
    }
    private static void closeOwnership() throws Exception {
        // Closing observers may reenter the peer before the first caller has
        // reached the transport. Only the terminal winner chooses its action.
        var connection=new CloseProbe();
        var current=new java.util.concurrent.atomic.AtomicReference<Peer>();
        var defaults=PeerOptions.defaults();
        var options=new PeerOptions(defaults.maxConcurrentHandlers(),defaults.maxPendingRequests(),defaults.queueCapacity(),
            defaults.maxFrameBytes(),defaults.requestTimeout(),defaults.writeTimeout(),Map.of(),event -> {
                if (event.get("type").equals("connection.closed")) { current.get().end(1001,"reentered"); current.get().fail(); }
            });
        try (var peer=new Peer(connection,"server",options)) {
            current.set(peer); peer.close();
            check(connection.closes.get()==1 && connection.code==1000,"reentry changed the selected transport close");
            check(connection.aborts.get()==0,"reentry aborted graceful close");
        }
        // Hold failure between its transport observation and terminal CAS,
        // then let an explicit close win. The loser must never abort it.
        connection.ended.complete(new CloseInfo(1000,""));
        connection=new CloseProbe(); connection.inspect=true;
        try (var peer=new Peer(connection,"server",defaults)) {
            var failure=CompletableFuture.runAsync(peer::fail);
            connection.inspected.get(3,TimeUnit.SECONDS);
            try { peer.close(); } finally { connection.resume.complete(null); }
            failure.get(3,TimeUnit.SECONDS);
            check(connection.closes.get()==1 && connection.aborts.get()==0,"losing failure aborted graceful close");
        }
        connection.ended.complete(new CloseInfo(1000,""));
    }
    private static void closeBeforeTransportNotification() throws Exception {
        // A transport may wake its read/write waiter after selecting a close
        // but before it completes the separate closed notification.
        for (boolean writing:List.of(false,true)) {
            var connection=new CloseProbe();
            connection.closeFailure=new CloseException(1000,"remote done");
            connection.failOnWrite=writing;
            try (var peer=new Peer(connection,"server",PeerOptions.defaults())) {
                if (writing) peer.emit("tick",null);
                else connection.releaseReceive.countDown();
                var info=peer.closed().get(3,TimeUnit.SECONDS);
                check(info.equals(new CloseInfo(1000,"remote done")),"transport close became abnormal before notification: "+info);
                check(!connection.ended.isDone(),"test did not hold the transport notification");
                check(connection.closes.get()==0 && connection.aborts.get()==0,"known transport close chose another transport action");
            } finally {
                connection.releaseReceive.countDown();
                connection.ended.complete(new CloseInfo(1000,"remote done"));
            }
        }
    }
    private static void closeDuringWrite() throws Exception {
        var connection=new CloseProbe(); connection.holdWrite=true;
        try (var peer=new Peer(connection,"server",PeerOptions.defaults())) {
            peer.emit("tick",null);
            connection.writeStarted.get(3,TimeUnit.SECONDS);
            peer.emit("queued",null);
            peer.close();
            connection.releaseWrite.countDown();
            check(!connection.writeInterrupted.get(3,TimeUnit.SECONDS),"graceful close interrupted an admitted transport write");
            connection.sender.join(3000);
            check(!connection.sender.isAlive() && connection.writes.get()==1,"close admitted another queued transport write");
            check(connection.closes.get()==1 && connection.aborts.get()==0,"active write changed graceful close ownership");
        } finally {
            connection.releaseWrite.countDown();
            connection.ended.complete(new CloseInfo(1000,""));
        }
    }
    private static void closeFromWriteObservation() throws Exception {
        var connection=new CloseProbe();
        var current=new java.util.concurrent.atomic.AtomicReference<Peer>();
        var defaults=PeerOptions.defaults();
        var options=new PeerOptions(defaults.maxConcurrentHandlers(),defaults.maxPendingRequests(),defaults.queueCapacity(),
            defaults.maxFrameBytes(),defaults.requestTimeout(),defaults.writeTimeout(),Map.of(),event -> {
                if (event.get("type").equals("frame.sent")) current.get().close();
            });
        try (var peer=new Peer(connection,"server",options)) {
            current.set(peer); peer.emit("tick",null);
            peer.closed().get(3,TimeUnit.SECONDS);
            connection.transportClosed.get(3,TimeUnit.SECONDS).join(3000);
            check(connection.writes.get()==0,"closing from observation admitted a new transport write");
        } finally { connection.ended.complete(new CloseInfo(1000,"")); }
    }
    private static final class CloseProbe implements Connection {
        final java.util.concurrent.atomic.AtomicInteger closes=new java.util.concurrent.atomic.AtomicInteger();
        final java.util.concurrent.atomic.AtomicInteger aborts=new java.util.concurrent.atomic.AtomicInteger();
        final java.util.concurrent.atomic.AtomicInteger writes=new java.util.concurrent.atomic.AtomicInteger();
        final CompletableFuture<Thread> transportClosed=new CompletableFuture<>();
        final CompletableFuture<Void> inspected=new CompletableFuture<>(), resume=new CompletableFuture<>();
        final CompletableFuture<Void> writeStarted=new CompletableFuture<>();
        final CompletableFuture<Boolean> writeInterrupted=new CompletableFuture<>();
        final java.util.concurrent.CountDownLatch releaseWrite=new java.util.concurrent.CountDownLatch(1);
        final java.util.concurrent.CountDownLatch releaseReceive=new java.util.concurrent.CountDownLatch(1);
        CloseException closeFailure;
        boolean failOnWrite;
        volatile boolean inspect;
        volatile boolean holdWrite;
        volatile int code;
        volatile Thread sender;
        final CompletableFuture<CloseInfo> ended=new CompletableFuture<>() {
            @Override public CloseInfo getNow(CloseInfo fallback) {
                if (inspect) { inspected.complete(null); resume.join(); }
                return super.getNow(fallback);
            }
        };
        public void send(Frame frame,Duration timeout) throws InterruptedException,CloseException {
            sender=Thread.currentThread(); writes.incrementAndGet();
            if (closeFailure!=null && failOnWrite) throw closeFailure;
            if (!holdWrite) return;
            writeStarted.complete(null);
            try { releaseWrite.await(); writeInterrupted.complete(false); }
            catch (InterruptedException interrupted) { writeInterrupted.complete(true); throw interrupted; }
        }
        public Frame receive(Duration timeout) throws InterruptedException,CloseException {
            if (closeFailure!=null && !failOnWrite) { releaseReceive.await(); throw closeFailure; }
            new java.util.concurrent.CountDownLatch(1).await(); throw new AssertionError("unreachable");
        }
        public void close(int chosen,String reason) { code=chosen; closes.incrementAndGet(); transportClosed.complete(Thread.currentThread()); }
        public void abort() { aborts.incrementAndGet(); }
        public CompletableFuture<CloseInfo> closed() { return ended; }
    }
    private static void peerWireRoundTrip(Peer server,Peer client) throws Exception {
        List<String> path=List.of("a/b","", "😀", "read");
        Runnable detach=Routes.of(server.wire()).register(path, new Receiver((delivered,message)-> {
            check(delivered.equals(path),"wire path changed");
            message.returnAddress().wire().send(List.of(),new Message(frame(Peer.map("version",1,"kind","response","id",fields(message.frame()).get("id"),"result",fields(message.frame()).get("params"))),null));
        },null));
        var result=new CompletableFuture<Map<String,Object>>();
        Wire address=new Wire() {
            public void send(List<String> delivered,Message message) { result.complete(fields(message.frame())); }


        };
        client.wire().send(path,new Message(frame(Peer.map("version",1,"kind","request","id","c:123","params",null)),new ReturnAddress(address)));
        var response=result.get(3,TimeUnit.SECONDS);
        check(response.containsKey("result") && response.get("result")==null,"Wire null result lost");
        detach.run(); detach.run();
    }
}
