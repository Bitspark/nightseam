package io.nightseam.runtime;

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
            peerWireRoundTrip(server,client);
        }
        pair=Pipe.pair(1<<20,8);
        try(var peer=new Peer(pair[0],"server",PeerOptions.defaults())) {
            pair[1].send(new Frame("text","{\"version\":1,\"kind\":\"event\",\"event\":\"tick\",\"data\":null,\"extra\":1}".getBytes(StandardCharsets.UTF_8)),BOUND);
            check(peer.closed().get(3,TimeUnit.SECONDS).code()==4011,"malformed profile frame close");
        }
        System.out.println("Java peer: "+rows+" frame rows, cancellation, presence, reverse context, identity and relative paths passed");
    }
    private static void peerWireRoundTrip(Peer server,Peer client) throws Exception {
        List<String> path=List.of("a/b","", "😀", "read");
        Runnable detach=server.wire().receive(path,new Receiver(false,(delivered,message)-> {
            check(delivered.equals(path),"wire path changed");
            message.returnAddress().send(List.of(),new Message(Peer.map("version",1,"kind","response","id",message.frame().get("id"),"result",message.frame().get("params")),null));
        },null));
        var result=new CompletableFuture<Map<String,Object>>();
        Wire address=new Wire() {
            public void send(List<String> delivered,Message message) { result.complete(message.frame()); }
            public Runnable receive(List<String> delivered,Receiver receiver) { throw new UnsupportedOperationException(); }
            public void close(int code,String reason) {}
        };
        client.wire().send(path,new Message(Peer.map("version",1,"kind","request","id","c:123","params",null),address));
        var response=result.get(3,TimeUnit.SECONDS);
        check(response.containsKey("result") && response.get("result")==null,"Wire null result lost");
        detach.run(); detach.run();
    }
}
