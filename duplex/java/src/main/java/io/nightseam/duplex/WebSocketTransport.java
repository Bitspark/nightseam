package io.nightseam.duplex;

import java.io.ByteArrayOutputStream;
import java.io.EOFException;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.ServerSocket;
import java.net.Socket;
import java.net.URI;
import java.net.InetSocketAddress;
import javax.net.ssl.SSLSocket;
import javax.net.ssl.SSLSocketFactory;
import javax.net.ssl.SSLParameters;
import java.nio.ByteBuffer;
import java.nio.charset.CharacterCodingException;
import java.nio.charset.CodingErrorAction;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.security.SecureRandom;
import java.time.Duration;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.Base64;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.FutureTask;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.concurrent.locks.ReentrantLock;

/** WebSocket adapters using the native RFC 6455 framing over JDK sockets. */
public final class WebSocketTransport {
    private static final int CAPACITY = 8;
    private static final SecureRandom RANDOM = new SecureRandom();
    private static final Duration CLOSE_TIMEOUT = Duration.ofSeconds(2);
    private WebSocketTransport() {}

    public static Connection dial(URI uri, int limit, List<String> protocols)
            throws IOException, InterruptedException, TimeoutException {
        boolean secure = "wss".equalsIgnoreCase(uri.getScheme());
        if (!secure && !"ws".equalsIgnoreCase(uri.getScheme()) || uri.getHost() == null
                || uri.getUserInfo() != null || uri.getFragment() != null) throw new IllegalArgumentException("invalid WebSocket URI");
        validateProtocols(protocols);
        int port = uri.getPort() < 0 ? (secure ? 443 : 80) : uri.getPort();
        Socket socket = new Socket();
        try {
            socket.connect(new InetSocketAddress(uri.getHost(), port), 10000);
            if (secure) socket = ((SSLSocketFactory) SSLSocketFactory.getDefault()).createSocket(socket, uri.getHost(), port, true);
            socket.setSoTimeout(10000);
            socket.setTcpNoDelay(true);
            if (socket instanceof SSLSocket tls) {
                SSLParameters parameters = tls.getSSLParameters();
                parameters.setEndpointIdentificationAlgorithm("HTTPS");
                tls.setSSLParameters(parameters);
                tls.startHandshake();
            }
            byte[] random = new byte[16];
            RANDOM.nextBytes(random);
            String key = Base64.getEncoder().encodeToString(random);
            String path = uri.getRawPath();
            if (path == null || path.isEmpty()) path = "/";
            if (uri.getRawQuery() != null) path += "?" + uri.getRawQuery();
            String host = uri.getHost();
            if (host.indexOf(':') >= 0 && !host.startsWith("[")) host = "[" + host + "]";
            if (uri.getPort() >= 0) host += ":" + port;
            String request = "GET " + path + " HTTP/1.1\r\nHost: " + host + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: " + key + "\r\n"
                    + (protocols.isEmpty() ? "" : "Sec-WebSocket-Protocol: " + String.join(", ", protocols) + "\r\n") + "\r\n";
            socket.getOutputStream().write(request.getBytes(StandardCharsets.US_ASCII));
            socket.getOutputStream().flush();
            InputStream input = socket.getInputStream();
            String[] lines = headers(input).split("\r\n");
            if (!lines[0].matches("HTTP/1\\.1 101(?: .*|$)")) throw new IOException("WebSocket upgrade refused: " + lines[0]);
            Map<String, String> fields = fields(lines);
            if (!"websocket".equalsIgnoreCase(fields.get("upgrade")) || !token(fields.get("connection"), "upgrade")
                    || !acceptKey(key).equals(fields.get("sec-websocket-accept")) || fields.containsKey("sec-websocket-extensions")) {
                throw new IOException("invalid WebSocket upgrade response");
            }
            String protocol = fields.getOrDefault("sec-websocket-protocol", "");
            if (!protocol.isEmpty() && !protocols.contains(protocol)) throw new IOException("unoffered WebSocket subprotocol");
            socket.setSoTimeout(0);
            Native connection = new Native(socket, input, limit, protocol, true);
            connection.start();
            return connection;
        } catch (IOException | RuntimeException failure) {
            closeSocket(socket);
            throw failure;
        }
    }

