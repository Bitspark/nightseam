package io.nightseam.duplex;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.Socket;
import java.net.ServerSocket;
import java.net.InetAddress;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.WebSocket;
import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Base64;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.concurrent.atomic.AtomicInteger;

/** The seam's public contract, executable without a test framework. */
public final class SeamTest {
    private static final Duration WAIT = Duration.ofSeconds(5);
    private interface Connect { Connection[] pair(int limit) throws Exception; }
    private interface Action { void run() throws Exception; }

    public static void main(String[] args) throws Exception {
        closeHandshake();
        transports("pipe", limit -> Pipe.pair(limit, 8));
        transports("WebSocket", limit -> {
            try (WebSocketTransport.Listener listener = WebSocketTransport.listen(limit, List.of("nightseam-test"))) {
                Connection client = WebSocketTransport.dial(listener.uri(), limit, List.of("nightseam-test"));
                Connection server = listener.accept(WAIT);
                equal(client.subprotocol(), "nightseam-test", "client protocol");
                equal(server.subprotocol(), "nightseam-test", "server protocol");
                return new Connection[] { client, server };
            }
        });
        pipeAdmission();
        pathViews();
        nativeServerProtocol();
        nativeClientProtocol();
        jdkInteroperation();
        System.out.println("Java seam: pipe, WebSocket, RFC6455 and path views passed");
    }

    private static void closeHandshake() throws Exception {
        try (WebSocketTransport.Listener listener = WebSocketTransport.listen(1024, List.of()); Socket raw = upgrade(listener)) {
            Connection connection = listener.accept(WAIT);
            try {
                // A consumer may release transport resources as soon as closed
                // completes. That callback must not discard the close reply.
                connection.closed().thenRun(connection::abort);
                rawFrame(raw.getOutputStream(), 8, true, true, new byte[] { 3, (byte) 0xe8 });
                equal(raw.getInputStream().read(), 0x88, "remote close acknowledged before callback");
                equal(raw.getInputStream().read(), 2, "close acknowledgment length");
                equal(raw.getInputStream().read(), 3, "close acknowledgment code high");
                equal(raw.getInputStream().read(), 0xe8, "close acknowledgment code low");
                equal(connection.closed().get(5, TimeUnit.SECONDS).code(), 1000, "chosen remote close code");
            } finally { connection.abort(); }
        }
        for (boolean acknowledge : List.of(true, false)) {
        try (WebSocketTransport.Listener listener = WebSocketTransport.listen(1024, List.of()); Socket raw = upgrade(listener)) {
            Connection connection = listener.accept(WAIT);
            try {
                connection.close(1000, "");
                equal(raw.getInputStream().read(), 0x88, "local close frame");
                equal(raw.getInputStream().read(), 2, "local close length");
                equal(raw.getInputStream().read(), 3, "local close code high");
                equal(raw.getInputStream().read(), 0xe8, "local close code low");
                check(!connection.closed().isDone(), "local close waits for acknowledgment");
                if (acknowledge) rawFrame(raw.getOutputStream(), 8, true, true, new byte[] { 3, (byte) 0xe8 });
                equal(connection.closed().get(5, TimeUnit.SECONDS).code(), 1000, "chosen local close code");
                equal(raw.getInputStream().read(), -1, "close sent once before socket shutdown");
            } finally { connection.abort(); }
        }
        }
    }

