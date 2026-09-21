package io.nightseam.conformance;

import io.nightseam.duplex.*;
import io.nightseam.runtime.*;
import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.net.URI;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.ArrayList;
import java.util.Base64;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.function.Predicate;
import static io.nightseam.runtime.Peer.map;

/** Private driver 1 adapter: canned behavior only, all wire behavior belongs to the runtime. */
public final class Testee {
    private final Map<String,Object> handles=new LinkedHashMap<>();
    private long next;
    private static final Duration LONG=Duration.ofDays(365);
    private static final class Inbox<T> {
        private final List<T> values=new ArrayList<>();
        synchronized void put(T value) { values.add(value); notifyAll(); }
        synchronized T take(Duration timeout,Predicate<T> match) throws Exception {
            long end=System.nanoTime()+timeout.toNanos();
            for(;;) {
                for(int i=0;i<values.size();i++) if(match.test(values.get(i))) return values.remove(i);
                long rest=end-System.nanoTime(); if(rest<=0) throw new TimeoutException();
                TimeUnit.NANOSECONDS.timedWait(this,rest);
            }
        }
    }
    private static final class Conn implements AutoCloseable {
        final Connection connection;
        final boolean lazy;
        final Inbox<Object> frames=new Inbox<>();
        Thread reader;
        Conn(Connection connection,boolean lazy) {
            this.connection=connection; this.lazy=lazy;
            if(!lazy) reader=Thread.ofVirtual().start(() -> {
                try { for(;;) frames.put(connection.receive(LONG)); }
                catch(Exception failure) { frames.put(failure); }
            });
        }
        Frame receive(Duration timeout) throws Exception {
            if(lazy) return connection.receive(timeout);
            Object value=frames.take(timeout,x->true);
            if(value instanceof Exception failure) { frames.put(failure); throw failure; }
            return (Frame)value;
        }
        public void close() { connection.abort(); if(reader!=null) reader.interrupt(); }
    }
    private static final class ControlledPeer implements AutoCloseable {
        final Peer peer;
        final List<Map<String,Object>> observations;
        final Inbox<Map<String,Object>> events=new Inbox<>(), requests=new Inbox<>();
        final Map<String,CompletableFuture<Void>> signals=new java.util.concurrent.ConcurrentHashMap<>();
        ControlledPeer(Connection connection,String role,Map<String,Object> options) {
            observations=java.util.Collections.synchronizedList(new ArrayList<>());
            @SuppressWarnings("unchecked") Map<String,String> families=(Map<String,String>)(Map<?,?>)options.getOrDefault("families",Map.of());
            PeerOptions config=new PeerOptions(number(options,"max_concurrent_handlers",64),number(options,"max_pending_requests",128),
                number(options,"queue_capacity",128),number(options,"max_frame_bytes",1<<20),
                Duration.ofMillis(number(options,"request_timeout_ms",30000)),Duration.ofMillis(number(options,"write_timeout_ms",10000)),
                families,Boolean.TRUE.equals(options.get("observe"))?observations::add:null);
            peer=new Peer(connection,role,config);
            peer.onEvent((context,data) -> {
                String name=(String)contextFrameEvent(context);
                var value=map("name",name,"data",data);
                if(context.metadata()!=null && !context.metadata().isEmpty()) value.put("meta",context.metadata());
                events.put(value); signals.computeIfAbsent(name,k->new CompletableFuture<>()).complete(null);
            });
        }
        private String contextFrameEvent(RequestContext context) { return context.event(); }
        void handle(String method,Map<String,Object> behavior) {
            peer.handle(method,(context,params) -> {
                var started=map("id",context.id(),"method",method,"phase","started");
                if(context.metadata()!=null && !context.metadata().isEmpty()) started.put("meta",context.metadata());
                requests.put(started);
                String outcome="ok";
                try {
                    switch(string(behavior,"kind","echo")) {
                        case "echo": return params;
                        case "return": return behavior.get("value");
                        case "fail":
                            outcome="error";
                            if(behavior.containsKey("data")) throw new PublicError(string(behavior,"code","failed"),string(behavior,"message","Failed"),behavior.get("data"));
                            throw new PublicError(string(behavior,"code","failed"),string(behavior,"message","Failed"));
                        case "wait": context.cancellation().get(); outcome="cancelled"; throw new PublicError("cancelled","Request cancelled");
                        case "hold": signals.computeIfAbsent(string(behavior,"until",""),k->new CompletableFuture<>()).get(); return behavior.get("value");
                        case "panic": outcome="panic"; throw new IllegalStateException(String.valueOf(behavior.get("value")));
                        case "reverse":
                            try { return peer.call(context,string(behavior,"method",""),behavior.getOrDefault("params",params),null,null).result().get(); }
                            catch(Exception failure) { outcome="error"; throw Peer.asError(failure); }
                        case "emit": peer.emit(context,string(behavior,"event",""),behavior.get("data"),null,peer.options().writeTimeout()); return behavior.get("then");
                        default: throw new IllegalArgumentException("unknown behavior");
                    }
                } finally { requests.put(map("id",context.id(),"method",method,"phase","ended","outcome",outcome)); }
            });
        }
        public void close() { peer.close(); for(var signal:signals.values()) signal.complete(null); }
    }
    private record Listener(WebSocketTransport.Listener transport,boolean peer,Map<String,Object> options) implements AutoCloseable {
        public void close() throws Exception { transport.close(); }
    }
    private String mint(Object value) { String name="h"+(++next); handles.put(name,value); return name; }
    private <T> T get(Map<String,Object> request,Class<T> type) {
        String name=string(request,"on",""); Object value=handles.get(name);
        if(value==null) throw new PublicError("unknown_handle",name.isEmpty()?"Missing handle":name);
        if(!type.isInstance(value)) throw new PublicError("invalid","Handle is not a "+type.getSimpleName());
        return type.cast(value);
    }
    private static int number(Map<String,Object> value,String name,int fallback) { Object raw=value.get(name); return raw==null?fallback:((Number)raw).intValue(); }
    private static String string(Map<String,Object> value,String name,String fallback) { Object raw=value.get(name); return raw==null?fallback:(String)raw; }
    private static Duration within(Map<String,Object> value) { return Duration.ofMillis(number(value,"within_ms",2000)); }
    private static Map<String,Object> object(Map<String,Object> value,String name) { Object raw=value.get(name); return raw==null?Map.of():Json.object(raw); }
    @SuppressWarnings("unchecked") private static List<String> protocols(Map<String,Object> value) { return (List<String>)(List<?>)value.getOrDefault("subprotocols",List.of()); }
    @SuppressWarnings("unchecked") private static Map<String,String> metadata(Map<String,Object> value) { return (Map<String,String>)(Map<?,?>)value.get("meta"); }
    private void reset() {
        for(Object value:new ArrayList<>(handles.values())) {
            try { if(value instanceof AutoCloseable closeable) closeable.close(); }
            catch(Exception failure) { System.err.println(failure); }
        }
        handles.clear();
    }
    private Object operation(Map<String,Object> request) throws Exception {
        String op=(String)request.get("op"); Duration bound=within(request);
        switch(op) {
            case "hello": return map("driver",1,"language","java","layers",List.of("seam","peer"),"features",List.of("listen","pipe","lazy","observer","propagator"));
            case "reset": case "bye": reset(); return Map.of();
            case "conn.listen": case "peer.listen": {
                boolean peer=op.startsWith("peer"); var options=object(request,"options");
                var listener=WebSocketTransport.listen(peer?number(options,"max_frame_bytes",1<<20):number(request,"limit",1<<20),protocols(request));
                return map("handle",mint(new Listener(listener,peer,options)),"url",listener.uri().toString());
            }
            case "conn.accept": case "peer.accept": {
                var listener=get(request,Listener.class); Connection connection=listener.transport().accept(bound);
                if(listener.peer()) { var controlled=new ControlledPeer(connection,"server",listener.options()); return map("handle",mint(controlled),"subprotocol",connection.subprotocol()); }
                return map("handle",mint(new Conn(connection,"lazy".equals(request.get("consume")))));
            }
            case "conn.dial": case "peer.dial": {
                boolean peer=op.startsWith("peer"); var options=object(request,"options");
                Connection connection=WebSocketTransport.dial(URI.create((String)request.get("url")),peer?number(options,"max_frame_bytes",1<<20):number(request,"limit",1<<20),protocols(request));
                if(peer) return map("handle",mint(new ControlledPeer(connection,"client",options)),"subprotocol",connection.subprotocol());
                return map("handle",mint(new Conn(connection,"lazy".equals(request.get("consume")))));
            }
            case "conn.pipe": {
                Connection[] pair=Pipe.pair(number(request,"limit",1<<20),8); boolean lazy="lazy".equals(request.get("consume"));
                return map("a",mint(new Conn(pair[0],lazy)),"b",mint(new Conn(pair[1],lazy)));
            }
            case "conn.send": {
                String kind=string(request,"kind","text"); byte[] data=kind.equals("text")?string(request,"text","").getBytes(StandardCharsets.UTF_8):Base64.getDecoder().decode(string(request,"base64",""));
                get(request,Conn.class).connection.send(new Frame(kind,data),bound); return Map.of();
            }
            case "conn.receive": {
                Frame frame=get(request,Conn.class).receive(bound);
                return map("kind",frame.kind(),frame.kind().equals("text")?"text":"base64",frame.kind().equals("text")?new String(frame.data(),StandardCharsets.UTF_8):Base64.getEncoder().encodeToString(frame.data()));
            }
            case "conn.close": get(request,Conn.class).connection.close(number(request,"code",1000),string(request,"reason","")); return Map.of();
            case "conn.abort": get(request,Conn.class).connection.abort(); return Map.of();
            case "conn.await_close": {
                CloseInfo info=get(request,Conn.class).connection.closed().get(bound.toNanos(),TimeUnit.NANOSECONDS); return map("code",info.code(),"reason",info.reason());
            }
            case "peer.over": {
                Conn connection=get(request,Conn.class); if(!connection.lazy) throw new PublicError("invalid","Peer takeover requires lazy connection");
                return map("handle",mint(new ControlledPeer(connection.connection,string(request,"role","client"),object(request,"options"))));
            }
            case "peer.handle": get(request,ControlledPeer.class).handle(string(request,"method",""),object(request,"behavior")); return Map.of();
            case "peer.identity": get(request,ControlledPeer.class).peer.identity(string(request,"path",""),string(request,"digest","")); return Map.of();
            case "peer.check_identity": get(request,ControlledPeer.class).peer.checkIdentity(string(request,"path",""),string(request,"digest",""),bound).get(bound.toNanos(),TimeUnit.NANOSECONDS); return Map.of();
            case "peer.on_event": {
                var controlled=get(request,ControlledPeer.class); String name=string(request,"name",""); var behavior=object(request,"behavior");
                controlled.peer.onEvent(name,(context,data) -> {
                    switch(string(behavior,"kind","record")) {
                        case "block": controlled.peer.closed().get(); break;
                        case "panic": throw new IllegalStateException("event panic");
                        default: break;
                    }
                }); return Map.of();
            }
            case "peer.call": {
                var controlled=get(request,ControlledPeer.class); int timeout=number(request,"timeout_ms",0);
                var call=controlled.peer.call(RequestContext.empty(),string(request,"method",""),request.get("params"),timeout==0?null:Duration.ofMillis(timeout),metadata(request));
                return map("handle",mint(call));
            }
            case "call.await": {
                try { return map("result",get(request,Peer.Call.class).result().get(bound.toNanos(),TimeUnit.NANOSECONDS)); }
                catch(java.util.concurrent.ExecutionException failure) { return map("error",Peer.asError(failure).value()); }
            }
            case "call.cancel": get(request,Peer.Call.class).cancel(); return Map.of();
            case "peer.emit": get(request,ControlledPeer.class).peer.emit(RequestContext.empty(),string(request,"event",""),request.get("data"),metadata(request),bound); return Map.of();
            case "peer.await_event": {
                var value=get(request,ControlledPeer.class).events.take(bound,e -> request.get("name").equals(e.get("name")));
                value.remove("name"); return value;
            }
            case "peer.await_request": return get(request,ControlledPeer.class).requests.take(bound,r -> request.get("method").equals(r.get("method")) && request.get("phase").equals(r.get("phase")));
            case "peer.observed": {
                var observed=get(request,ControlledPeer.class).observations;
                synchronized(observed) {
                    var values=new ArrayList<Map<String,Object>>();
                    for(var item:observed) {
                        var copy=new LinkedHashMap<>(item); copy.remove("at");
                        if(!Boolean.TRUE.equals(request.get("trace"))) copy.remove("trace");
                        for(String metric:List.of("bytes","duration","deadline")) if(copy.get(metric) instanceof Number measured)
                            copy.put(metric,metric.equals("bytes")?measured.longValue()>0:measured.longValue()>=0);
                        values.add(copy);
                    }
                    if(!Boolean.FALSE.equals(request.get("drain"))) observed.clear(); return values;
                }
            }
            case "peer.close": get(request,ControlledPeer.class).peer.close(); return Map.of();
            case "peer.await_close": {
                CloseInfo info=get(request,ControlledPeer.class).peer.closed().get(bound.toNanos(),TimeUnit.NANOSECONDS); return map("clean",info.code()==1000,"code",info.code());
            }
            case "peer.recorded_wire_witness": return RecordedWire.witness(bound);
            default: throw new PublicError("unsupported","Unsupported operation: "+op);
        }
    }
    public static void main(String[] args) throws Exception {
        Testee testee=new Testee();
        try(var input=new BufferedReader(new InputStreamReader(System.in,StandardCharsets.UTF_8))) {
            String line;
            while((line=input.readLine())!=null) {
                Map<String,Object> request=Json.object(Json.parse(line)); Map<String,Object> response;
                try { response=map("id",request.get("id"),"ok",testee.operation(request)); }
                catch(Exception failure) {
                    Object error=failure instanceof CloseException close
                        ?map("code","closed","message",close.getMessage(),"close_code",close.code(),"reason",close.reason())
                        :Peer.asError(failure).value();
                    response=map("id",request.get("id"),"error",error);
                }
                System.out.println(Json.stringify(response)); System.out.flush();
                if("bye".equals(request.get("op"))) break;
            }
        } finally { testee.reset(); }
    }
}
