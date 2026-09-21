package io.nightseam.runtime;

import java.math.BigDecimal;
import java.math.BigInteger;
import java.nio.ByteBuffer;
import java.nio.charset.CharacterCodingException;
import java.nio.charset.CodingErrorAction;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Collections;
import java.util.IdentityHashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;

/** JSON values retain field presence, exact decimal values and Unicode scalar strings. */
public final class Json {
    private Json() {}
    static final String UNICODE_ERROR = "invalid Unicode: expected Unicode scalar strings";

    public static Object parse(String text) { return new Parser(text, false).parse(); }
    public static Object parse(byte[] bytes) { return parse(decode(bytes)); }

    /** The profile forbids duplicate envelope members; nested payloads retain ordinary JSON semantics. */
    public static Map<String, Object> parseEnvelope(String text) {
        return object(new Parser(text, true).parse());
    }
    public static Map<String, Object> parseEnvelope(byte[] bytes) { return parseEnvelope(decode(bytes)); }

    private static String decode(byte[] bytes) {
        try {
            return StandardCharsets.UTF_8.newDecoder().onMalformedInput(CodingErrorAction.REPORT)
                .onUnmappableCharacter(CodingErrorAction.REPORT).decode(ByteBuffer.wrap(bytes)).toString();
        } catch (CharacterCodingException e) {
            throw new IllegalArgumentException(UNICODE_ERROR, e);
        }
    }

    public static boolean validUnicode(String value) {
        if (value == null) return false;
        for (int i = 0; i < value.length(); i++) {
            char c = value.charAt(i);
            if (Character.isHighSurrogate(c)) {
                if (++i == value.length() || !Character.isLowSurrogate(value.charAt(i))) return false;
            } else if (Character.isLowSurrogate(c)) return false;
        }
        return true;
    }

    @SuppressWarnings("unchecked")
    public static Map<String, Object> object(Object value) {
        if (!(value instanceof Map<?, ?> map) || map.keySet().stream().anyMatch(k -> !(k instanceof String)))
            throw new IllegalArgumentException("expected JSON object");
        return (Map<String, Object>) map;
    }

    @SuppressWarnings("unchecked")
    public static List<Object> array(Object value) {
        if (!(value instanceof List<?>)) throw new IllegalArgumentException("expected JSON array");
        return (List<Object>) value;
    }

    public static String stringify(Object value) {
        StringBuilder out = new StringBuilder();
        write(value, out, Collections.newSetFromMap(new IdentityHashMap<>()));
        return out.toString();
    }

    private static void write(Object value, StringBuilder out, Set<Object> seen) {
        if (value == null) { out.append("null"); return; }
        if (value instanceof String text) { quote(text, out); return; }
        if (value instanceof Boolean) { out.append(value); return; }
        if (value instanceof RawNumber || value instanceof BigDecimal || value instanceof BigInteger || value instanceof Byte
            || value instanceof Short || value instanceof Integer || value instanceof Long) {
            out.append(value); return;
        }
        if (value instanceof Double n) {
            if (!Double.isFinite(n)) throw new IllegalArgumentException("expected finite JSON number");
            out.append(n); return;
        }
        if (value instanceof Float n) {
            if (!Float.isFinite(n)) throw new IllegalArgumentException("expected finite JSON number");
            out.append(n); return;
        }
        if (!seen.add(value)) throw new IllegalArgumentException("expected acyclic JSON");
        try {
            if (value instanceof Map<?, ?> map) {
                out.append('{'); boolean first = true;
                for (Map.Entry<?, ?> entry : map.entrySet()) {
                    if (!(entry.getKey() instanceof String key)) throw new IllegalArgumentException("expected string JSON member name");
                    if (!first) out.append(','); first = false;
                    quote(key, out); out.append(':'); write(entry.getValue(), out, seen);
                }
                out.append('}');
            } else if (value instanceof List<?> list) {
                out.append('['); boolean first = true;
                for (Object item : list) {
                    if (!first) out.append(','); first = false; write(item, out, seen);
                }
                out.append(']');
            } else throw new IllegalArgumentException("expected JSON value");
        } finally { seen.remove(value); }
    }

    private static void quote(String text, StringBuilder out) {
        if (!validUnicode(text)) throw new IllegalArgumentException(UNICODE_ERROR);
        out.append('"');
        for (int i = 0; i < text.length(); i++) {
            char c = text.charAt(i);
            switch (c) {
                case '"' -> out.append("\\\"");
                case '\\' -> out.append("\\\\");
                case '\b' -> out.append("\\b");
                case '\f' -> out.append("\\f");
                case '\n' -> out.append("\\n");
                case '\r' -> out.append("\\r");
                case '\t' -> out.append("\\t");
                default -> {
                    if (c < 32) out.append(String.format("\\u%04x", (int) c));
                    else out.append(c);
                }
            }
        }
        out.append('"');
    }

    /** The numeric value is exact and its original JSON token survives forwarding. */
    private static final class Decimal extends BigDecimal {
        private static final long serialVersionUID = 1L;
        private final String token;
        Decimal(String token) { super(token); this.token = token; }
        @Override public String toString() { return token; }
    }

