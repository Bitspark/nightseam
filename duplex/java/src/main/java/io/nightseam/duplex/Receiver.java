package io.nightseam.duplex;

import java.util.List;
import java.util.function.BiConsumer;

/** A receiver uses exact matching unless namespace is true; callbacks may be null. */
public record Receiver(boolean namespace, BiConsumer<List<String>, Message> message,
                       BiConsumer<Integer, String> closed) {}
