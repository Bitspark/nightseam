package io.nightseam.runtime;

import io.nightseam.duplex.Message;
import io.nightseam.duplex.Receiver;
import io.nightseam.duplex.Wire;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.ScheduledThreadPoolExecutor;
import java.util.concurrent.TimeUnit;

/** A bounded asynchronous local carrier with two relative origins. */
public final class WirePair implements AutoCloseable {
    private final Object lock = new Object();
    private final PeerOptions options;
    private final Endpoint left = new Endpoint();
    private final Endpoint right = new Endpoint();
    private final ScheduledThreadPoolExecutor deadlines = new ScheduledThreadPoolExecutor(1,
        Thread.ofPlatform().daemon().name("nightseam-wire-deadline").factory());
    private boolean closed;

    private WirePair(PeerOptions options) {
        this.options = Objects.requireNonNull(options, "options");
        // Completed requests must not leave cancelled timers retained until
        // their former deadline; their count would escape the pending bound.
        deadlines.setRemoveOnCancelPolicy(true);
        left.other = right;
        right.other = left;
        Thread.ofVirtual().name("nightseam-wire-left").start(left::run);
        Thread.ofVirtual().name("nightseam-wire-right").start(right::run);
    }

    public static WirePair create(PeerOptions options) { return new WirePair(options); }
    public static WirePair create() { return create(PeerOptions.defaults()); }
    public Wire left() { return left; }
    public Wire right() { return right; }
    @Override public void close() { end(1000, "done"); }

    private record Route(List<String> path, boolean namespace) {}
    private record Registration(List<String> path, Receiver receiver) {}
    private record Delivery(List<String> path, Message message, Call call, String refusal) {}

    // Wire implementations may override equals: capability identity is always
    // object identity, independently of a caller's request identifier.
    private static final class ReturnKey {
        final Wire address;
        final Object id;
        ReturnKey(Wire address, Object id) { this.address = address; this.id = id; }
        @Override public int hashCode() { return 31 * System.identityHashCode(address) + id.hashCode(); }
        @Override public boolean equals(Object value) {
            return value instanceof ReturnKey key && key.address == address && key.id.equals(id);
        }
    }

    private final class Call {
        final ReturnKey key;
        final List<String> path;
        final Message original;
        final Wire returning;
        Registration registration;
        ScheduledFuture<?> timer;
        boolean completed;
        boolean responded;
        boolean active;
        boolean cancelQueued;
        boolean cancelled;

        Call(Endpoint endpoint, ReturnKey key, List<String> path, Message original) {
            this.key = key;
            this.path = path;
            this.original = original;
            this.returning = new Return(endpoint, this);
        }
    }

    private final class Endpoint implements Wire {
        Endpoint other;
        final ArrayDeque<Delivery> queue = new ArrayDeque<>();
        final Map<ReturnKey,Call> calls = new HashMap<>();
        final Map<Route,Registration> receivers = new HashMap<>();
        int dataQueued;
        int active;
        ScheduledFuture<?> eventTimer;
        long eventGeneration;
        boolean eventRunning;

        @Override public void send(List<String> path, Message message) {
            List<String> copiedPath = List.copyOf(path);
            var frame = Wires.validateFrame(copiedPath, message.frame(), options.maxFrameBytes());
            String kind = (String) frame.get("kind");
            if (!List.of("request", "event", "cancel").contains(kind))
                throw new IllegalArgumentException("send responses to their return address");
            if (!kind.equals("event") && message.returnAddress() == null)
                throw new IllegalArgumentException("requests and cancels require a return address");
            other.admit(copiedPath, new Message(frame, message.returnAddress()));
        }

        void admit(List<String> path, Message message) {
            String kind = (String) message.frame().get("kind");
            synchronized (lock) {
                requireOpen();
                Call call = null;
                String refusal = null;
                if (kind.equals("cancel")) {
                    call = calls.get(new ReturnKey(message.returnAddress(), message.frame().get("id")));
                    if (call == null || call.completed || call.cancelQueued || call.cancelled) return;
                    call.cancelQueued = true;
                    message = new Message(message.frame(), call.returning);
                } else {
                    if (dataQueued >= options.queueCapacity()) {
                        // Carrier teardown/callbacks occur outside this lock.
                        int depth = dataQueued;
                        Thread.ofVirtual().start(() -> {
                            observe(Map.of("kind", "backpressure", "queued", depth, "stalled", true));
                        });
                        end(1008, "local wire queue limit reached");
                        throw new IllegalStateException("local wire backpressure");
                    }
                    dataQueued++;
                    if (kind.equals("request")) {
                        var key = new ReturnKey(message.returnAddress(), message.frame().get("id"));
                        if (calls.containsKey(key)) refusal = "invalid_message";
                        else if (calls.size() >= options.maxPendingRequests()) refusal = "busy";
                        else {
                            call = new Call(this, key, path, message);
                            calls.put(key, call);
                            message = new Message(message.frame(), call.returning);
                        }
                    }
                }
                queue.add(new Delivery(path, message, call, refusal));
                lock.notifyAll();
            }
        }

