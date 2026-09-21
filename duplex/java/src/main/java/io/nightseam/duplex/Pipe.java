package io.nightseam.duplex;

import java.time.Duration;
import java.util.ArrayDeque;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.TimeoutException;

/** Two bounded in-memory ends with the same order and ending semantics as a socket. */
public final class Pipe {
    private Pipe() {}

    public static Connection[] pair(int byteLimit, int capacity) {
        if (capacity <= 0) throw new IllegalArgumentException("capacity must be positive");
        Object lock = new Object();
        End a = new End(lock, byteLimit, capacity);
        End b = new End(lock, byteLimit, capacity);
        a.remote = b;
        b.remote = a;
        return new Connection[] { a, b };
    }

    private static final class End implements Connection {
        private final Object lock;
        private final int limit;
        private final int capacity;
        private final ArrayDeque<Frame> incoming = new ArrayDeque<>();
        private final CompletableFuture<CloseInfo> closed = new CompletableFuture<>();
        private End remote;
        private CloseInfo ending;

        End(Object lock, int limit, int capacity) {
            this.lock = lock;
            this.limit = limit;
            this.capacity = capacity;
        }

        @Override public void send(Frame frame, Duration timeout) throws CloseException, InterruptedException, TimeoutException {
            long deadline = Timeouts.deadline(timeout);
            synchronized (lock) {
                while (true) {
                    if (ending != null) throw exception(ending);
                    if (remote.ending != null) throw exception(remote.ending);
                    if (remote.incoming.size() < capacity) {
                        remote.incoming.addLast(frame);
                        lock.notifyAll();
                        return;
                    }
                    Timeouts.await(lock, deadline);
                }
            }
        }

        @Override public Frame receive(Duration timeout) throws CloseException, InterruptedException, TimeoutException {
            long deadline = Timeouts.deadline(timeout);
            synchronized (lock) {
                while (true) {
                    if (ending != null) throw exception(ending);
                    if (!incoming.isEmpty()) {
                        Frame frame = incoming.removeFirst();
                        lock.notifyAll();
                        if (limit > 0 && frame.data().length > limit) {
                            finish(new CloseInfo(1009, "frame exceeds receive limit"));
                            throw exception(ending);
                        }
                        return frame;
                    }
                    if (remote.ending != null) throw exception(remote.ending);
                    Timeouts.await(lock, deadline);
                }
            }
        }

        private void finish(CloseInfo info) {
            synchronized (lock) {
                if (ending != null) return;
                ending = info;
                incoming.clear();
                lock.notifyAll();
            }
            closed.complete(info);
            remote.closed.complete(info);
        }

        @Override public void close(int code, String reason) { finish(new CloseInfo(code, reason)); }
        @Override public void abort() { finish(new CloseInfo(1006, "")); }
        @Override public CompletableFuture<CloseInfo> closed() { return closed; }
    }

    private static CloseException exception(CloseInfo info) { return new CloseException(info.code(), info.reason()); }
}