    public static Listener listen(int limit, List<String> protocols) throws IOException {
        return new Listener(limit, protocols);
    }

    /** A loopback listener. Accepted connections remain independently owned by the caller. */
    public static final class Listener implements AutoCloseable {
        private final ServerSocket server;
        private final int limit;
        private final List<String> protocols;
        private final ArrayDeque<Connection> accepted = new ArrayDeque<>();
        private final Set<Socket> handshakes = new HashSet<>();
        private boolean ended;

        private Listener(int limit, List<String> protocols) throws IOException {
            this.limit = limit;
            this.protocols = List.copyOf(protocols);
            validateProtocols(protocols);
            server = new ServerSocket(0, 16, InetAddress.getByName("127.0.0.1"));
            Thread.ofVirtual().name("nightseam-ws-listener").start(this::serve);
        }

        public URI uri() { return URI.create("ws://127.0.0.1:" + server.getLocalPort() + "/"); }

        public synchronized Connection accept(Duration timeout) throws IOException, InterruptedException, TimeoutException {
            long deadline = Timeouts.deadline(timeout);
            while (true) {
                if (ended) throw new IOException("WebSocket listener closed");
                if (!accepted.isEmpty()) {
                    Connection connection = accepted.removeFirst();
                    notifyAll();
                    return connection;
                }
                Timeouts.await(this, deadline);
            }
        }

        private void serve() {
            try {
                while (true) {
                    Socket socket = server.accept();
                    synchronized (this) {
                        if (ended || handshakes.size() >= 16) { socket.close(); continue; }
                        handshakes.add(socket);
                    }
                    Thread.ofVirtual().name("nightseam-ws-handshake").start(() -> upgrade(socket));
                }
            } catch (IOException stopped) { close(); }
        }

        private void upgrade(Socket socket) {
            boolean handedOff = false;
            try {
                socket.setSoTimeout(5000);
                socket.setTcpNoDelay(true);
                InputStream input = socket.getInputStream();
                String request = headers(input);
                String[] lines = request.split("\r\n");
                if (!lines[0].matches("GET [^ ]+ HTTP/1\\.1")) throw new IOException("invalid upgrade request");
                Map<String, String> fields = fields(lines);
                if (!"websocket".equalsIgnoreCase(fields.get("upgrade")) || !token(fields.get("connection"), "upgrade")
                        || !"13".equals(fields.get("sec-websocket-version"))) throw new IOException("invalid WebSocket upgrade");
                String key = fields.get("sec-websocket-key");
                if (key == null || Base64.getDecoder().decode(key).length != 16) throw new IOException("invalid WebSocket key");
                String selected = "";
                for (String protocol : protocols) if (token(fields.get("sec-websocket-protocol"), protocol, false)) { selected = protocol; break; }
                String accept = acceptKey(key);
                String response = "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + accept + "\r\n"
                        + (selected.isEmpty() ? "" : "Sec-WebSocket-Protocol: " + selected + "\r\n") + "\r\n";
                socket.getOutputStream().write(response.getBytes(StandardCharsets.US_ASCII));
                socket.getOutputStream().flush();
                socket.setSoTimeout(0);
                Native connection = new Native(socket, input, limit, selected, false);
                synchronized (this) {
                    while (!ended && accepted.size() >= 16) wait();
                    if (ended) { connection.abort(); return; }
                    accepted.addLast(connection);
                    handedOff = true;
                    handshakes.remove(socket);
                    notifyAll();
                }
                connection.start();
            } catch (IOException | IllegalArgumentException | InterruptedException failure) {
                if (failure instanceof InterruptedException) Thread.currentThread().interrupt();
            } finally {
                synchronized (this) { handshakes.remove(socket); }
                if (!handedOff) closeSocket(socket);
            }
        }

