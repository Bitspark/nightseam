package io.nightseam.duplex;

import java.nio.ByteBuffer;
import java.nio.charset.CharacterCodingException;
import java.nio.charset.CodingErrorAction;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/** Path views allocate no peer, channel, queue, or application dispatch. */
public final class Wires {
    private Wires() {}

    public static String encodePath(List<String> path) {
        StringBuilder encoded = new StringBuilder();
        for (String segment : path) {
            scalar(segment);
            encoded.append(segment.getBytes(StandardCharsets.UTF_8).length).append(':').append(segment);
        }
        return encoded.toString();
    }

    public static List<String> decodePath(String encoded) {
        scalar(encoded);
        byte[] bytes = encoded.getBytes(StandardCharsets.UTF_8);
        List<String> path = new ArrayList<>();
        for (int at = 0; at < bytes.length;) {
            int first = at;
            long length = 0;
            while (at < bytes.length && bytes[at] != ':') {
                int digit = bytes[at++] - '0';
                if (digit < 0 || digit > 9 || length > Integer.MAX_VALUE / 10L) throw invalidPath();
                length = length * 10 + digit;
                if (length > bytes.length) throw invalidPath();
            }
            if (at == first || at == bytes.length || at - first > 1 && bytes[first] == '0') throw invalidPath();
            at++;
            if (length > bytes.length - at) throw invalidPath();
            try {
                path.add(StandardCharsets.UTF_8.newDecoder().onMalformedInput(CodingErrorAction.REPORT)
                        .onUnmappableCharacter(CodingErrorAction.REPORT)
                        .decode(ByteBuffer.wrap(bytes, at, (int) length)).toString());
            } catch (CharacterCodingException invalid) { throw invalidPath(); }
            at += (int) length;
        }
        return List.copyOf(path);
    }

    static void scalar(String text) {
        if (text == null) throw invalidPath();
        for (int i = 0; i < text.length(); i++) {
            char ch = text.charAt(i);
            if (Character.isHighSurrogate(ch)) {
                if (++i == text.length() || !Character.isLowSurrogate(text.charAt(i))) throw invalidPath();
            } else if (Character.isLowSurrogate(ch)) throw invalidPath();
        }
    }

    private static IllegalArgumentException invalidPath() { return new IllegalArgumentException("invalid wire path"); }

    private static List<String> join(List<String> a, List<String> b) {
        ArrayList<String> joined = new ArrayList<>(a.size() + b.size());
        joined.addAll(a);
        joined.addAll(b);
        return List.copyOf(joined);
    }

    public static Wire at(Wire root, List<String> path) {
        encodePath(path);
        List<String> prefix = List.copyOf(path);
        return new Wire() {
            @Override public void send(List<String> relative, Message message) { root.send(join(prefix, relative), message); }
            @Override public Runnable receive(List<String> relative, Receiver receiver) {
                return root.receive(join(prefix, relative), new Receiver(receiver.namespace(),
                        receiver.message() == null ? null : (delivered, message) ->
                                receiver.message().accept(List.copyOf(delivered.subList(prefix.size(), delivered.size())), message),
                        receiver.closed()));
            }
            @Override public void close(int code, String reason) { root.close(code, reason); }
        };
    }

    public static Wire mount(Map<String, Wire> children) { return new Mount(children); }

    private static final class Mount implements Wire {
        private final Map<String, Wire> children;
        private final Set<Registration> registrations = new LinkedHashSet<>();
        private boolean closed;

        private static final class Registration {
            final Receiver receiver;
            Runnable detach;
            boolean active = true;
            Registration(Receiver receiver) { this.receiver = receiver; }
        }

        Mount(Map<String, Wire> children) {
            this.children = new LinkedHashMap<>(children);
            for (String key : children.keySet()) scalar(key);
        }

        private synchronized Wire destination(List<String> path) {
            if (closed) throw new IllegalStateException("wire closed");
            encodePath(path);
            if (path.isEmpty() || children.get(path.getFirst()) == null) throw new IllegalArgumentException("wire path has no destination");
            return children.get(path.getFirst());
        }

        @Override public void send(List<String> path, Message message) {
            Wire child = destination(path);
            child.send(List.copyOf(path.subList(1, path.size())), message);
        }

        @Override public Runnable receive(List<String> path, Receiver receiver) {
            if (path.isEmpty() && receiver.namespace()) return namespace(receiver);
            Wire child;
            Registration registration = new Registration(receiver);
            synchronized (this) {
                child = destination(path);
                registrations.add(registration);
            }
            Runnable detach;
            try {
                String key = path.getFirst();
                detach = child.receive(List.copyOf(path.subList(1, path.size())), new Receiver(receiver.namespace(),
                        receiver.message() == null ? null : (delivered, message) -> receiver.message().accept(join(List.of(key), delivered), message),
                        (code, reason) -> remove(registration, true, code, reason)));
            } catch (RuntimeException error) {
                remove(registration, false, 0, "");
                throw error;
            }
            synchronized (this) {
                if (registration.active) {
                    registration.detach = detach;
                    return () -> remove(registration, false, 0, "");
                }
            }
            detach.run();
            throw new IllegalStateException("wire closed");
        }

        private Runnable namespace(Receiver receiver) {
            Registration registration = new Registration(receiver);
            List<String> keys;
            List<Runnable> detaches = new ArrayList<>();
            Object guard = new Object();
            boolean[] ended = { false };
            synchronized (this) {
                if (closed) throw new IllegalStateException("wire closed");
                keys = children.entrySet().stream().filter(e -> e.getValue() != null).map(Map.Entry::getKey).toList();
                registration.detach = () -> {
                    List<Runnable> held;
                    synchronized (guard) {
                        ended[0] = true;
                        held = List.copyOf(detaches);
                        detaches.clear();
                    }
                    held.forEach(Runnable::run);
                };
                registrations.add(registration);
            }
            int[] remaining = { keys.size() };
            try {
                for (String key : keys) {
                    Runnable detach = receive(List.of(key), new Receiver(true, receiver.message(), (code, reason) -> {
                        boolean last;
                        synchronized (guard) { last = --remaining[0] == 0; }
                        if (last) remove(registration, true, code, reason);
                    }));
                    synchronized (guard) {
                        if (ended[0]) {
                            detach.run();
                            throw new IllegalStateException("wire closed");
                        }
                        detaches.add(detach);
                    }
                }
            } catch (RuntimeException error) {
                remove(registration, false, 0, "");
                throw error;
            }
            return () -> remove(registration, false, 0, "");
        }

        private void remove(Registration registration, boolean tell, int code, String reason) {
            Runnable detach;
            synchronized (this) {
                if (!registration.active) return;
                registration.active = false;
                registrations.remove(registration);
                detach = registration.detach;
            }
            if (detach != null) detach.run();
            if (tell && registration.receiver.closed() != null) registration.receiver.closed().accept(code, reason);
        }

        @Override public void close(int code, String reason) {
            List<Registration> held;
            synchronized (this) {
                if (closed) return;
                closed = true;
                held = List.copyOf(registrations);
                for (Registration registration : held) registration.active = false;
                registrations.clear();
            }
            for (Registration registration : held) if (registration.detach != null) registration.detach.run();
            for (Registration registration : held) if (registration.receiver.closed() != null) registration.receiver.closed().accept(code, reason);
        }
    }
}
