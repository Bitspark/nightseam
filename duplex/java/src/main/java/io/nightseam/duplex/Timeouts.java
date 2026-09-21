package io.nightseam.duplex;

import java.time.Duration;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

final class Timeouts {
    private Timeouts() {}
    static long deadline(Duration duration) {
        if (duration == null) return Long.MAX_VALUE;
        if (duration.isNegative()) throw new IllegalArgumentException("timeout must not be negative");
        long nanos;
        try { nanos = duration.toNanos(); } catch (ArithmeticException overflow) { return Long.MAX_VALUE; }
        long now = System.nanoTime();
        return nanos > Long.MAX_VALUE - Math.max(0, now) ? Long.MAX_VALUE : now + nanos;
    }
    static long remaining(long deadline) throws TimeoutException {
        long nanos = deadline == Long.MAX_VALUE ? Long.MAX_VALUE : deadline - System.nanoTime();
        if (nanos <= 0) throw new TimeoutException("duplex operation timed out");
        return nanos;
    }
    static void await(Object lock, long deadline) throws InterruptedException, TimeoutException {
        TimeUnit.NANOSECONDS.timedWait(lock, remaining(deadline));
    }
}