        @Override public Runnable receive(List<String> path, Receiver receiver) {
            io.nightseam.duplex.Wires.encodePath(path);
            Objects.requireNonNull(receiver.message(), "wire receiver callback");
            var route = new Route(List.copyOf(path), receiver.namespace());
            var registration = new Registration(route.path(), receiver);
            synchronized (lock) {
                requireOpen();
                if (receivers.putIfAbsent(route, registration) != null)
                    throw new IllegalStateException("wire receiver already registered");
            }
            return () -> { synchronized (lock) { receivers.remove(route, registration); } };
        }

        @Override public void close(int code, String reason) { end(code, reason); }

        Registration match(List<String> path) {
            Registration exact = receivers.get(new Route(path, false));
            if (exact != null) return exact;
            Registration best = null;
            for (var entry : receivers.entrySet()) {
                var prefix = entry.getKey().path();
                if (entry.getKey().namespace() && prefix.size() <= path.size()
                    && path.subList(0, prefix.size()).equals(prefix)
                    && (best == null || prefix.size() > best.path().size())) best = entry.getValue();
            }
            return best;
        }

        void run() {
            try {
                while (true) {
                    Delivery delivery;
                    synchronized (lock) {
                        while (!closed && queue.isEmpty()) lock.wait();
                        if (closed) return;
                        delivery = queue.remove();
                        if (!"cancel".equals(delivery.message().frame().get("kind"))) dataQueued--;
                    }
                    dispatch(delivery);
                }
            } catch (InterruptedException interrupted) {
                Thread.currentThread().interrupt();
                end(1001, "wire dispatcher interrupted");
            }
        }

        void dispatch(Delivery delivery) {
            if (delivery.refusal() != null) {
                Wires.refuse(delivery.message(), delivery.refusal(), delivery.refusal().equals("busy")
                    ? "Outstanding call limit reached" : "Duplicate active request identifier");
                return;
            }
            String kind = (String) delivery.message().frame().get("kind");
            if (kind.equals("cancel")) { deliverCancel(delivery.call(), delivery.message()); return; }
            Registration registration;
            String refusal = null;
            synchronized (lock) {
                if (closed) return;
                registration = match(delivery.path());
                Call call = delivery.call();
                if (call != null) {
                    if (registration == null) refusal = "method_not_found";
                    else if (active >= options.maxConcurrentHandlers()) refusal = "busy";
                    else {
                        call.registration = registration;
                        call.active = true;
                        active++;
                        call.timer = deadlines.schedule(() -> timeout(call), options.requestTimeout().toNanos(), TimeUnit.NANOSECONDS);
                    }
                }
                if (kind.equals("event") && registration != null) {
                    long generation = ++eventGeneration;
                    eventRunning = true;
                    eventTimer = deadlines.schedule(() -> {
                        int depth;
                        synchronized (lock) {
                            if (closed || !eventRunning || eventGeneration != generation) return;
                            eventRunning = false;
                            depth = dataQueued;
                        }
                        end(1008, "local wire event consumer stalled");
                        observe(Map.of("kind", "backpressure", "queued", depth, "stalled", true));
                    }, options.writeTimeout().toNanos(), TimeUnit.NANOSECONDS);
                }
            }
            if (refusal != null) {
                Wires.refuse(delivery.message(), refusal, refusal.equals("busy")
                    ? "Too many concurrent requests" : "Unknown method");
                return;
            }
            if (registration == null) return;
            deliver(registration, delivery.path(), delivery.message());
            if (kind.equals("event")) {
                synchronized (lock) {
                    eventRunning = false;
                    if (eventTimer != null) eventTimer.cancel(false);
                    eventTimer = null;
                }
            }
        }