        @Override public void close() {
            List<Connection> pending;
            List<Socket> upgrading;
            synchronized (this) {
                if (ended) return;
                ended = true;
                pending = new ArrayList<>(accepted);
                accepted.clear();
                upgrading = new ArrayList<>(handshakes);
                notifyAll();
            }
            try { server.close(); } catch (IOException ignored) {}
            pending.forEach(Connection::abort);
            upgrading.forEach(WebSocketTransport::closeSocket);
        }
    }

    private abstract static class Queued implements Connection {
        final int limit;
        final ArrayDeque<Frame> frames = new ArrayDeque<>();
        final CompletableFuture<CloseInfo> closed = new CompletableFuture<>();
        final ReentrantLock sending = new ReentrantLock();
        CloseInfo ending;
        boolean local;

        Queued(int limit) { this.limit = limit; }

        synchronized void checkOpen() throws CloseException {
            if (ending != null) throw new CloseException(ending.code(), ending.reason());
        }

        void end(int code, String reason, boolean local) {
            CloseInfo info;
            synchronized (this) {
                if (ending != null) return;
                this.local = local;
                ending = info = new CloseInfo(code, reason);
                if (local) frames.clear();
                notifyAll();
            }
            closed.complete(info);
        }

        synchronized void publish(Frame frame) throws InterruptedException, CloseException {
            while (ending == null && frames.size() >= CAPACITY) wait();
            checkOpen();
            frames.addLast(frame);
            notifyAll();
        }

        @Override public Frame receive(Duration timeout) throws InterruptedException, TimeoutException, CloseException {
            long deadline = Timeouts.deadline(timeout);
            Frame frame;
            synchronized (this) {
                while (frames.isEmpty() || local) {
                    checkOpen();
                    Timeouts.await(this, deadline);
                }
                frame = frames.removeFirst();
                notifyAll();
            }
            consumed();
            return frame;
        }

        void consumed() {}
        @Override public CompletableFuture<CloseInfo> closed() { return closed; }
        boolean tooLarge(long size) { return size > Integer.MAX_VALUE || limit > 0 && size > limit; }

        void acquire(long deadline) throws InterruptedException, TimeoutException, CloseException {
            if (!sending.tryLock(Timeouts.remaining(deadline), TimeUnit.NANOSECONDS)) throw new TimeoutException("duplex send timed out");
            try { checkOpen(); } catch (CloseException closed) { sending.unlock(); throw closed; }
        }
    }

    private static final class Native extends Queued {
        private final Socket socket;
        private final InputStream input;
        private final OutputStream output;
        private final String protocol;
        private final boolean client;

        Native(Socket socket, InputStream input, int limit, String protocol, boolean client) throws IOException {
            super(limit);
            this.socket = socket;
            this.input = input;
            this.output = socket.getOutputStream();
            this.protocol = protocol;
            this.client = client;
        }

        void start() { Thread.ofVirtual().name("nightseam-ws-reader").start(this::read); }
        @Override public String subprotocol() { return protocol; }