    private static void transports(String name, Connect connect) throws Exception {
        Connection[] pair = connect.pair(1 << 20);
        try {
            for (int direction = 0; direction < 2; direction++) {
                Connection sender = pair[direction], receiver = pair[1 - direction];
                CompletableFuture<Void> sent = run(() -> {
                    for (int i = 0; i < 64; i++) sender.send(frame(i % 3 == 0 ? "binary" : "text", "frame " + i + " 😀"), WAIT);
                });
                for (int i = 0; i < 64; i++) {
                    Frame got = receiver.receive(WAIT);
                    equal(got.kind(), i % 3 == 0 ? "binary" : "text", name + " kind");
                    equal(string(got), "frame " + i + " 😀", name + " frame order");
                }
                sent.get(5, TimeUnit.SECONDS);
                byte[] bytes = new byte[] { 0, -1, 0, 1, -128 };
                sender.send(new Frame("binary", bytes), WAIT);
                bytes[0] = 55;
                check(Arrays.equals(receiver.receive(WAIT).data(), new byte[] { 0, -1, 0, 1, -128 }), "frame owns bytes");
            }
        } finally { abort(pair); }

        for (int direction = 0; direction < 2; direction++) {
            pair = connect.pair(1024);
            Connection sender = pair[direction], receiver = pair[1 - direction];
            try {
                sender.send(frame("text", "😀".repeat(256)), WAIT);
                equal(string(receiver.receive(WAIT)), "😀".repeat(256), "inclusive UTF-8 byte limit");
                sender.send(frame("text", "😀".repeat(256) + "x"), WAIT);
                Connection limited = receiver;
                closeCode(() -> limited.receive(WAIT), 1009);
                closeCode(() -> limited.receive(WAIT), 1009);
                equal(receiver.closed().get(5, TimeUnit.SECONDS).code(), 1009, "oversize completion");
            } finally { abort(pair); }

            for (int code : new int[] { 1000, 1001, 1002, 1003, 1007, 1008, 1009, 1011, 4011 }) {
                pair = connect.pair(1024);
                sender = pair[direction];
                receiver = pair[1 - direction];
                try {
                    sender.send(frame("text", "last"), WAIT);
                    sender.close(code, "reason é");
                    equal(string(receiver.receive(WAIT)), "last", "frames before close remain ordered");
                    Connection ended = receiver;
                    closeCode(() -> ended.receive(WAIT), code);
                    equal(receiver.closed().get(5, TimeUnit.SECONDS).reason(), "reason é", "close reason");
                    Connection local = sender;
                    expect(CloseException.class, () -> local.send(frame("text", "late"), WAIT));
                    expect(CloseException.class, () -> local.receive(WAIT));
                } finally { abort(pair); }
            }

            pair = connect.pair(1024);
            sender = pair[direction];
            receiver = pair[1 - direction];
            try {
                Connection waiting = receiver;
                CompletableFuture<Void> received = run(() -> closeCode(() -> waiting.receive(WAIT), 1006));
                sender.abort();
                received.get(3, TimeUnit.SECONDS);
            } finally { abort(pair); }

            pair = connect.pair(1 << 20);
            sender = pair[direction];
            try {
                boolean timedOut = false;
                long deadline = System.nanoTime() + Duration.ofSeconds(3).toNanos();
                for (int i = 0; i < 400; i++) {
                    try { sender.send(new Frame("binary", new byte[256 << 10]), Duration.ofNanos(Math.max(1, deadline - System.nanoTime()))); }
                    catch (TimeoutException expected) { timedOut = true; break; }
                }
                check(timedOut, name + " accepted 100 MiB without a receiver");
            } finally { abort(pair); }
        }
    }

    private static void pipeAdmission() throws Exception {
        Connection[] pair = Pipe.pair(1024, 8);
        try {
            for (int i = 0; i < 8; i++) pair[0].send(frame("text", "frame " + i), WAIT);
            expect(TimeoutException.class, () -> pair[0].send(frame("text", "ninth"), Duration.ofMillis(50)));
            for (int i = 0; i < 8; i++) equal(string(pair[1].receive(WAIT)), "frame " + i, "pipe bounded order");
            pair[0].send(frame("text", "ninth"), WAIT);
            equal(string(pair[1].receive(WAIT)), "ninth", "timed-out send is not queued");
            expect(TimeoutException.class, () -> pair[1].receive(Duration.ofMillis(10)));
            for (int i = 0; i < 8; i++) pair[0].send(frame("text", "full"), WAIT);
            CompletableFuture<Void> blocked = run(() -> expect(CloseException.class, () -> pair[0].send(frame("text", "blocked"), WAIT)));
            pair[1].abort();
            blocked.get(2, TimeUnit.SECONDS);
        } finally { abort(pair); }
    }

