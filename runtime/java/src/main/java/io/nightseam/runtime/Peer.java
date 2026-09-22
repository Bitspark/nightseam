package io.nightseam.runtime;

import dev.bitspark.bitwire.*;

import io.nightseam.duplex.CloseException;
import io.nightseam.duplex.CloseInfo;
import io.nightseam.duplex.Connection;
import io.nightseam.duplex.Frame;
import java.nio.charset.StandardCharsets;
import java.security.SecureRandom;
import java.time.Duration;
import java.util.ArrayList;
import java.util.HexFormat;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.ScheduledThreadPoolExecutor;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;
import java.util.function.BiConsumer;

/** A symmetric bounded nightseam.duplex/1 peer over any framed connection. */
public final class Peer implements AutoCloseable {
    @FunctionalInterface public interface Handler { Object handle(RequestContext context, Object params) throws Exception; }
    @FunctionalInterface public interface EventHandler { void handle(RequestContext context, Object data) throws Exception; }
    private record Queued(Map<String,Object> frame, byte[] bytes) {}
    private static final SecureRandom RANDOM = new SecureRandom();
    private static final Duration READ_WAIT = Duration.ofDays(365);
    private final Connection connection;
    private final PeerOptions options;
    private final String role, prefix;
    private final AtomicLong next = new AtomicLong();
    private final AtomicBoolean ended = new AtomicBoolean();
    private final CompletableFuture<CloseInfo> closed = new CompletableFuture<>();
    private final ArrayBlockingQueue<Queued> output;
    private final ArrayBlockingQueue<Map<String,Object>> events;
    private final ConcurrentHashMap<String,Call> pending = new ConcurrentHashMap<>();
    private final ConcurrentHashMap<String,RequestContext> incoming = new ConcurrentHashMap<>();
    private final ConcurrentHashMap<String,Handler> handlers = new ConcurrentHashMap<>();
    private final ConcurrentHashMap<String,EventHandler> eventHandlers = new ConcurrentHashMap<>();
    private final ConcurrentHashMap<String,Long> observationStarts = new ConcurrentHashMap<>();
    private final CopyOnWriteArrayList<BiConsumer<RequestContext,Object>> listeners = new CopyOnWriteArrayList<>();
    private final ScheduledThreadPoolExecutor deadlines = new ScheduledThreadPoolExecutor(1,r -> {
        Thread t = new Thread(r,"nightseam-deadlines"); t.setDaemon(true); return t;
    });
    private final List<Thread> loops = new ArrayList<>();
    private final Thread writer;
    private volatile PeerWire wire;