        void deliver(Registration registration, List<String> path, Message message) {
            try {
                registration.receiver().message().accept(path, message);
            } catch (Throwable failure) {
                observe(Map.of("kind", "handler_panic", "method", io.nightseam.duplex.Wires.encodePath(path)));
                if ("request".equals(message.frame().get("kind"))) Wires.refuse(message, "internal", "Internal error");
                else end(1008, "wire event receiver failed");
            }
        }

        void deliverCancel(Call call, Message message) {
            Registration registration;
            synchronized (lock) {
                call.cancelQueued = false;
                call.cancelled = true;
                registration = call.completed ? null : call.registration;
                retire(call);
            }
            if (registration != null) deliver(registration, call.path, message);
        }

        void timeout(Call call) {
            boolean respond;
            synchronized (lock) {
                if (closed || call.completed) return;
                if (!call.cancelQueued && !call.cancelled) {
                    call.cancelQueued = true;
                    var cancel = new Message(Map.of("version", 1, "kind", "cancel", "id", call.key.id), call.returning);
                    queue.add(new Delivery(call.path, cancel, call, null));
                    lock.notifyAll();
                }
                respond = !call.responded;
                call.responded = true;
            }
            // Deadline completion must not release active work until its actual
            // response: an uncooperative handler cannot replenish its budget.
            if (respond) Wires.refuse(call.original, "cancelled", "Request cancelled");
        }

        void complete(Call call) {
            synchronized (lock) {
                if (!call.completed) {
                    call.completed = true;
                    if (call.active) { active--; call.active = false; }
                    if (call.timer != null) call.timer.cancel(false);
                }
                retire(call);
            }
        }

        void retire(Call call) {
            if (call.completed && !call.cancelQueued) calls.remove(call.key, call);
        }
    }

    private final class Return implements Wire {
        private final Endpoint endpoint;
        private final Call call;
        Return(Endpoint endpoint, Call call) { this.endpoint = endpoint; this.call = call; }

        @Override public void send(List<String> path, Message message) {
            if (!path.isEmpty() || !"response".equals(message.frame().get("kind"))
                || !Objects.equals(message.frame().get("id"), call.key.id))
                throw new IllegalArgumentException("invalid wire response");
            var frame = Wires.validateFrame(path, message.frame(), options.maxFrameBytes());
            try {
                synchronized (lock) {
                    if (call.responded || call.completed) throw new IllegalStateException("wire request already ended");
                    call.responded = true;
                }
                call.key.address.send(path, new Message(frame, message.returnAddress()));
            } finally {
                endpoint.complete(call);
            }
        }
        @Override public Runnable receive(List<String> path, Receiver receiver) {
            throw new IllegalStateException("return addresses cannot receive registrations");
        }
        @Override public void close(int code, String reason) { endpoint.complete(call); }
    }

    private void requireOpen() { if (closed) throw new IllegalStateException("wire pair closed"); }

    private void observe(Map<String,Object> event) {
        if (options.observer() != null) {
            try { options.observer().accept(event); } catch (Throwable ignored) { }
        }
    }

    private void end(int code, String reason) {
        var receivers = new ArrayList<Receiver>();
        var requests = new ArrayList<Message>();
        synchronized (lock) {
            if (closed) return;
            closed = true;
            for (Endpoint endpoint : List.of(left, right)) {
                if (endpoint.eventTimer != null) endpoint.eventTimer.cancel(false);
                endpoint.eventRunning = false;
                for (Registration registration : endpoint.receivers.values()) receivers.add(registration.receiver());
                for (Call call : endpoint.calls.values()) {
                    if (call.timer != null) call.timer.cancel(false);
                    if (!call.responded) requests.add(call.original);
                    call.responded = true;
                    call.completed = true;
                }
                endpoint.receivers.clear();
                endpoint.calls.clear();
                endpoint.queue.clear();
                endpoint.dataQueued = 0;
                endpoint.active = 0;
            }
            lock.notifyAll();
        }
        deadlines.shutdownNow();
        Thread.ofVirtual().name("nightseam-wire-close").start(() -> {
            observe(Map.of("kind", "connection_closed", "code", code, "reason", reason, "local", true));
            for (Receiver receiver : receivers) {
                if (receiver.closed() != null) {
                    try { receiver.closed().accept(code, reason); } catch (Throwable ignored) { }
                }
            }
            for (Message request : requests) Wires.refuse(request, "disconnected", "Connection ended; outcome may be unknown");
        });
    }
}
