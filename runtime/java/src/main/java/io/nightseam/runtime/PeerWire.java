package io.nightseam.runtime;

import io.nightseam.duplex.CloseInfo;
import io.nightseam.duplex.Message;
import io.nightseam.duplex.Receiver;
import io.nightseam.duplex.Wire;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

/** The peer's ordered local admission boundary, sharing its existing carrier. */
final class PeerWire implements Wire {
    private final Peer peer;
    private final PeerOptions options;
    private final Object lock = new Object();
    private final ArrayDeque<Delivery> queue = new ArrayDeque<>();
    private final Map<ReturnKey,Outgoing> outgoing = new HashMap<>();
    private final Map<Route,Registration> receivers = new HashMap<>();
    private final Set<Incoming> incoming = new HashSet<>();
    private int dataQueued;
    private boolean ended;

    PeerWire(Peer peer) {
        this.peer = peer;
        this.options = peer.options();
        peer.closed().thenAccept(this::ending);
        Thread.ofVirtual().name("nightseam-peer-wire").start(this::run);
    }

    private record Route(List<String> path, boolean namespace) {}
    private record Registration(List<String> path, Receiver receiver) {}
    private enum Kind { REQUEST, EVENT, CANCEL, RESPONSE, INCOMING_CANCEL }
    private record Delivery(Kind kind, List<String> path, Message message,
        Outgoing outgoing, Incoming incoming, String refusal) {}

    private static final class ReturnKey {
        final Wire address;
        final String id;
        ReturnKey(Wire address, String id) { this.address = address; this.id = id; }
        @Override public int hashCode() { return 31 * System.identityHashCode(address) + id.hashCode(); }
        @Override public boolean equals(Object other) {
            return other instanceof ReturnKey key && address == key.address && id.equals(key.id);
        }
    }

    private static final class Outgoing {
        final ReturnKey key;
        final Message original;
        Peer.Call call;
        boolean completed;
        boolean cancelQueued;
        boolean cancelled;
        boolean responseQueued;
        boolean responded;
        Object result;
        Throwable failure;
        Outgoing(ReturnKey key, Message original) { this.key = key; this.original = original; }
    }

    private final class Incoming {
        final Registration registration;
        final List<String> path;
        final RequestContext context;
        final CompletableFuture<Object> result = new CompletableFuture<>();
        final Wire returning;
        boolean started;
        boolean cancelled;
        boolean cancelQueued;
        boolean cancelDelivered;
        boolean responded;
        boolean completed;

        Incoming(Registration registration, List<String> path, RequestContext context) {
            this.registration = registration;
            this.path = path;
            this.context = context;
            returning = new Wire() {
                @Override public void send(List<String> path, Message message) {
                    if (!path.isEmpty() || !"response".equals(message.frame().get("kind"))
                        || !context.id().equals(message.frame().get("id")))
                        throw new IllegalArgumentException("invalid wire response");
                    Map<String,Object> frame = Wires.validateFrame(path, message.frame(), options.maxFrameBytes());
                    synchronized (lock) {
                        if (ended || cancelled || responded || result.isDone()) throw new IllegalStateException("wire response already ended");
                        responded = true;
                    }
                    if (frame.containsKey("error")) result.completeExceptionally(PublicError.from(Json.object(frame.get("error"))));
                    else result.complete(frame.get("result"));
                }
                @Override public Runnable receive(List<String> path, Receiver receiver) {
                    throw new IllegalStateException("return addresses cannot receive registrations");
                }
                @Override public void close(int code, String reason) {
                    synchronized (lock) {
                        if (responded || result.isDone()) return;
                        responded = true;
                    }
                    result.completeExceptionally(new PublicError("disconnected", "Return address closed"));
                }
            };
        }
    }