    private static void pathViews() throws Exception {
        List<List<String>> paths = List.of(List.of(), List.of(""), List.of("a", "b"), List.of("a/b"), List.of("a.b"), List.of("é", "é", "😀", "\ufeff", "\u0000"));
        for (List<String> path : paths) {
            equal(Wires.decodePath(Wires.encodePath(path)), path, "path roundtrip");
            for (List<String> suffix : paths) {
                ArrayList<String> combined = new ArrayList<>(path);
                combined.addAll(suffix);
                equal(Wires.encodePath(combined), Wires.encodePath(path) + Wires.encodePath(suffix), "path concatenation");
            }
        }
        equal(Wires.encodePath(List.of("a", "😀", "")), "1:a4:😀0:", "byte-length encoding");
        for (String malformed : List.of("01:a", "00:", "1", ":", "-1:a", "2:a", "1:é", "999999999999999999999999:x", "1:\ud800")) {
            expect(IllegalArgumentException.class, () -> Wires.decodePath(malformed));
        }
        expect(IllegalArgumentException.class, () -> Wires.encodePath(List.of("\udfff")));
        Root root = new Root();
        Wire selected = Wires.at(Wires.at(root, List.of("outer")), List.of("inner"));
        List<List<String>> delivered = new ArrayList<>();
        Message message = new Message(Map.of("kind", "request"), root);
        Runnable detach = selected.receive(List.of("read"), new Receiver(false, (path, got) -> {
            delivered.add(path);
            check(got == message && got.returnAddress() == root, "local return identity");
        }, null));
        selected.send(List.of("read"), message);
        check(delivered.isEmpty(), "views must not invoke an application on send");
        root.drain();
        equal(delivered, List.of(List.of("read")), "selected path is relative");
        detach.run(); detach.run();
        selected.send(List.of("read"), message);
        root.drain();
        equal(delivered.size(), 1, "detach prevents new dispatch");
        selected.close(1000, "selected");
        check(root.closed, "selected close owns endpoint");

        Root first = new Root(), second = new Root();
        Wire mount = Wires.mount(Map.of("", first, "b", second));
        List<List<String>> namespace = new ArrayList<>();
        AtomicInteger closes = new AtomicInteger();
        Runnable off = mount.receive(List.of(), new Receiver(true, (path, got) -> namespace.add(path), (code, reason) -> closes.incrementAndGet()));
        mount.send(List.of("", "read"), message);
        mount.send(List.of("b", "write"), message);
        first.drain(); second.drain();
        equal(namespace, List.of(List.of("", "read"), List.of("b", "write")), "mount consumes and restores one segment");
        expect(IllegalArgumentException.class, () -> mount.send(List.of(), message));
        expect(IllegalStateException.class, () -> mount.receive(List.of(), new Receiver(true, null, null)));
        first.close(1001, "first");
        equal(closes.get(), 0, "one child does not end namespace");
        second.close(1000, "second");
        equal(closes.get(), 1, "last child ends namespace once");
        off.run(); off.run();

        Root borrowed = new Root();
        Wire closedMount = Wires.mount(Map.of("borrowed", borrowed));
        closedMount.receive(List.of("borrowed", "read"), new Receiver(false, null, (code, reason) -> closes.incrementAndGet()));
        closedMount.close(1000, "mount");
        closedMount.close(1000, "mount");
        check(!borrowed.closed && borrowed.receivers.isEmpty(), "mount detaches and leaves children usable");
        equal(closes.get(), 2, "mount receiver closes once");
    }