        private void read() {
            ByteArrayOutputStream message = new ByteArrayOutputStream();
            int kind = 0;
            try {
                while (true) {
                    int first = octet(input), second = octet(input);
                    boolean fin = (first & 0x80) != 0;
                    int opcode = first & 15;
                    if ((first & 0x70) != 0 || ((second & 0x80) != 0) == client || opcode != 0 && opcode != 1 && opcode != 2 && opcode != 8 && opcode != 9 && opcode != 10) {
                        protocol(1002, "invalid WebSocket frame"); return;
                    }
                    long length = second & 127;
                    if (length == 126) {
                        length = (octet(input) << 8) | octet(input);
                        if (length < 126) { protocol(1002, "noncanonical frame length"); return; }
                    } else if (length == 127) {
                        length = 0;
                        for (int i = 0; i < 8; i++) {
                            int part = octet(input);
                            if (i == 0 && (part & 128) != 0) { protocol(1002, "invalid frame length"); return; }
                            length = length << 8 | part;
                        }
                        if (length < 65536) { protocol(1002, "noncanonical frame length"); return; }
                    }
                    if (opcode >= 8 && (!fin || length > 125)) { protocol(1002, "invalid control frame"); return; }
                    if (opcode < 8 && (opcode == 0 ? kind == 0 : kind != 0)) { protocol(1002, "invalid continuation"); return; }
                    if (opcode < 8 && (tooLarge(length) || tooLarge(length + message.size()))) { protocol(1009, "frame exceeds receive limit"); return; }
                    byte[] mask = client ? null : readBytes(input, 4);
                    byte[] data = readBytes(input, (int) length);
                    if (mask != null) for (int i = 0; i < data.length; i++) data[i] ^= mask[i % 4];
                    if (opcode == 8) {
                        if (data.length == 1) { protocol(1002, "invalid close frame"); return; }
                        int code = data.length == 0 ? 1005 : (data[0] & 255) << 8 | data[1] & 255;
                        String reason = data.length <= 2 ? "" : utf8(java.util.Arrays.copyOfRange(data, 2, data.length));
                        if (data.length >= 2 && !validCode(code)) { protocol(1002, "invalid close code"); return; }
                        end(code, reason, false);
                        try { writeControl(8, data); } catch (Exception ignored) {}
                        return;
                    }
                    if (opcode == 9) { writeControl(10, data); continue; }
                    if (opcode == 10) continue;
                    if (opcode != 0) kind = opcode;
                    message.writeBytes(data);
                    if (fin) {
                        byte[] complete = message.toByteArray();
                        if (kind == 1) utf8(complete);
                        publish(new Frame(kind == 1 ? "text" : "binary", complete));
                        message.reset();
                        kind = 0;
                    }
                }
            } catch (CharacterCodingException invalid) { protocol(1007, "invalid UTF-8"); }
            catch (InterruptedException interrupted) { Thread.currentThread().interrupt(); end(1006, "", false); }
            catch (IOException | TimeoutException failure) { end(1006, "", false); }
            finally { closeSocket(socket); }
        }

        @Override public void send(Frame frame, Duration timeout) throws IOException, InterruptedException, TimeoutException {
            if ("text".equals(frame.kind())) utf8(frame.data());
            write("text".equals(frame.kind()) ? 1 : 2, frame.data(), Timeouts.deadline(timeout), true);
        }

        private void writeControl(int opcode, byte[] data) throws IOException, InterruptedException, TimeoutException {
            write(opcode, data, Timeouts.deadline(CLOSE_TIMEOUT), false);
        }

        private void write(int opcode, byte[] data, long deadline, boolean check) throws IOException, InterruptedException, TimeoutException {
            if (check) acquire(deadline);
            else if (!sending.tryLock(Timeouts.remaining(deadline), TimeUnit.NANOSECONDS)) throw new TimeoutException("WebSocket write timed out");
            try {
                FutureTask<Void> writing = new FutureTask<>(() -> { writeFrame(output, opcode, data, client); return null; });
                Thread.ofVirtual().name("nightseam-ws-writer").start(writing);
                try { writing.get(Timeouts.remaining(deadline), TimeUnit.NANOSECONDS); }
                catch (ExecutionException failure) { abort(); throw io(failure.getCause()); }
                catch (TimeoutException | InterruptedException failure) { abort(); throw failure; }
            } finally { sending.unlock(); }
        }

        private void protocol(int code, String reason) {
            end(code, reason, true);
            try { writeControl(8, closePayload(code, reason)); } catch (Exception ignored) {}
            closeSocket(socket);
        }

        @Override public void close(int code, String reason) {
            validateClose(code, reason);
            synchronized (this) { if (ending != null) return; }
            end(code, reason, true);
            Thread.ofVirtual().name("nightseam-ws-close").start(() -> {
                try { writeControl(8, closePayload(code, reason)); } catch (Exception ignored) {}
                finally { closeSocket(socket); }
            });
        }
        @Override public void abort() { end(1006, "", true); closeSocket(socket); }
    }