    @Override public void send(List<String> path, Message message) {
        List<String> copied = List.copyOf(path);
        Map<String,Object> frame = Wires.validateFrame(copied, message.frame(), options.maxFrameBytes());
        String kind = (String)frame.get("kind");
        if (!List.of("request", "event", "cancel").contains(kind))
            throw new IllegalArgumentException("send a response to its return address");
        if (!kind.equals("event") && message.returnAddress() == null)
            throw new IllegalArgumentException("requests and cancels require a return address");
        Message snapshot = new Message(frame, message.returnAddress());
        boolean overflow = false;
        synchronized (lock) {
            requireOpen();
            Outgoing state = null;
            String refusal = null;
            if (kind.equals("cancel")) {
                state = outgoing.get(new ReturnKey(message.returnAddress(), (String)frame.get("id")));
                if (state == null || state.completed || state.cancelQueued || state.cancelled) return;
                state.cancelQueued = true;
            } else if (dataQueued >= options.queueCapacity()) {
                overflow = true;
            } else {
                dataQueued++;
                if (kind.equals("request")) {
                    var key = new ReturnKey(message.returnAddress(), (String)frame.get("id"));
                    if (outgoing.containsKey(key)) refusal = "invalid_message";
                    else if (outgoing.size() >= options.maxPendingRequests()) refusal = "busy";
                    else {
                        state = new Outgoing(key, snapshot);
                        outgoing.put(key, state);
                    }
                }
            }
            if (!overflow) {
                queue.add(new Delivery(kind.equals("request") ? Kind.REQUEST : kind.equals("event") ? Kind.EVENT : Kind.CANCEL,
                    copied, snapshot, state, null, refusal));
                lock.notifyAll();
            }
        }
        if (overflow) {
            fail(true);
            throw new IllegalStateException("peer wire backpressure");
        }
    }

    private void run() {
        try {
            while (true) {
                Delivery delivery;
                synchronized (lock) {
                    while (!ended && queue.isEmpty()) lock.wait();
                    if (ended) return;
                    delivery = queue.remove();
                    if (delivery.kind() == Kind.REQUEST || delivery.kind() == Kind.EVENT) dataQueued--;
                }
                dispatch(delivery);
            }
        } catch (InterruptedException interrupted) {
            Thread.currentThread().interrupt();
            close(1001, "peer wire dispatcher interrupted");
        }
    }

    @SuppressWarnings("unchecked")
    private void dispatch(Delivery delivery) {
        if (delivery.refusal() != null) {
            Wires.refuse(delivery.message(), delivery.refusal(), delivery.refusal().equals("busy")
                ? "Outstanding call limit reached" : "Duplicate active request identifier");
            return;
        }
        if (delivery.kind() == Kind.RESPONSE) { deliverResponse(delivery.outgoing()); return; }
        if (delivery.kind() == Kind.INCOMING_CANCEL) { deliverIncomingCancel(delivery.incoming()); return; }
        if (delivery.kind() == Kind.CANCEL) {
            Peer.Call cancel = null;
            synchronized (lock) {
                Outgoing state = delivery.outgoing();
                state.cancelQueued = false;
                state.cancelled = true;
                if (!state.completed) cancel = state.call;
                retire(state);
            }
            if (cancel != null) cancel.cancel();
            return;
        }
        Map<String,Object> frame = delivery.message().frame();
        String name = io.nightseam.duplex.Wires.encodePath(delivery.path());
        Map<String,String> metadata = (Map<String,String>)(Map<?,?>)frame.get("meta");
        if (delivery.kind() == Kind.EVENT) {
            try { peer.emitCarried(RequestContext.empty(), name, frame.get("data"), metadata, options.writeTimeout(), frame, true); }
            catch (Exception failure) { fail(false); }
            return;
        }
        Outgoing state = delivery.outgoing();
        try {
            Peer.Call call = peer.begin(RequestContext.empty(), name, frame.get("params"), null, metadata, frame, true);
            synchronized (lock) { state.call = call; }
            // This callback only queues a reserved completion. It never invokes
            // application return code on send(), the reader, or a timer thread.
            call.result().whenComplete((result, failure) -> complete(state, result, failure));
        } catch (RuntimeException failure) { complete(state, null, failure); }
    }

