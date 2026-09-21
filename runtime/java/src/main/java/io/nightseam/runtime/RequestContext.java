package io.nightseam.runtime;

import java.util.Map;
import java.util.concurrent.CompletableFuture;
import java.util.LinkedHashMap;
import java.util.ArrayList;

/** Received carriage is separate from data and is never implicitly re-emitted. */
public final class RequestContext {
    private final Map<String,Object> frame;
    private final CompletableFuture<Void> cancelled = new CompletableFuture<>();
    private final Map<Object,Runnable> cancellationHandlers = new LinkedHashMap<>();
    private volatile boolean cancellationRequested;
    public RequestContext(Map<String,Object> frame) {
        this.frame = new LinkedHashMap<>(frame);
        if(frame.get("meta") instanceof Map<?,?> meta) this.frame.put("meta",new LinkedHashMap<>(meta));
    }
    public static RequestContext empty() { return new RequestContext(Map.of()); }
    public String id() { return (String)frame.getOrDefault("id", ""); }
    public String method() { return (String)frame.getOrDefault("method", ""); }
    public String event() { return (String)frame.getOrDefault("event", ""); }
    @SuppressWarnings("unchecked")
    public Map<String,String> metadata() {
        var meta=(Map<String,String>)(Map<?,?>)frame.get("meta");
        return meta==null?null:new LinkedHashMap<>(meta);
    }
    public String traceparent() { return (String)frame.getOrDefault("traceparent", ""); }
    public String tracestate() { return (String)frame.getOrDefault("tracestate", ""); }
    public boolean isCancelled() { return cancellationRequested; }
    public CompletableFuture<Void> cancellation() { return cancelled.copy(); }
    public void cancel() {
        ArrayList<Runnable> handlers;
        synchronized(cancellationHandlers) {
            if(cancellationRequested) return;
            cancellationRequested=true;
            handlers=new ArrayList<>(cancellationHandlers.values()); cancellationHandlers.clear();
        }
        cancelled.complete(null);
        for(Runnable handler:handlers) {
            try { handler.run(); } catch(Throwable ignored) { /* One listener cannot suppress other cancellations. */ }
        }
    }
    /** Register cancellation without retaining completed child calls for this context's lifetime. */
    public Runnable onCancel(Runnable handler) {
        Object key=new Object(); boolean immediate;
        synchronized(cancellationHandlers) {
            immediate=cancellationRequested;
            if(!immediate) cancellationHandlers.put(key,handler);
        }
        if(immediate) handler.run();
        return () -> { synchronized(cancellationHandlers) { cancellationHandlers.remove(key); } };
    }
    Map<String,Object> frame() { return frame; }
}
