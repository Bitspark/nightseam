package io.nightseam.conformance;

import io.nightseam.duplex.Message;
import io.nightseam.duplex.Receiver;
import io.nightseam.duplex.Wire;
import io.nightseam.duplex.Wires;
import java.time.Duration;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.locks.ReentrantLock;

/** A consumer record/follow composition, used only as an acceptance witness. */
public final class RecordedWire {
    private RecordedWire() {}

    /** Exercise both linearization cuts and a stalled subscriber, within one deadline. */
    public static Map<String,Object> witness(Duration within) {
        var deadline = new Deadline(within);
        return Map.of("cases", List.of(headCase(deadline, false), headCase(deadline, true)),
            "stalled", stallCase(deadline));
    }

    private record Entry(List<String> path, Message message, int sequence) {}
    private record Attachment(int head, Follower follower) {}

    private static final class Deadline {
        final long end;
        Deadline(Duration within) {
            if (within.isNegative() || within.isZero()) throw new IllegalArgumentException("deadline must be positive");
            end = System.nanoTime() + within.toNanos();
        }
        long remaining() {
            long remaining = end - System.nanoTime();
            if (remaining <= 0) throw new IllegalStateException("recorded wire witness timeout");
            return remaining;
        }
        <T> T take(BlockingQueue<T> queue) {
            try {
                T value = queue.poll(remaining(), TimeUnit.NANOSECONDS);
                if (value == null) throw new IllegalStateException("recorded wire witness timeout");
                return value;
            } catch (InterruptedException interrupted) {
                Thread.currentThread().interrupt();
                throw new IllegalStateException("recorded wire witness interrupted", interrupted);
            }
        }
        void await(CountDownLatch signal) {
            try {
                if (!signal.await(remaining(), TimeUnit.NANOSECONDS))
                    throw new IllegalStateException("recorded wire witness timeout");
            } catch (InterruptedException interrupted) {
                Thread.currentThread().interrupt();
                throw new IllegalStateException("recorded wire witness interrupted", interrupted);
            }
        }
    }

    private static final class Store implements Wire, AutoCloseable {
        final ReentrantLock append = new ReentrantLock();
        final List<Entry> entries = new ArrayList<>();
        final Set<Follower> followers = new HashSet<>();
        final AtomicBoolean callbacksOutsideAppend = new AtomicBoolean(true);
        final AtomicInteger callbacks = new AtomicInteger();

        int head() {
            append.lock();
            try { return entries.size(); } finally { append.unlock(); }
        }

        void callback() {
            // Reentrancy alone cannot detect Java's reentrant monitor. Observe
            // whether the application callback actually owns append exclusion.
            if (append.isHeldByCurrentThread()) callbacksOutsideAppend.set(false);
            head();
            callbacks.incrementAndGet();
        }

        @Override public void send(List<String> path, Message message) {
            append.lock();
            try {
                var entry = new Entry(List.copyOf(path), message, entries.size() + 1);
                entries.add(entry);
                var iterator = followers.iterator();
                while (iterator.hasNext()) {
                    Follower follower = iterator.next();
                    if (!follower.live.offer(entry)) {
                        iterator.remove();
                        follower.stop(); // Signal only; its writer owns closure.
                    }
                }
            } finally { append.unlock(); }
        }

        Attachment attach(int after, Wire target, int bound, boolean pause) {
            var follower = new Follower(target, bound);
            List<Entry> history;
            int head;
            append.lock();
            try {
                head = entries.size();
                history = List.copyOf(entries.subList(after, head));
                followers.add(follower);
            } finally { append.unlock(); }
            follower.start(history, pause);
            return new Attachment(head, follower);
        }

        @Override public Runnable receive(List<String> path, Receiver receiver) {
            throw new IllegalArgumentException("append store has no receive route");
        }
        @Override public void close(int code, String reason) {
            append.lock();
            try { followers.forEach(Follower::stop); followers.clear(); }
            finally { append.unlock(); }
        }
        @Override public void close() { close(1000, "done"); }
    }

    private static final class Follower implements AutoCloseable {
        final Wire target;
        final BlockingQueue<Entry> live;
        final BlockingQueue<Integer> sent = new ArrayBlockingQueue<>(32);
        final CountDownLatch paused = new CountDownLatch(1);
        final CountDownLatch resume = new CountDownLatch(1);
        final CountDownLatch done = new CountDownLatch(1);
        final AtomicBoolean stopped = new AtomicBoolean();
        volatile Thread writer;