    public Peer(Connection connection,String role,PeerOptions options) {
        if (!role.equals("client") && !role.equals("server")) throw new IllegalArgumentException("invalid role");
        this.connection=connection; this.role=role; this.prefix=role.equals("client")?"c:":"s:"; this.options=options;
        deadlines.setRemoveOnCancelPolicy(true);
        output=new ArrayBlockingQueue<>(options.queueCapacity()); events=new ArrayBlockingQueue<>(options.queueCapacity());
        observe(map("type","connection.opened","role",role));
        writer=Thread.ofVirtual().name("nightseam-writer").unstarted(this::writeLoop);
        loops.add(writer);
        loops.add(Thread.ofVirtual().name("nightseam-events").unstarted(this::eventLoop));
        loops.add(Thread.ofVirtual().name("nightseam-reader").unstarted(this::readLoop));
        for (Thread loop:loops) loop.start();
    }
    boolean hasRawHandlers() { return !handlers.isEmpty() || !eventHandlers.isEmpty(); }
    boolean hasHandlerOrEvent(String name) { return handlers.containsKey(name) || eventHandlers.containsKey(name); }
    public String subprotocol() { return connection.subprotocol(); }
    public CompletableFuture<CloseInfo> closed() { return closed; }
    public PeerOptions options() { return options; }
    public synchronized Endpoint wire() { if(wire==null) wire=new PeerWire(this); return wire; }
    public synchronized void handle(String name,Handler handler) {
        if (name.isEmpty() || handler==null) throw new IllegalArgumentException("invalid method");
        if(wire!=null && wire.hasExactReceiver(name)) throw new IllegalStateException("operation already has a wire receiver");
        handlers.put(name,handler);
    }
    public synchronized void onEvent(String name,EventHandler handler) {
        if (name.isEmpty() || handler==null) throw new IllegalArgumentException("invalid event");
        if(wire!=null && wire.hasExactReceiver(name)) throw new IllegalStateException("operation already has a wire receiver");
        eventHandlers.put(name,handler);
    }
    public Runnable onEvent(BiConsumer<RequestContext,Object> listener) {
        listeners.add(listener); return () -> listeners.remove(listener);
    }
    public final class Call {
        private final String id,method;
        private final Map<String,Object> frame;
        private final CompletableFuture<Object> result = new CompletableFuture<>();
        private final AtomicBoolean completed = new AtomicBoolean();
        private volatile ScheduledFuture<?> timer;
        private volatile Runnable detachCancellation=() -> {};
        private Call(String id,String method,Map<String,Object> frame) { this.id=id;this.method=method;this.frame=frame; }
        public CompletableFuture<Object> result() { return result; }
        public void cancel() { complete(null,new PublicError("cancelled","The caller gave up"),true); }
        private void complete(Object value,PublicError error,boolean withdraw) {
            if (!completed.compareAndSet(false,true)) return;
            if(timer!=null) timer.cancel(false);
            detachCancellation.run();
            pending.remove(id,this);
            requestEnded(frame,false,error);
            if (withdraw && !ended.get()) {
                var cancel=map("version",1,"kind","cancel","id",id); copyTrace(frame,cancel);
                output.offer(encoded(cancel));
            }
            if (error==null) result.complete(value); else result.completeExceptionally(error);
        }
    }
    public Call call(String method,Object params) { return call(RequestContext.empty(),method,params,null,null); }
    public Call call(RequestContext context,String method,Object params,Duration timeout,Map<String,String> meta) {
        return begin(context,method,params,timeout,meta,null,false);
    }
    Call begin(RequestContext context,String method,Object params,Duration timeout,Map<String,String> meta,
            Map<String,Object> carried,boolean immediate) {
        if (method==null || method.isEmpty()) throw new IllegalArgumentException("a call requires a method");
        String id=prefix+next.incrementAndGet();
        var frame=map("version",1,"kind","request","id",id,"method",method,"params",params);
        if (carried==null) inject(context,frame); else copyTrace(carried,frame);
        metadata(meta,frame);
        var call=new Call(id,method,frame);
        if(context.isCancelled()) {
            call.completed.set(true); call.result.completeExceptionally(new PublicError("cancelled","The caller gave up")); return call;
        }
        Duration limit=timeout==null || timeout.compareTo(options.requestTimeout())>0?options.requestTimeout():timeout;
        long deadline=System.nanoTime()+limit.toNanos();
        synchronized(pending) {
            if (ended.get() || pending.size()>=options.maxPendingRequests()) {
                call.completed.set(true);
                call.result.completeExceptionally(new PublicError(ended.get()?"disconnected":"busy",ended.get()?"Connection closed":"Outstanding call limit reached"));
                return call;
            }
            pending.put(id,call);
        }
        requestStarted(frame,false);
        try {
            enqueue(frame,Duration.ofNanos(Math.max(0,deadline-System.nanoTime())),immediate,context);
            call.timer=deadlines.schedule(() -> call.complete(null,new PublicError("request_timeout","The request deadline passed"),true),Math.max(0,deadline-System.nanoTime()),TimeUnit.NANOSECONDS);
            if(call.completed.get()) call.timer.cancel(false);
            call.detachCancellation=context.onCancel(call::cancel);
            if(call.completed.get()) call.detachCancellation.run();
        } catch(Exception e) { call.complete(null,e instanceof TimeoutException?new PublicError("request_timeout","The request deadline passed"):asError(e),false); }
        return call;
    }
    public void emit(String name,Object data) throws Exception { emit(RequestContext.empty(),name,data,null,options.writeTimeout()); }
    public void emit(RequestContext context,String name,Object data,Map<String,String> meta,Duration timeout) throws Exception {
        emitCarried(context,name,data,meta,timeout,null,false);
    }
    void emitCarried(RequestContext context,String name,Object data,Map<String,String> meta,Duration timeout,
            Map<String,Object> carried,boolean immediate) throws Exception {
        if (name.isEmpty()) throw new IllegalArgumentException("an event requires a name");
        var frame=map("version",1,"kind","event","event",name,"data",data);
        if (carried==null) inject(context,frame); else copyTrace(carried,frame);
        if(context.isCancelled()) throw new PublicError("cancelled","The caller gave up");
        metadata(meta,frame); eventObserved("event.emitted",frame); enqueue(frame,timeout,immediate,context);
    }
    private Queued encoded(Map<String,Object> frame) {
        byte[] bytes=Json.stringify(frame).getBytes(StandardCharsets.UTF_8);
        if (bytes.length>options.maxFrameBytes()) throw new PublicError("frame_too_large","Frame exceeds size limit");
        return new Queued(frame,bytes);
    }
    private void enqueue(Map<String,Object> frame,Duration bound,boolean immediate) throws Exception {
        enqueue(frame,bound,immediate,null);
    }
    private void enqueue(Map<String,Object> frame,Duration bound,boolean immediate,RequestContext context) throws Exception {
        if (ended.get()) throw new PublicError("disconnected","Connection closed");
        if(context!=null && context.isCancelled()) throw new PublicError("cancelled","The caller gave up");
        if(bound.isNegative() || bound.isZero()) throw new TimeoutException("Send deadline passed");
        Queued value=encoded(frame);
        if (output.offer(value)) return;
        if (!immediate) {
            backpressure(output.size(),false);
            Duration limit=bound.compareTo(options.writeTimeout())<0?bound:options.writeTimeout();
            long until=System.nanoTime()+limit.toNanos();
            while(System.nanoTime()<until) {
                if(ended.get()) throw new PublicError("disconnected","Connection closed");
                if(context!=null && context.isCancelled()) throw new PublicError("cancelled","The caller gave up");
                if(output.offer(value,Math.min(TimeUnit.MILLISECONDS.toNanos(10),Math.max(0,until-System.nanoTime())),TimeUnit.NANOSECONDS)) return;
            }
            if (bound.compareTo(options.writeTimeout())<0) throw new TimeoutException("Send deadline passed");
        }
        backpressure(output.size(),true); fail(); throw new PublicError("disconnected","Consumer stalled");
    }
    private void writeLoop() {
        try {
            while (!ended.get()) {
                Queued value=output.take();
                if (ended.get()) return;
                frameObserved("frame.sent",value.frame(),value.bytes().length);
                if (ended.get()) return;
                connection.send(new Frame("text",value.bytes()),options.writeTimeout());
            }
        } catch (Exception e) { fail(e); }
    }
    private void readLoop() {
        try {
            while (!ended.get()) {
                Frame received=connection.receive(READ_WAIT);
                Map<String,Object> frame;
                try {
                    if (!received.kind().equals("text") || received.data().length>options.maxFrameBytes()) throw new IllegalArgumentException();
                    frame=Envelope.decode(received.data(),role);
                } catch (RuntimeException invalid) { end(4011,"Invalid duplex frame"); return; }
                frameObserved("frame.received",frame,received.data().length);
                String id=(String)frame.get("id");
                switch ((String)frame.get("kind")) {
                    case "response" -> {
                        Call call=pending.get(id);
                        if (call!=null) call.complete(frame.get("result"),frame.containsKey("error")?PublicError.from(Json.object(frame.get("error"))):null,false);
                    }
                    case "cancel" -> { RequestContext context=incoming.get(id); if (context!=null) context.cancel(); }
                    case "request" -> startRequest(frame);
                    case "event" -> {
                        if (!events.offer(frame)) {
                            backpressure(events.size(),false);
                            if (!events.offer(frame,options.writeTimeout().toNanos(),TimeUnit.NANOSECONDS)) {
                                backpressure(events.size(),true); fail(); return;
                            }
                        }
                    }
                    default -> throw new AssertionError();
                }
            }
        } catch (Exception e) { fail(e); }
    }
    private void startRequest(Map<String,Object> frame) {
        String id=(String)frame.get("id"), name=(String)frame.get("method");
        if (incoming.containsKey(id)) { end(4011,"Duplicate active request"); return; }
        Handler handler=handlers.get(name);
        if (handler==null && wire!=null) handler=wire.handler(name);
        requestStarted(frame,true);
        if (handler==null || incoming.size()>=options.maxConcurrentHandlers()) {
            PublicError error=new PublicError(handler==null?"method_not_found":"busy",handler==null?"Unknown method":"Too many concurrent requests");
            requestEnded(frame,true,error); respond(frame,null,error,true); return;
        }
        RequestContext context=new RequestContext(frame);
        incoming.put(id,context);
        var deadline=deadlines.schedule(context::cancel,options.requestTimeout().toNanos(),TimeUnit.NANOSECONDS);
        Handler selected=handler;
        Thread.ofVirtual().name("nightseam-request").start(() -> {
          try {
            Object result=null; PublicError error=null;
            try {
                result=selected.handle(context,frame.get("params"));
                if (context.isCancelled()) error=new PublicError("cancelled","Request cancelled");
            } catch(PublicError publicError) { error=publicError; }
            catch(Throwable failure) {
                if (context.isCancelled()) error=new PublicError("cancelled","Request cancelled");
                else {
                    var panic=map("type","handler.panic","method",name,"value",failure.getMessage()==null?failure.toString():failure.getMessage());
                    family(panic,name); observe(panic);
                    error=new PublicError("internal","Internal error");
                }
            }
            requestEnded(frame,true,error); respond(frame,result,error,false);
          } finally {
            deadline.cancel(false); incoming.remove(id,context); context.cancel();
          }
        });
    }
    private void respond(Map<String,Object> request,Object result,PublicError error,boolean immediate) {
        var frame=map("version",1,"kind","response","id",request.get("id")); copyTrace(request,frame);
        if (error!=null) frame.put("error",error.value()); else frame.put("result",result);
        try { enqueue(frame,options.writeTimeout(),immediate); }
        catch(Exception failure) {
            if (ended.get()) return;
            frame.remove("result"); frame.put("error",new PublicError("internal","Response could not be encoded").value());
            try { enqueue(frame,options.writeTimeout(),immediate); } catch(Exception retry) { fail(); }
        }
    }
    private void eventLoop() {
        try {
            while (!ended.get()) {
                var frame=events.take(); String name=(String)frame.get("event");
                var context=new RequestContext(frame); eventObserved("event.delivered",frame);
                EventHandler handler=eventHandlers.get(name);
                if (handler!=null) handler.handle(context,frame.get("data")); else if(wire!=null) wire.event(name,context,frame);
                for (var listener:listeners) listener.accept(context,frame.get("data"));
            }
        } catch(Throwable failure) { if (!ended.get()) fail(); }
    }
    public void identity(String path,String digest) {
        var identity=identityValue(path,digest);
        handle("identity.check",(context,request) -> { var remote=readIdentity(request); compareIdentity(identity,remote); return identity; });
    }
    public CompletableFuture<Void> checkIdentity(String path,String digest,Duration timeout) {
        var expected=identityValue(path,digest); var done=new CompletableFuture<Void>();
        call(RequestContext.empty(),"identity.check",expected,timeout,null).result().whenComplete((result,failure) -> {
            try {
                if (failure instanceof PublicError p && p.code().equals("method_not_found")) { done.complete(null); return; }
                if (failure!=null) { done.completeExceptionally(failure); return; }
                compareIdentity(expected,readIdentity(result)); done.complete(null);
            } catch(Exception e) { done.completeExceptionally(e); }
        });
        return done;
    }
    private static Map<String,Object> identityValue(String path,String digest) {
        var value=map("path",path); if (digest!=null && !digest.isEmpty()) value.put("digest",digest); return readIdentity(value);
    }
    private static Map<String,Object> readIdentity(Object value) {
        if (!(value instanceof Map<?,?>)) throw new PublicError("contract_invalid","A declaration identity is an object");
        var identity=Json.object(value);
        if (!(identity.get("path") instanceof String p) || p.isEmpty() || !Json.validUnicode(p)
            || !java.util.Set.of("path","digest").containsAll(identity.keySet())
            || identity.containsKey("digest") && (!(identity.get("digest") instanceof String d) || !d.matches("[0-9a-f]{64}")))
            throw new PublicError("contract_invalid","Invalid declaration identity");
        return identity;
    }
    private static void compareIdentity(Map<String,Object> expected,Map<String,Object> actual) {
        if (!expected.get("path").equals(actual.get("path")) || expected.containsKey("digest") && actual.containsKey("digest") && !expected.get("digest").equals(actual.get("digest")))
            throw new PublicError("contract_mismatch","The declaration identity differs");
    }
    @Override public void close() { end(1000,""); }
    void end(int code,String reason) { if (finish(new CloseInfo(code,reason),true)) connection.close(code,reason); }
    void fail() {
        if (ended.get()) return;
        CloseInfo known=connection.closed().getNow(null);
        if (known==null) { if (finish(new CloseInfo(1006,""),true)) connection.abort(); }
        else finish(known,false);
    }
    private void fail(Exception failure) {
        // Receive/send can observe the chosen close before the transport's
        // separate completion notification. Its exception is already final.
        if (failure instanceof CloseException close) finish(new CloseInfo(close.code(),close.reason()),false);
        else fail();
    }
    private boolean finish(CloseInfo info,boolean local) {
        if (!ended.compareAndSet(false,true)) return false;
        for (Call call:pending.values()) call.complete(null,new PublicError("disconnected","Connection closed"),false);
        for (RequestContext context:incoming.values()) context.cancel();
        deadlines.shutdownNow();
        // An admitted transport write must finish under the close handshake's
        // own deadline. Interrupting it early can abort the socket before the
        // close frame is written; an idle writer is woken once closure settles.
        if (local) connection.closed().whenComplete((closedInfo,failure) -> {
            if (writer!=Thread.currentThread()) writer.interrupt();
        });
        for (Thread loop:loops) if (loop!=Thread.currentThread() && (!local || loop!=writer)) loop.interrupt();
        var observation=map("type","connection.closed","code",info.code(),"local",local);
        if (!info.reason().isEmpty()) observation.put("reason",info.reason());
        observe(observation); if(wire!=null) wire.ending(info); closed.complete(info);
        return true;
    }
    private void requestStarted(Map<String,Object> frame,boolean inbound) {
        if(options.observer()==null) return;
        observationStarts.put(inbound+":"+frame.get("id"),System.nanoTime());
        var value=map("type","request.started","id",frame.get("id"),"method",frame.get("method"),"incoming",inbound);
        family(value,(String)frame.get("method")); trace(value,frame); observe(value);
    }
    private void requestEnded(Map<String,Object> frame,boolean inbound,PublicError error) {
        if(options.observer()==null) return;
        Long started=observationStarts.remove(inbound+":"+frame.get("id"));
        var value=map("type","request.ended","id",frame.get("id"),"method",frame.get("method"),"incoming",inbound,"duration",started==null?0L:System.nanoTime()-started);
        String outcome=error==null?"ok":"error";
        if (error!=null && error.code().equals("request_timeout")) outcome="timeout";
        // Public cancelled refusals are errors; only a locally withdrawn request is cancellation.
        if (error!=null && error.code().equals("cancelled") && (inbound?incoming.get((String)frame.get("id"))!=null && incoming.get((String)frame.get("id")).isCancelled():error.getMessage().equals("The caller gave up"))) outcome="cancelled";
        value.put("outcome",outcome);
        if (error!=null) value.put("error_code",error.code());
        family(value,(String)frame.get("method")); trace(value,frame); observe(value);
    }
    private void frameObserved(String type,Map<String,Object> frame,int bytes) {
        var value=map("type",type,"kind",frame.get("kind"),"bytes",bytes);
        if (frame.containsKey("id")) value.put("id",frame.get("id"));
        String name=(String)frame.getOrDefault("method",frame.getOrDefault("event",""));
        if (!name.isEmpty()) { value.put("name",name); family(value,name); }
        trace(value,frame); observe(value);
    }
    private void eventObserved(String type,Map<String,Object> frame) {
        String name=(String)frame.get("event"); var value=map("type",type,"name",name,"bytes",Json.stringify(frame.get("data")).getBytes(StandardCharsets.UTF_8).length);
        family(value,name); trace(value,frame); observe(value);
    }
    private void backpressure(int queued,boolean stalled) { observe(map("type","backpressure","queued",queued,"stalled",stalled,"deadline",options.writeTimeout().toNanos())); }
    void observe(Map<String,Object> value) {
        if (options.observer()!=null) {
            var snapshot=new LinkedHashMap<>(value); snapshot.put("at",System.currentTimeMillis());
            try { options.observer().accept(snapshot); } catch(Throwable ignored) { /* Observation cannot strand protocol work. */ }
        }
    }
    private void family(Map<String,Object> value,String name) { String family=options.families().get(name); if (family!=null && !family.isEmpty()) value.put("family",family); }
    private static void trace(Map<String,Object> value,Map<String,Object> frame) {
        if (!(frame.get("traceparent") instanceof String p) || !Envelope.validTraceparent(p)) return;
        var trace=map("trace_id",p.substring(3,35),"span_id",p.substring(36,52),"flags",p.substring(53));
        if (frame.get("tracestate") instanceof String s && !s.isEmpty()) trace.put("state",s);
        value.put("trace",trace);
    }
    private static void inject(RequestContext context,Map<String,Object> frame) {
        String parent=context.traceparent(); byte[] span=new byte[8]; RANDOM.nextBytes(span);
        if (Envelope.validTraceparent(parent)) {
            frame.put("traceparent",parent.substring(0,36)+HexFormat.of().formatHex(span)+parent.substring(52));
            if (!context.tracestate().isEmpty()) frame.put("tracestate",context.tracestate());
        } else { byte[] trace=new byte[16]; RANDOM.nextBytes(trace); frame.put("traceparent","00-"+HexFormat.of().formatHex(trace)+"-"+HexFormat.of().formatHex(span)+"-01"); }
    }
    private static void copyTrace(Map<String,Object> source,Map<String,Object> destination) {
        for (String key:List.of("traceparent","tracestate")) if (source.containsKey(key)) destination.put(key,source.get(key));
    }
    private static void metadata(Map<String,String> meta,Map<String,Object> frame) {
        if (meta==null) return;
        var copy=new LinkedHashMap<String,Object>();
        meta.forEach((key,value) -> { if (!key.startsWith("nightseam.")) copy.put(key,value); });
        if (!copy.isEmpty()) frame.put("meta",copy);
    }
    public static PublicError asError(Throwable failure) {
        while (failure.getCause()!=null && (failure instanceof java.util.concurrent.ExecutionException || failure instanceof java.util.concurrent.CompletionException)) failure=failure.getCause();
        if (failure instanceof PublicError p) return p;
        if (failure instanceof TimeoutException) return new PublicError("timeout","Deadline passed");
        return new PublicError("failed",failure.getMessage()==null?failure.toString():failure.getMessage());
    }
    public static Map<String,Object> map(Object... entries) {
        var value=new LinkedHashMap<String,Object>();
        for(int i=0;i<entries.length;i+=2) value.put((String)entries[i],entries[i+1]);
        return value;
    }

}