    private void complete(Outgoing state, Object result, Throwable failure) {
        synchronized (lock) {
            if (ended || state.completed) return;
            state.completed = true;
            state.responseQueued = true;
            state.result = result;
            state.failure = failure;
            queue.add(new Delivery(Kind.RESPONSE, List.of(), null, state, null, null));
            lock.notifyAll();
        }
    }

    private void deliverResponse(Outgoing state) {
        synchronized (lock) {
            state.responseQueued = false;
            if (state.responded || ended) { retire(state); return; }
            state.responded = true;
            retire(state);
        }
        var frame = new LinkedHashMap<String,Object>();
        frame.put("version", 1); frame.put("kind", "response"); frame.put("id", state.key.id);
        copyTrace(state.original.frame(), frame);
        if (state.failure == null) frame.put("result", state.result);
        else {
            PublicError error = Peer.asError(state.failure);
            if (error.code().equals("request_timeout")) error = new PublicError("cancelled", "Request cancelled");
            frame.put("error", error.value());
        }
        try { state.key.address.send(List.of(), new Message(frame, null)); }
        catch (Throwable ignored) { }
    }

    private void retire(Outgoing state) {
        // Queued completion and cancellation each retain their original request
        // reservation, so repeated completed calls cannot grow control storage.
        if (state.completed && !state.cancelQueued && !state.responseQueued) outgoing.remove(state.key, state);
    }

    @Override public Runnable receive(List<String> path, Receiver receiver) {
        String name = io.nightseam.duplex.Wires.encodePath(path);
        if (receiver.message() == null || name.isEmpty() && !receiver.namespace())
            throw new IllegalArgumentException("a peer receiver needs a callback and operation path or namespace");
        var route = new Route(List.copyOf(path), receiver.namespace());
        var registration = new Registration(route.path(), receiver);
        synchronized (peer) { synchronized (lock) {
            requireOpen();
            if (!receiver.namespace() && peer.hasHandlerOrEvent(name))
                throw new IllegalStateException("wire operation already has a raw handler");
            if (receivers.putIfAbsent(route, registration) != null) throw new IllegalStateException("wire receiver already registered");
        } }
        return () -> { synchronized (lock) { receivers.remove(route, registration); } };
    }

    boolean hasExactReceiver(String name) {
        List<String> decoded=path(name);
        if(decoded==null) return false;
        synchronized(lock) { return receivers.containsKey(new Route(decoded,false)); }
    }

    private Registration match(List<String> path) {
        synchronized (lock) {
            if (ended) return null;
            Registration exact = receivers.get(new Route(path, false));
            if (exact != null) return exact;
            for (int size = path.size(); size >= 0; size--) {
                Registration candidate = receivers.get(new Route(path.subList(0, size), true));
                if (candidate != null) return candidate;
            }
            return null;
        }
    }

    private static List<String> path(String name) {
        try { return io.nightseam.duplex.Wires.decodePath(name); }
        catch (IllegalArgumentException invalid) { return null; }
    }

    Peer.Handler handler(String name) {
        List<String> path = path(name);
        if (path == null) return null;
        Registration registration = match(path);
        if (registration == null) return null;
        return (context, params) -> receiveRequest(registration, path, context);
    }

    private Object receiveRequest(Registration registration, List<String> path, RequestContext context) throws Exception {
        Incoming call = new Incoming(registration, path, context);
        synchronized (lock) {
            requireOpen();
            if (context.isCancelled()) throw new PublicError("cancelled", "Request cancelled");
            if (incoming.size() >= options.maxConcurrentHandlers()) throw new PublicError("busy", "Too many concurrent requests");
            incoming.add(call);
        }
        context.cancellation().thenRun(() -> cancelIncoming(call));
        var frame = new LinkedHashMap<>(context.frame());
        frame.remove("method");
        try {
            // The peer already owns one bounded handler worker for this request.
            registration.receiver().message().accept(path, new Message(frame, call.returning));
            synchronized (lock) { call.started = true; enqueueIncomingCancel(call); }
            return call.result.get(options.requestTimeout().toNanos(), TimeUnit.NANOSECONDS);
        } catch (ExecutionException failure) {
            throw Peer.asError(failure);
        } catch (TimeoutException timeout) {
            context.cancel();
            throw new PublicError("cancelled", "Request cancelled");
        } finally {
            synchronized (lock) {
                call.completed = true;
                if (!call.cancelQueued) incoming.remove(call);
            }
        }
    }