        Follower(Wire target, int bound) {
            this.target = target;
            live = new ArrayBlockingQueue<>(bound);
        }
        void start(List<Entry> history, boolean pause) {
            writer = Thread.ofVirtual().name("recorded-wire-follower").unstarted(() -> {
                try {
                    for (int index = 0; index < history.size(); index++) {
                        if (!send(history.get(index))) return;
                        if (index == 0 && pause) {
                            paused.countDown();
                            resume.await();
                        }
                    }
                    while (!stopped.get()) if (!send(live.take())) return;
                } catch (InterruptedException interrupted) {
                    Thread.currentThread().interrupt();
                } finally {
                    target.close(1008, "recorded handoff ended");
                    done.countDown();
                }
            });
            writer.start();
            if (stopped.get()) writer.interrupt();
        }
        boolean send(Entry entry) {
            if (stopped.get()) return false;
            try { target.send(entry.path(), entry.message()); }
            catch (RuntimeException failure) { return false; }
            if (!sent.offer(entry.sequence())) throw new IllegalStateException("witness sent evidence full");
            return true;
        }
        void through(Deadline deadline, int sequence) {
            while (deadline.take(sent) != sequence) { }
        }
        void stop() {
            if (stopped.compareAndSet(false, true) && writer != null) writer.interrupt();
        }
        @Override public void close() {
            stop();
            new Deadline(Duration.ofSeconds(2)).await(done);
        }
    }

    // A fixture-only asynchronous root. Production selection and mounting
    // route into it; it dispatches the original root-relative path unchanged.
    private static final class Root implements Wire, AutoCloseable {
        final Map<List<String>,Receiver> receivers = new HashMap<>();
        final BlockingQueue<Entry> queue = new ArrayBlockingQueue<>(16);
        final AtomicBoolean stopped = new AtomicBoolean();
        final CountDownLatch done = new CountDownLatch(1);
        final Thread dispatcher;

        Root() {
            dispatcher = Thread.ofVirtual().name("recorded-wire-root").start(() -> {
                try {
                    while (!stopped.get()) {
                        Entry entry = queue.take();
                        Receiver receiver;
                        synchronized (receivers) { receiver = receivers.get(entry.path()); }
                        if (receiver != null && receiver.message() != null)
                            receiver.message().accept(entry.path(), entry.message());
                    }
                } catch (InterruptedException interrupted) {
                    Thread.currentThread().interrupt();
                } finally { done.countDown(); }
            });
        }
        @Override public void send(List<String> path, Message message) {
            if (stopped.get()) throw new IllegalStateException("witness root closed");
            if (!queue.offer(new Entry(List.copyOf(path), message, 0)))
                throw new IllegalStateException("witness output queue full");
        }
        @Override public Runnable receive(List<String> path, Receiver receiver) {
            Wires.encodePath(path);
            List<String> key = List.copyOf(path);
            synchronized (receivers) {
                if (receivers.putIfAbsent(key, receiver) != null)
                    throw new IllegalStateException("witness receiver exists");
            }
            return () -> { synchronized (receivers) { receivers.remove(key, receiver); } };
        }
        @Override public void close(int code, String reason) {
            if (stopped.compareAndSet(false, true)) dispatcher.interrupt();
            new Deadline(Duration.ofSeconds(2)).await(done);
        }
        @Override public void close() { close(1000, "done"); }
    }

    private static final class Presentation implements AutoCloseable {
        final Root root = new Root();
        final Root end = new Root();
        final Wire wire;
        final BlockingQueue<Integer> values = new ArrayBlockingQueue<>(32);
        final BlockingQueue<Integer> closed = new ArrayBlockingQueue<>(4);
        final BlockingQueue<RuntimeException> errors = new ArrayBlockingQueue<>(4);

        Presentation(Store store) {
            Wire destination = Wires.at(Wires.mount(Map.of("out", Wires.at(end, List.of("destination")))), List.of("out"));
            wire = Wires.at(Wires.mount(Map.of("outer", Wires.mount(Map.of("in", Wires.at(root, List.of("source")))))),
                List.of("outer", "in"));
            destination.receive(List.of("tick"), new Receiver(false, (path, message) -> {
                try {
                    if (!path.equals(List.of("tick"))) throw new IllegalStateException("destination path: " + path);
                    store.callback();
                    if (!values.offer(((Number)message.frame().get("data")).intValue()))
                        throw new IllegalStateException("witness values full");
                } catch (RuntimeException failure) { errors.offer(failure); }
            }, null));
            wire.receive(List.of("tick"), new Receiver(false, (path, message) -> {
                try { destination.send(path, message); }
                catch (RuntimeException failure) { errors.offer(failure); }
            }, (code, reason) -> { store.callback(); closed.offer(code); }));
        }

