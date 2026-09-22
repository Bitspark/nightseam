package io.nightseam.duplex;

import dev.bitspark.bitwire.*;
import java.util.*;

/** Explicit path routing over one borrowed Bitwire endpoint attachment. */
public final class Dispatcher implements Wire, AutoCloseable {
    private record Route(List<String> path, boolean prefix) {}
    private record Registration(Route route, Receiver receiver) {}
    private final Endpoint root;
    private final Map<Route,Registration> routes = new LinkedHashMap<>();
    private final WeakHashMap<ReturnAddress,Map<String,Registration>> calls = new WeakHashMap<>();
    private Runnable detach;
    private boolean ended;

    public Dispatcher(Endpoint root) {
        this.root = Objects.requireNonNull(root);
        Runnable acquired = root.receive(new Receiver(this::dispatch, this::close));
        synchronized (this) { if (!ended) { detach = acquired; return; } }
        acquired.run();
        throw new IllegalStateException("endpoint closed during attachment");
    }
    @Override public void send(List<String> path, Message message) {
        synchronized (this) { requireOpen(); }
        root.send(path, message);
    }
    public Runnable register(List<String> path, Receiver receiver) { return register(path, false, receiver); }
    public Runnable registerPrefix(List<String> path, Receiver receiver) { return register(path, true, receiver); }
    private synchronized Runnable register(List<String> path, boolean prefix, Receiver receiver) {
        requireOpen(); Wires.encodePath(path);
        var route = new Route(List.copyOf(path), prefix);
        var registration = new Registration(route, receiver);
        if (routes.putIfAbsent(route, registration) != null) throw new IllegalStateException("route already registered");
        return () -> { synchronized (Dispatcher.this) { routes.remove(route, registration); } };
    }
    private void dispatch(List<String> path, Message message) {
        Registration selected = null;
        synchronized (this) {
            if (ended) return;
            if (message.frame() instanceof ProfileFrame.Cancel cancel && message.returnAddress() != null) {
                var captured = calls.get(message.returnAddress());
                if (captured != null) selected = captured.remove(cancel.id());
            } else {
                selected = routes.get(new Route(path, false));
                if (selected == null) for (var candidate : routes.values()) {
                    var prefix = candidate.route().path();
                    if (candidate.route().prefix() && path.size() >= prefix.size()
                        && path.subList(0, prefix.size()).equals(prefix)
                        && (selected == null || prefix.size() > selected.route().path().size())) selected = candidate;
                }
                if (selected != null && message.frame() instanceof ProfileFrame.Request request && message.returnAddress() != null)
                    calls.computeIfAbsent(message.returnAddress(), key -> new HashMap<>()).put(request.id(), selected);
            }
        }
        if (selected != null && selected.receiver().message() != null) selected.receiver().message().accept(path, message);
        else if (message.frame() instanceof ProfileFrame.Request request && message.returnAddress() != null)
            message.returnAddress().wire().send(List.of(), new Message(new ProfileFrame.Response(request.id(),
                null, new ProfileError("method_not_found", "Unknown method"), request.traceparent(), request.tracestate()), null));
    }
    public Endpoint select(List<String> path) {
        Wires.encodePath(path);
        var prefix = List.copyOf(path);
        return new Endpoint() {
            private Object token;
            private Runnable release;
            private Receiver receiver;
            private boolean closed;
            @Override public void send(List<String> relative, Message message) {
                synchronized (this) { if (closed) throw new IllegalStateException("endpoint closed"); }
                var joined = new ArrayList<>(prefix); joined.addAll(relative);
                Dispatcher.this.send(joined, message);
            }
            @Override public synchronized Runnable receive(Receiver value) {
                if (closed || token != null) throw new IllegalStateException("endpoint closed or already attached");
                Object current = new Object(); token = current; receiver = value;
                try {
                    release = registerPrefix(prefix, new Receiver(value.message() == null ? null : (delivered, message) ->
                        value.message().accept(List.copyOf(delivered.subList(prefix.size(), delivered.size())), message), this::close));
                } catch (RuntimeException failure) { token = null; receiver = null; throw failure; }
                return () -> { synchronized (this) {
                    if (token != current) return;
                    token = null; receiver = null; Runnable held = release; release = null; held.run();
                } };
            }
            @Override public void close(int code, String reason) {
                Receiver held; Runnable off;
                synchronized (this) {
                    if (closed) return;
                    closed = true; token = null; held = receiver; receiver = null; off = release; release = null;
                }
                if (off != null) off.run();
                if (held != null && held.closed() != null) held.closed().accept(code, reason);
            }
        };
    }
    private void requireOpen() { if (ended) throw new IllegalStateException("dispatcher closed"); }
    public void close(int code, String reason) {
        List<Registration> held; Runnable off;
        synchronized (this) {
            if (ended) return;
            ended = true; held = List.copyOf(routes.values()); routes.clear(); calls.clear(); off = detach; detach = null;
        }
        if (off != null) off.run();
        for (var registration : held) if (registration.receiver().closed() != null) registration.receiver().closed().accept(code, reason);
    }
    @Override public void close() { close(1000, "done"); }
}