    private void cancelIncoming(Incoming call) {
        synchronized (lock) {
            if (ended || call.completed || call.responded) return;
            call.cancelled = true;
            enqueueIncomingCancel(call);
        }
        call.result.completeExceptionally(new PublicError("cancelled", "Request cancelled"));
    }

    private void enqueueIncomingCancel(Incoming call) {
        if (!ended && call.started && call.cancelled && !call.cancelQueued && !call.cancelDelivered && !call.responded) {
            call.cancelQueued = true;
            queue.add(new Delivery(Kind.INCOMING_CANCEL, call.path, null, null, call, null));
            lock.notifyAll();
        }
    }

    private void deliverIncomingCancel(Incoming call) {
        boolean deliver;
        synchronized (lock) {
            call.cancelQueued = false;
            call.cancelDelivered = true;
            deliver = !ended && !call.responded;
            if (call.completed) incoming.remove(call);
        }
        if (!deliver) return;
        var frame = new LinkedHashMap<String,Object>();
        frame.put("version", 1); frame.put("kind", "cancel"); frame.put("id", call.context.id());
        copyTrace(call.context.frame(), frame);
        try { call.registration.receiver().message().accept(call.path, new Message(frame, call.returning)); }
        catch (Throwable ignored) { }
    }

    void event(String name, RequestContext context, Map<String,Object> frame) {
        List<String> path = path(name);
        if (path == null) return;
        Registration registration = match(path);
        if (registration == null) return;
        var message = new LinkedHashMap<>(frame);
        message.remove("event");
        // The peer's bounded event loop owns this callback, preserving its order.
        registration.receiver().message().accept(path, new Message(message, null));
    }

    private void requireOpen() {
        if (ended || peer.closed().isDone()) throw new IllegalStateException("peer wire closed");
    }

    void ending(CloseInfo info) {
        var registrations = new ArrayList<Registration>();
        var pending = new ArrayList<Message>();
        synchronized (lock) {
            if (ended) return;
            ended = true;
            registrations.addAll(receivers.values());
            for (Outgoing state : outgoing.values()) {
                if (!state.responded) { pending.add(state.original); state.responded = true; }
            }
            for (Incoming call : incoming) call.result.completeExceptionally(new PublicError("disconnected", "Connection closed"));
            receivers.clear(); outgoing.clear(); incoming.clear(); queue.clear(); dataQueued = 0;
            lock.notifyAll();
        }
        Thread.ofVirtual().name("nightseam-peer-wire-close").start(() -> {
            for (Registration registration : registrations) {
                if (registration.receiver().closed() != null) {
                    try { registration.receiver().closed().accept(info.code(), info.reason()); }
                    catch (Throwable ignored) { }
                }
            }
            for (Message request : pending) Wires.refuse(request, "disconnected", "Connection ended; outcome may be unknown");
        });
    }

    private void fail(boolean pressure) {
        int depth;
        synchronized (lock) { depth = dataQueued; }
        ending(new CloseInfo(1006, ""));
        Thread.ofVirtual().name("nightseam-peer-wire-failure").start(() -> {
            if (pressure) peer.observe(Map.of("type", "backpressure", "queued", depth, "stalled", true, "deadline", options.writeTimeout().toNanos()));
            peer.fail();
        });
    }

    @Override public void close(int code, String reason) {
        ending(new CloseInfo(code, reason));
        peer.end(code, reason);
    }

    private static void copyTrace(Map<String,Object> source, Map<String,Object> destination) {
        for (String key : List.of("traceparent", "tracestate"))
            if (source.containsKey(key)) destination.put(key, source.get(key));
    }
}