        List<Integer> collect(Deadline deadline) {
            wire.send(List.of("tick"), message(0));
            var collected = new ArrayList<Integer>();
            while (true) {
                RuntimeException failure = errors.poll();
                if (failure != null) throw failure;
                int value = deadline.take(values);
                if (value == 0) return collected;
                collected.add(value);
            }
        }
        @Override public void close() {
            wire.close(1000, "done");
            root.close();
            end.close();
        }
    }

    private static Message message(int value) {
        return new Message(Map.of("version", 1, "kind", "event", "data", value), null);
    }

    private static Map<String,Object> headCase(Deadline deadline, boolean appendBeforeHead) {
        try (var store = new Store(); var first = new Presentation(store)) {
            Wire source = Wires.at(Wires.mount(Map.of("record", store)), List.of("record"));
            for (int value = 1; value <= 3; value++) source.send(List.of("tick"), message(value));
            if (appendBeforeHead) source.send(List.of("tick"), message(4));
            Attachment attachment = store.attach(0, first.wire, 2, true);
            try (var follower = attachment.follower()) {
                deadline.await(follower.paused);
                var produced = new ArrayBlockingQueue<Boolean>(1);
                Thread.ofVirtual().name("recorded-wire-producer").start(() -> {
                    for (int value = appendBeforeHead ? 5 : 4; value <= 5; value++)
                        source.send(List.of("tick"), message(value));
                    produced.offer(follower.resume.getCount() == 1);
                });
                boolean progress = deadline.take(produced);
                follower.resume.countDown();
                follower.through(deadline, 5);
                source.send(List.of("tick"), message(6));
                follower.through(deadline, 6);
                List<Integer> all = first.collect(deadline);
                try (var second = new Presentation(store); var late = store.attach(3, second.wire, 2, false).follower()) {
                    late.through(deadline, 6);
                    List<Integer> afterThree = second.collect(deadline);
                    return Map.of("cut", appendBeforeHead ? "append_before_head" : "head_before_append",
                        "head", attachment.head(), "first", all, "after_three", afterThree,
                        "producer_progress", progress,
                        "callbacks_outside_append", store.callbacks.get() > 0 && store.callbacksOutsideAppend.get());
                }
            }
        }
    }

    private static Map<String,Object> stallCase(Deadline deadline) {
        try (var store = new Store(); var stalled = new Presentation(store); var healthy = new Presentation(store)) {
            for (int value = 1; value <= 3; value++) store.send(List.of("tick"), message(value));
            try (var slow = store.attach(0, stalled.wire, 2, true).follower()) {
                deadline.await(slow.paused);
                try (var fast = store.attach(3, healthy.wire, 2, false).follower()) {
                    for (int value = 4; value <= 5; value++) {
                        store.send(List.of("tick"), message(value));
                        fast.through(deadline, value);
                    }
                    int queued = slow.live.size();
                    store.send(List.of("tick"), message(6));
                    fast.through(deadline, 6);
                    deadline.await(slow.done);
                    int code = deadline.take(stalled.closed);
                    boolean refused = false;
                    try { stalled.wire.send(List.of("tick"), message(99)); }
                    catch (IllegalStateException expected) { refused = true; }
                    if (!refused) throw new IllegalStateException("stalled carrier accepted after close");
                    var underneath = new ArrayBlockingQueue<Integer>(1);
                    stalled.root.receive(List.of("probe"), new Receiver(false,
                        (path, message) -> underneath.offer(((Number)message.frame().get("data")).intValue()), null));
                    stalled.root.send(List.of("probe"), message(99));
                    int probe = deadline.take(underneath);
                    store.send(List.of("tick"), message(7));
                    fast.through(deadline, 7);
                    List<Integer> values = healthy.collect(deadline);
                    return Map.of("bound", slow.live.size() + slow.live.remainingCapacity(), "queued_at_bound", queued,
                        "closed", 1 + stalled.closed.size(), "close_code", code, "healthy", values,
                        "underneath", List.of(probe), "head", store.head(),
                        "producer_progress", slow.resume.getCount() == 1 && values.contains(7));
                }
            }
        }
    }
}