    /** JSON permits exponents beyond BigDecimal's scale range; forwarding retains their exact tokens. */
    private static final class RawNumber extends Number {
        private static final long serialVersionUID = 1L;
        private final String token;
        RawNumber(String token) { this.token = token; }
        @Override public String toString() { return token; }
        @Override public double doubleValue() { return Double.parseDouble(token); }
        @Override public float floatValue() { return Float.parseFloat(token); }
        @Override public int intValue() { return (int) doubleValue(); }
        @Override public long longValue() { return (long) doubleValue(); }
    }

    private static final class Parser {
        private final String text;
        private final boolean envelope;
        private int pos;
        Parser(String text, boolean envelope) {
            if (!validUnicode(text)) throw new IllegalArgumentException(UNICODE_ERROR);
            this.text = text; this.envelope = envelope;
        }
        Object parse() {
            Object value = value(0); space();
            if (pos != text.length()) throw error("expected exactly one JSON value");
            return value;
        }
        private Object value(int depth) {
            space();
            if (depth > 1000) throw error("JSON nesting exceeds limit");
            if (pos == text.length()) throw error("expected JSON value");
            char c = text.charAt(pos);
            if (c == '"') return string();
            if (c == '{') {
                pos++; space(); Map<String, Object> map = new LinkedHashMap<>();
                if (take('}')) return map;
                do {
                    space();
                    if (pos == text.length() || text.charAt(pos) != '"') throw error("expected JSON member name");
                    String key = string(); space(); require(':');
                    Object member = value(depth + 1);
                    if (envelope && depth == 0 && map.containsKey(key)) throw error("duplicate envelope member " + key);
                    map.put(key, member); space();
                    if (take('}')) return map;
                    require(',');
                } while (true);
            }
            if (c == '[') {
                pos++; space(); List<Object> list = new ArrayList<>();
                if (take(']')) return list;
                do {
                    list.add(value(depth + 1)); space();
                    if (take(']')) return list;
                    require(',');
                } while (true);
            }
            if (text.startsWith("true", pos)) { pos += 4; return true; }
            if (text.startsWith("false", pos)) { pos += 5; return false; }
            if (text.startsWith("null", pos)) { pos += 4; return null; }
            int start = pos;
            take('-');
            if (!take('0')) {
                if (pos == text.length() || text.charAt(pos) < '1' || text.charAt(pos) > '9') throw error("expected JSON number");
                digits();
            }
            if (take('.')) { int before = pos; digits(); if (before == pos) throw error("expected fraction digits"); }
            if (take('e') || take('E')) {
                if (!take('+')) take('-');
                int before = pos; digits(); if (before == pos) throw error("expected exponent digits");
            }
            String token = text.substring(start, pos);
            try { return new Decimal(token); }
            catch (NumberFormatException e) { return new RawNumber(token); }
        }
        private String string() {
            require('"'); StringBuilder out = new StringBuilder();
            while (pos < text.length()) {
                char c = text.charAt(pos++);
                if (c == '"') {
                    String result = out.toString();
                    if (!validUnicode(result)) throw new IllegalArgumentException(UNICODE_ERROR);
                    return result;
                }
                if (c < 32) throw error("unescaped control character");
                if (c != '\\') { out.append(c); continue; }
                if (pos == text.length()) throw error("unterminated escape");
                switch (text.charAt(pos++)) {
                    case '"' -> out.append('"');
                    case '\\' -> out.append('\\');
                    case '/' -> out.append('/');
                    case 'b' -> out.append('\b');
                    case 'f' -> out.append('\f');
                    case 'n' -> out.append('\n');
                    case 'r' -> out.append('\r');
                    case 't' -> out.append('\t');
                    case 'u' -> {
                        char unit = hex();
                        if (Character.isLowSurrogate(unit)) throw new IllegalArgumentException(UNICODE_ERROR);
                        out.append(unit);
                        if (Character.isHighSurrogate(unit)) {
                            if (pos + 2 > text.length() || text.charAt(pos) != '\\' || text.charAt(pos + 1) != 'u')
                                throw new IllegalArgumentException(UNICODE_ERROR);
                            pos += 2; char low = hex();
                            if (!Character.isLowSurrogate(low)) throw new IllegalArgumentException(UNICODE_ERROR);
                            out.append(low);
                        }
                    }
                    default -> throw error("invalid JSON escape");
                }
            }
            throw error("unterminated JSON string");
        }
        private char hex() {
            int result = 0;
            for (int n = 0; n < 4; n++) {
                if (pos == text.length()) throw error("invalid Unicode escape");
                char c = text.charAt(pos++);
                int v = c >= '0' && c <= '9' ? c - '0' : c >= 'a' && c <= 'f' ? c - 'a' + 10 : c >= 'A' && c <= 'F' ? c - 'A' + 10 : -1;
                if (v < 0) throw error("invalid Unicode escape");
                result = result * 16 + v;
            }
            return (char) result;
        }
        private void digits() { while (pos < text.length() && text.charAt(pos) >= '0' && text.charAt(pos) <= '9') pos++; }
        private void space() { while (pos < text.length() && " \t\r\n".indexOf(text.charAt(pos)) >= 0) pos++; }
        private boolean take(char c) { if (pos < text.length() && text.charAt(pos) == c) { pos++; return true; } return false; }
        private void require(char c) { if (!take(c)) throw error("expected '" + c + "'"); }
        private IllegalArgumentException error(String message) { return new IllegalArgumentException(message + " at character " + pos); }
    }
}