    private static void nativeServerProtocol() throws Exception {
        try (WebSocketTransport.Listener listener = WebSocketTransport.listen(16, List.of()); Socket raw = upgrade(listener)) {
            Connection connection = listener.accept(WAIT);
            try {
                OutputStream output = raw.getOutputStream();
                rawFrame(output, 1, false, true, new byte[] { (byte) 0xf0, (byte) 0x9f });
                rawFrame(output, 9, true, true, new byte[] { 9 });
                rawFrame(output, 0, true, true, new byte[] { (byte) 0x98, (byte) 0x80 });
                equal(string(connection.receive(WAIT)), "😀", "fragmented UTF-8 whole message");
                equal(raw.getInputStream().read(), 0x8a, "ping answered by pong");
                equal(raw.getInputStream().read(), 1, "pong length");
                equal(raw.getInputStream().read(), 9, "pong payload");
                rawFrame(output, 1, false, true, "123456789".getBytes(StandardCharsets.UTF_8));
                rawFrame(output, 0, true, true, "12345678".getBytes(StandardCharsets.UTF_8));
                closeCode(() -> connection.receive(WAIT), 1009);
            } finally { connection.abort(); }
        }
        for (int violation = 0; violation < 10; violation++) {
            try (WebSocketTransport.Listener listener = WebSocketTransport.listen(1024, List.of()); Socket raw = upgrade(listener)) {
                Connection connection = listener.accept(WAIT);
                try {
                    int code = 1002;
                    switch (violation) {
                        case 0 -> rawFrame(raw.getOutputStream(), 1, true, false, new byte[] { 1 });
                        case 1 -> rawFrame(raw.getOutputStream(), 0, true, true, new byte[0]);
                        case 2 -> rawFrame(raw.getOutputStream(), 9, false, true, new byte[0]);
                        case 3 -> rawFrame(raw.getOutputStream(), 8, true, true, new byte[] { 1 });
                        case 4 -> { rawFrame(raw.getOutputStream(), 1, true, true, new byte[] { (byte) 0xff }); code = 1007; }
                        case 5 -> raw.getOutputStream().write(new byte[] { (byte) 0x81, (byte) 0xfe, 0, 1 });
                        case 6 -> raw.getOutputStream().write(new byte[] { (byte) 0x81, (byte) 0xff, (byte) 0x80, 0, 0, 0, 0, 0, 0, 0 });
                        case 7 -> raw.getOutputStream().write(new byte[] { (byte) 0xc1, (byte) 0x80 });
                        case 8 -> rawFrame(raw.getOutputStream(), 8, true, true, new byte[] { 3, (byte) 0xee });
                        case 9 -> {
                            raw.getOutputStream().write(new byte[] { (byte) 0x81, (byte) 0xff, 0, 0, 0, 1, 0, 0, 0, 0 });
                            code = 1009;
                        }
                        default -> throw new AssertionError();
                    }
                    int expectedCode = code;
                    closeCode(() -> connection.receive(WAIT), expectedCode);
                } finally { connection.abort(); }
            }
        }
        try (WebSocketTransport.Listener listener = WebSocketTransport.listen(1024, List.of()); Socket raw = new Socket("127.0.0.1", listener.uri().getPort())) {
            raw.setSoTimeout(1000);
            raw.getOutputStream().write("GET / HTTP/1.1\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 12\r\nSec-WebSocket-Key: AA==\r\n\r\n".getBytes(StandardCharsets.US_ASCII));
            equal(raw.getInputStream().read(), -1, "invalid handshake rejected");
        }
    }

