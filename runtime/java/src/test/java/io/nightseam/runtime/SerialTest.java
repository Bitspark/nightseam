package io.nightseam.runtime;

import io.nightseam.duplex.*;
import java.nio.file.*;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.Map;
import java.util.concurrent.*;

final class SerialTest {
    private static final Duration BOUND=Duration.ofSeconds(2);
    private static void check(boolean ok,String message) { if(!ok) throw new AssertionError(message); }
    static void run(Path root) throws Exception {
        var table=Json.object(Json.parse(Files.readAllBytes(root.resolve("conformance/tables/serials.json"))));
        for(var item:Json.array(table.get("rows"))) {
            var row=Json.object(item); var pair=Pipe.pair(1<<20,8);
            try(var peer=new Peer(pair[0],"server",PeerOptions.defaults())) {
                peer.handle("echo",(context,params)->params);
                String before=(String)row.get("before");
                pair[1].send(new Frame("text",before.getBytes(StandardCharsets.UTF_8)),BOUND);
                if(Json.object(Json.parse(before)).get("kind").equals("request")) pair[1].receive(BOUND);
                pair[1].send(new Frame("text",((String)row.get("frame")).getBytes(StandardCharsets.UTF_8)),BOUND);
                if(Boolean.TRUE.equals(row.get("valid"))) {
                    pair[1].receive(BOUND);
                    check(!peer.closed().isDone(),"valid serial closed: "+row.get("name"));
                } else check(peer.closed().get(2,TimeUnit.SECONDS).code()==4011,"serial refusal: "+row.get("name"));
            }
        }
        var reserved=new CountDownLatch(1); var release=new CountDownLatch(1);
        var defaults=PeerOptions.defaults();
        var options=new PeerOptions(defaults.maxConcurrentHandlers(),defaults.maxPendingRequests(),1,
            defaults.maxFrameBytes(),defaults.requestTimeout(),defaults.writeTimeout(),Map.of(),event->{
                if(event.get("type").equals("request.started") && "c:1".equals(event.get("id"))) {
                    reserved.countDown();
                    try { release.await(2,TimeUnit.SECONDS); } catch(InterruptedException e) { Thread.currentThread().interrupt(); }
                }
            });
        var pair=Pipe.pair(1<<20,8);
        try(var peer=new Peer(pair[0],"client",options)) {
            var first=Thread.ofVirtual().start(()->peer.call("first",null));
            check(reserved.await(2,TimeUnit.SECONDS),"first reservation missing");
            var second=Thread.ofVirtual().start(()->peer.call("second",null));
            second.join(50); release.countDown(); first.join(2000); second.join(2000);
            for(int n=1;n<=2;n++) {
                var frame=Json.object(Json.parse(pair[1].receive(BOUND).data()));
                check(frame.get("id").equals("c:"+n),"serial publication inverted: "+frame);
            }
        } finally { release.countDown(); }
        var waiting=new CountDownLatch(1); var unblock=new CountDownLatch(1);
        var bounded=new PeerOptions(defaults.maxConcurrentHandlers(),1,1,
            defaults.maxFrameBytes(),defaults.requestTimeout(),defaults.writeTimeout(),Map.of(),event->{
                if(event.get("type").equals("request.started")) {
                    waiting.countDown();
                    try { unblock.await(2,TimeUnit.SECONDS); } catch(InterruptedException e) { Thread.currentThread().interrupt(); }
                }
            });
        pair=Pipe.pair(1<<20,8);
        try(var peer=new Peer(pair[0],"client",bounded)) {
            var first=Thread.ofVirtual().start(()->peer.call("held",null));
            check(waiting.await(2,TimeUnit.SECONDS),"publication did not enter");
            try { peer.call("over-budget",null).result().get(200,TimeUnit.MILLISECONDS); throw new AssertionError("publication exceeded pending limit"); }
            catch(ExecutionException error) { check(Peer.asError(error).code().equals("busy"),"publication waiter escaped pending limit"); }
            finally { unblock.countDown(); }
            first.join(2000);
        }
        pair=Pipe.pair(1<<20,8);
        try(var peer=new Peer(pair[0],"client",PeerOptions.defaults())) {
            var next=Peer.class.getDeclaredField("next"); next.setAccessible(true);
            // Exercise the final reservation without billions of requests.
            next.setLong(peer,Long.MAX_VALUE);
            try { peer.call("overflow",null).result().get(2,TimeUnit.SECONDS); throw new AssertionError("serial wrapped"); }
            catch(ExecutionException error) { check(Peer.asError(error).code().equals("identifier_exhausted"),"wrong exhaustion error"); }
            check(peer.closed().get(2,TimeUnit.SECONDS).code()==4011,"exhaustion did not close");
        }
    }
}