    private static void writeFrame(OutputStream out, int opcode, byte[] data, boolean masked) throws IOException {
        int flag = masked ? 128 : 0;
        out.write(0x80 | opcode);
        if (data.length < 126) out.write(flag | data.length);
        else if (data.length <= 65535) { out.write(flag | 126); out.write(data.length >>> 8); out.write(data.length); }
        else {
            out.write(flag | 127);
            for (int shift = 56; shift >= 0; shift -= 8) out.write((int) ((long) data.length >>> shift));
        }
        if (masked) {
            byte[] mask = new byte[4];
            RANDOM.nextBytes(mask);
            out.write(mask);
            byte[] copy = data.clone();
            for (int i = 0; i < copy.length; i++) copy[i] ^= mask[i % 4];
            out.write(copy);
        } else out.write(data);
        out.flush();
    }

    private static byte[] closePayload(int code, String reason) {
        byte[] text = reason.getBytes(StandardCharsets.UTF_8);
        ByteBuffer data = ByteBuffer.allocate(2 + text.length);
        data.putShort((short) code).put(text);
        return data.array();
    }
    private static boolean validCode(int code) {
        return code >= 1000 && code <= 1014 && code != 1004 && code != 1005 && code != 1006 || code >= 3000 && code <= 4999;
    }
    private static void validateClose(int code, String reason) {
        Wires.scalar(reason);
        if (!validCode(code) || reason.getBytes(StandardCharsets.UTF_8).length > 123) throw new IllegalArgumentException("invalid WebSocket close");
    }
    private static String utf8(byte[] data) throws CharacterCodingException {
        return StandardCharsets.UTF_8.newDecoder().onMalformedInput(CodingErrorAction.REPORT).onUnmappableCharacter(CodingErrorAction.REPORT).decode(ByteBuffer.wrap(data)).toString();
    }
    private static int octet(InputStream input) throws IOException {
        int value = input.read();
        if (value < 0) throw new EOFException("WebSocket ended without close");
        return value;
    }
    private static byte[] readBytes(InputStream input, int length) throws IOException {
        byte[] data = input.readNBytes(length);
        if (data.length != length) throw new EOFException("partial WebSocket frame");
        return data;
    }
    private static String headers(InputStream input) throws IOException {
        ByteArrayOutputStream data = new ByteArrayOutputStream();
        int tail = 0;
        for (int i = 0; i < 16384; i++) {
            int next = octet(input);
            data.write(next);
            tail = tail << 8 | next;
            if (tail == 0x0d0a0d0a) return data.toString(StandardCharsets.ISO_8859_1);
        }
        throw new IOException("WebSocket headers exceed limit");
    }
    private static String acceptKey(String key) {
        return Base64.getEncoder().encodeToString(sha1((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").getBytes(StandardCharsets.US_ASCII)));
    }
    private static void validateProtocols(List<String> protocols) {
        Set<String> seen = new HashSet<>();
        for (String protocol : protocols) {
            if (!protocol.matches("[!#$%&'*+.^_`|~0-9A-Za-z-]+") || !seen.add(protocol)) throw new IllegalArgumentException("invalid subprotocol");
        }
    }
    private static Map<String, String> fields(String[] lines) throws IOException {
        Map<String, String> fields = new HashMap<>();
        for (int i = 1; i < lines.length; i++) {
            int colon = lines[i].indexOf(':');
            if (colon < 1) throw new IOException("invalid HTTP header");
            fields.merge(lines[i].substring(0, colon).toLowerCase(Locale.ROOT), lines[i].substring(colon + 1).trim(), (a, b) -> a + "," + b);
        }
        return fields;
    }
    private static boolean token(String list, String token) { return token(list, token, true); }
    private static boolean token(String list, String token, boolean ignoreCase) {
        if (list == null) return false;
        for (String item : list.split(",")) if (ignoreCase ? item.trim().equalsIgnoreCase(token) : item.trim().equals(token)) return true;
        return false;
    }
    private static byte[] sha1(byte[] data) {
        try { return MessageDigest.getInstance("SHA-1").digest(data); }
        catch (NoSuchAlgorithmException impossible) { throw new AssertionError(impossible); }
    }
    private static IOException io(Throwable cause) { return cause instanceof IOException error ? error : new IOException(cause); }
    private static void closeSocket(Socket socket) { try { socket.close(); } catch (IOException ignored) {} }
}