    private static void nativeClientProtocol() throws Exception {
        for (int variant = 0; variant < 5; variant++) {
            int test = variant;
            try (ServerSocket server = new ServerSocket(0, 1, InetAddress.getByName("127.0.0.1"))) {
                CompletableFuture<Void> served = run(() -> {
                    try (Socket socket = server.accept()) {
                        socket.setSoTimeout(5000);
                        String request = rawHeaders(socket.getInputStream());
                        String key = Arrays.stream(request.split("\r\n")).filter(line -> line.startsWith("Sec-WebSocket-Key: ")).findFirst().orElseThrow().substring(19);
                        String accept = Base64.getEncoder().encodeToString(java.security.MessageDigest.getInstance("SHA-1")
                                .digest((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").getBytes(StandardCharsets.US_ASCII)));
                        String extra = test == 3 ? "Sec-WebSocket-Protocol: unoffered\r\n" : test == 4 ? "Sec-WebSocket-Extensions: permessage-deflate\r\n" : "";
                        String reply = "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: "
                                + (test == 2 ? "invalid" : accept) + "\r\n" + extra + "\r\n";
                        socket.getOutputStream().write(reply.getBytes(StandardCharsets.US_ASCII));
                        if (test == 0) {
                            rawFrame(socket.getOutputStream(), 1, false, false, "first ".getBytes(StandardCharsets.UTF_8));
                            rawFrame(socket.getOutputStream(), 0, true, false, "😀".getBytes(StandardCharsets.UTF_8));
                            rawFrame(socket.getOutputStream(), 8, true, false, new byte[] { 3, (byte) 0xe9 });
                            // The native client's close acknowledgment must be masked.
                            equal(socket.getInputStream().read(), 0x88, "client close acknowledgment");
                            check((socket.getInputStream().read() & 128) != 0, "client masks control frames");
                        } else if (test == 1) {
                            rawFrame(socket.getOutputStream(), 1, true, true, new byte[0]);
                            equal(socket.getInputStream().read(), 0x88, "client refuses masked server frame");
                        }
                    }
                });
                URI uri = URI.create("ws://127.0.0.1:" + server.getLocalPort() + "/");
                if (test >= 2) expect(IOException.class, () -> WebSocketTransport.dial(uri, 1024, List.of()));
                else {
                    Connection connection = WebSocketTransport.dial(uri, 1024, List.of());
                    try {
                        if (test == 0) equal(string(connection.receive(WAIT)), "first 😀", "native client fragments");
                        closeCode(() -> connection.receive(WAIT), test == 0 ? 1001 : 1002);
                        served.get(5, TimeUnit.SECONDS);
                    } finally { connection.abort(); }
                }
                served.get(5, TimeUnit.SECONDS);
            }
        }
    }

    private static void jdkInteroperation() throws Exception {
        try (WebSocketTransport.Listener listener = WebSocketTransport.listen(1024, List.of("nightseam-test")); HttpClient client = HttpClient.newHttpClient()) {
            CompletableFuture<Integer> closed = new CompletableFuture<>();
            WebSocket socket = client.newWebSocketBuilder().subprotocols("nightseam-test").buildAsync(listener.uri(), new WebSocket.Listener() {
                @Override public void onOpen(WebSocket ws) { ws.request(Long.MAX_VALUE); }
                @Override public java.util.concurrent.CompletionStage<?> onClose(WebSocket ws, int code, String reason) { closed.complete(code); return null; }
            }).get(5, TimeUnit.SECONDS);
            Connection accepted = listener.accept(WAIT);
            try {
                socket.sendText("first ", false).get(5, TimeUnit.SECONDS);
                socket.sendText("😀", true).get(5, TimeUnit.SECONDS);
                equal(string(accepted.receive(WAIT)), "first 😀", "JDK fragmented client");
                socket.sendBinary(ByteBuffer.wrap(new byte[] { 0, 1, 2 }), true).get(5, TimeUnit.SECONDS);
                check(Arrays.equals(accepted.receive(WAIT).data(), new byte[] { 0, 1, 2 }), "JDK binary");
                accepted.close(1003, "text required");
                equal(closed.get(5, TimeUnit.SECONDS), 1003, "JDK receives required close code");
            } finally { socket.abort(); accepted.abort(); }
        }
    }

    private static Socket upgrade(WebSocketTransport.Listener listener) throws Exception {
        Socket socket = new Socket("127.0.0.1", listener.uri().getPort());
        socket.setSoTimeout(2000);
        socket.getOutputStream().write("GET / HTTP/1.1\r\nHost: localhost\r\nUpgrade: websocket\r\nConnection: keep-alive, Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n".getBytes(StandardCharsets.US_ASCII));
        check(rawHeaders(socket.getInputStream()).startsWith("HTTP/1.1 101"), "raw upgrade");
        return socket;
    }

    private static String rawHeaders(InputStream input) throws IOException {
        ByteArrayOutputStream header = new ByteArrayOutputStream();
        while (!header.toString(StandardCharsets.US_ASCII).endsWith("\r\n\r\n")) {
            int next = input.read();
            if (next == -1) throw new IOException("upgrade refused");
            header.write(next);
        }
        return header.toString(StandardCharsets.US_ASCII);
    }

    private static void rawFrame(OutputStream output, int opcode, boolean fin, boolean mask, byte[] data) throws IOException {
        output.write((fin ? 128 : 0) | opcode);
        output.write((mask ? 128 : 0) | data.length);
        byte[] key = { 1, 2, 3, 4 };
        if (mask) output.write(key);
        byte[] bytes = data.clone();
        if (mask) for (int i = 0; i < bytes.length; i++) bytes[i] ^= key[i % 4];
        output.write(bytes);
        output.flush();
    }

    private static final class Root implements Wire {
        final Map<String, Receiver> receivers = new LinkedHashMap<>();
        final List<Runnable> queue = new ArrayList<>();
        boolean closed;
        @Override public void send(List<String> path, Message message) {
            if (closed) throw new IllegalStateException("closed");
            String key = Wires.encodePath(path);
            queue.add(() -> {
                Receiver receiver = receivers.get(key);
                if (receiver == null) {
                    int longest = -1;
                    for (Map.Entry<String, Receiver> entry : receivers.entrySet()) {
                        if (entry.getKey().startsWith("namespace ")) {
                            String prefix = entry.getKey().substring(10);
                            if (key.startsWith(prefix) && prefix.length() > longest) { longest = prefix.length(); receiver = entry.getValue(); }
                        }
                    }
                }
                if (receiver != null && receiver.message() != null) receiver.message().accept(path, message);
            });
        }
        @Override public Runnable receive(List<String> path, Receiver receiver) {
            if (closed) throw new IllegalStateException("closed");
            String key = (receiver.namespace() ? "namespace " : "") + Wires.encodePath(path);
            if (receivers.putIfAbsent(key, receiver) != null) throw new IllegalStateException("receiver exists");
            return () -> receivers.remove(key, receiver);
        }
        @Override public void close(int code, String reason) {
            if (closed) return;
            closed = true;
            List<Receiver> held = List.copyOf(receivers.values());
            receivers.clear();
            for (Receiver receiver : held) if (receiver.closed() != null) receiver.closed().accept(code, reason);
        }
        void drain() { List<Runnable> held = List.copyOf(queue); queue.clear(); held.forEach(Runnable::run); }
    }

    private static Frame frame(String kind, String data) { return new Frame(kind, data.getBytes(StandardCharsets.UTF_8)); }
    private static String string(Frame frame) { return new String(frame.data(), StandardCharsets.UTF_8); }
    private static void abort(Connection[] pair) { for (Connection connection : pair) connection.abort(); }
    private static CompletableFuture<Void> run(Action action) {
        CompletableFuture<Void> future = new CompletableFuture<>();
        Thread.ofVirtual().start(() -> { try { action.run(); future.complete(null); } catch (Throwable failure) { future.completeExceptionally(failure); } });
        return future;
    }
    private static void check(boolean okay, String message) { if (!okay) throw new AssertionError(message); }
    private static void equal(Object got, Object want, String message) { check(java.util.Objects.equals(got, want), message + ": got " + got + ", want " + want); }
    private static void expect(Class<? extends Exception> type, Action action) throws Exception {
        try { action.run(); } catch (Exception failure) { if (type.isInstance(failure)) return; throw failure; }
        throw new AssertionError("expected " + type.getSimpleName());
    }
    private static void closeCode(Action action, int expected) throws Exception {
        try { action.run(); } catch (CloseException closed) { equal(closed.code(), expected, "close code"); return; }
        throw new AssertionError("expected close " + expected);
    }
}
