package io.nightseam.runtime;

import java.math.BigDecimal;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Objects;

/** Runs the unchanged shared tables without a test-framework dependency. */
public final class SchemaTest {
    private SchemaTest() {}
    private static int assertions;

    public static void main(String[] args) throws Exception {
        Path checkout = Path.of(args.length == 0 ? "." : args[0]);
        Map<String, Object> table = Json.object(Json.parse(Files.readAllBytes(checkout.resolve("conformance/tables/validator.json"))));
        Map<String, Schema> imports = new LinkedHashMap<>();
        Json.object(table.get("imported")).forEach((name, wire) -> imports.put(name, new Schema(Json.object(wire), imports)));
        Schema schema = new Schema(Json.object(table.get("wire")), imports);
        int cases = 0;
        for (Object item : Json.array(table.get("cases"))) {
            Map<String, Object> row = Json.object(item);
            String name = "case " + cases++ + ": " + Json.stringify(row);
            Map<String, Object> slots = row.containsKey("slots") ? Json.object(row.get("slots")) : Map.of();
            String message = refusal(() -> schema.validate(row.get("expression"), row.get("value"), slots));
            equal(message == null, row.get("valid"), name);
            if (row.containsKey("message")) equal(message, row.get("message"), name);
        }
        int patterns = 0;
        for (Object item : Json.array(table.get("patterns"))) {
            Map<String, Object> row = Json.object(item); String pattern = (String) row.get("pattern");
            Object expression = Map.of("array", Map.of("nullable", Map.of("kind", "record", "fields",
                List.of(Map.of("name", "tag", "type", "string", "pattern", pattern)))));
            Map<String, Object> wire = Map.of("types", Map.of("Probe", Map.of("kind", "alias", "type", expression)));
            String constructor = refusal(() -> new Schema(wire, Map.of()));
            equal(constructor == null, row.get("valid"), "pattern construction " + pattern);
            String inline = refusal(() -> schema.validate(expression, List.of()));
            equal(inline == null, row.get("valid"), "unused inline pattern " + pattern);
            if (Boolean.FALSE.equals(row.get("valid"))) {
                check(constructor.endsWith(": outside Nightseam dialect"), "constructor dialect diagnostic");
                equal(inline, constructor, "inline dialect diagnostic");
            }
            patterns++;
        }
        int patternValues = 0;
        for (Object item : Json.array(table.get("patternValues"))) {
            Map<String, Object> row = Json.object(item); String pattern = (String) row.get("pattern");
            Object expression = Map.of("kind", "record", "fields", List.of(Map.of("name", "text", "type", "string", "required", true, "pattern", pattern)));
            String message = refusal(() -> schema.validate(expression, Map.of("text", row.get("value"))));
            equal(message == null, row.get("valid"), "pattern value " + Json.stringify(row));
            if (Boolean.FALSE.equals(row.get("valid"))) equal(message, "$.text: expected a match of " + pattern, "pattern mismatch diagnostic");
            patternValues++;
        }
        int equivalence = 0;
        for (Object item : Json.array(table.get("equivalence"))) {
            Map<String, Object> row = Json.object(item);
            for (Object value : Json.array(row.get("values"))) {
                String generic = refusal(() -> schema.validate(row.get("generic"), value));
                String bound = refusal(() -> schema.validate(row.get("bound"), value));
                equal(generic == null, bound == null, "equivalence " + Json.stringify(value));
                equivalence++;
            }
        }
        Map<String, Object> unicode = Json.object(Json.parse(Files.readAllBytes(checkout.resolve("conformance/tables/unicode.json"))));
        int unicodeRows = 0;
        for (Object item : Json.array(unicode.get("rows"))) {
            Map<String, Object> row = Json.object(item); String raw = (String) row.get("raw");
            String failure = refusal(() -> Json.parse(raw));
            equal(failure == null, row.get("valid"), "Unicode " + row.get("name"));
            if (failure == null) schema.validate("json", Json.parse(raw));
            else equal(failure, Json.UNICODE_ERROR, "Unicode diagnostic");
            equal(refusal(() -> Json.parse(raw.getBytes(StandardCharsets.UTF_8))) == null, row.get("valid"), "UTF-8 Unicode " + row.get("name"));
            unicodeRows++;
        }
        jsonTests(schema);
        bindingTests();
        System.out.println("SchemaTest: " + cases + " validator cases, " + patterns + " patterns, " + patternValues
            + " pattern values, " + equivalence + " equivalence values, " + unicodeRows + " Unicode rows; " + assertions + " assertions passed");
    }

    private static void jsonTests(Schema schema) {
        String source = "{\"first\":1e3,\"negative\":-0,\"scaled\":1.00,\"big\":9007199254740993,\"null\":null}";
        Map<String, Object> parsed = Json.object(Json.parse(source));
        check(parsed instanceof LinkedHashMap<?, ?>, "ordered object model");
        check(parsed.get("big") instanceof BigDecimal, "exact decimal model");
        equal(((BigDecimal) parsed.get("big")).toBigIntegerExact().toString(), "9007199254740993", "exact integer value");
        equal(Json.stringify(parsed), source, "numeric tokens and presence survive round trip");
        check(parsed.containsKey("null") && parsed.get("null") == null && !parsed.containsKey("absent"), "absent and null remain distinct");
        equal(Json.stringify(Json.parse("{\"x\":1,\"x\":2}")), "{\"x\":2}", "ordinary duplicate member uses last value");
        check(refusal(() -> Json.parseEnvelope("{\"kind\":\"event\",\"kind\":\"event\"}")) != null, "duplicate top-level envelope member refused");
        Json.parseEnvelope("{\"data\":{\"x\":1,\"x\":2}}");
        Json.parseEnvelope("{\"kind\":\"event\",\"data\":null}");
        for (String invalid : List.of("", "true false", "01", "1.", "1e", "1e+", "+1", "[1,]", "{\"x\":1,}", "{x:1}", "\"\\q\"", "\"line\nfeed\"", "NaN", "Infinity"))
            check(refusal(() -> Json.parse(invalid)) != null, "malformed JSON: " + invalid);
        for (byte[] invalid : List.of(new byte[]{(byte) 0xc0, (byte) 0xaf}, new byte[]{34, (byte) 0xed, (byte) 0xa0, (byte) 0x80, 34}, new byte[]{(byte) 0xff}))
            equal(refusal(() -> Json.parse(invalid)), Json.UNICODE_ERROR, "invalid UTF-8");
        for (String malformed : List.of("\ud800", "\udc00", "\ud800x\udc00")) {
            check(!Json.validUnicode(malformed), "malformed UTF-16");
            equal(refusal(() -> Json.stringify(malformed)), Json.UNICODE_ERROR, "encoder Unicode");
            equal(refusal(() -> schema.validate("string", malformed)), Json.UNICODE_ERROR, "value Unicode");
        }
        equal(refusal(() -> schema.validate("integer", Json.parse("9007199254740991.1"))), "$: expected JavaScript-safe integer", "fraction cannot round to safe integer");
        schema.validate("integer", Json.parse("90071992547409910e-1"));
        schema.validate("integer", Json.parse("1.000000000000000000000000000000000000"));
        equal(refusal(() -> schema.validate("number", Json.parse("1e400"))), "$: expected finite number", "huge decimal remains visible to validator");
        equal(refusal(() -> schema.validate("json", Json.parse("{\"n\":1e400}"))), "$.n: expected finite JSON number", "finite JSON recursively");
        for (String token : List.of("1e999999999999", "-1e-999999999999", "0e999999999999", "-0.000e-999999999999")) {
            Object number = Json.parse(token);
            check(number instanceof Number, "arbitrary exponent keeps numeric representation");
            equal(Json.stringify(number), token, "arbitrary exponent round trip");
            equal(Json.stringify(Json.parseEnvelope("{\"params\":[" + token + "]}")), "{\"params\":[" + token + "]}", "arbitrary exponent payload forwarding");
        }
        Object overflow = Json.parse("1e999999999999"), underflow = Json.parse("-1e-999999999999");
        equal(refusal(() -> schema.validate("number", overflow)), "$: expected finite number", "arbitrary exponent overflow number");
        equal(refusal(() -> schema.validate("integer", overflow)), "$: expected finite number", "arbitrary exponent overflow integer");
        equal(refusal(() -> schema.validate("json", overflow)), "$: expected finite JSON number", "arbitrary exponent overflow JSON");
        schema.validate("number", underflow);
        schema.validate("json", underflow);
        equal(refusal(() -> schema.validate("integer", underflow)), "$: expected JavaScript-safe integer", "underflow cannot turn a fraction into an integer");
        schema.validate("integer", Json.parse("0e999999999999"));
        schema.validate("integer", Json.parse("-0.000e-999999999999"));
        equal(((Number) underflow).intValue(), 0, "explicit integer conversion of raw number");
        equal(((Number) overflow).longValue(), Long.MAX_VALUE, "explicit long conversion of raw number");
        equal(((Number) overflow).floatValue(), Float.POSITIVE_INFINITY, "explicit float conversion of raw number");
        List<Object> cycle = new ArrayList<>(); cycle.add(cycle);
        check(refusal(() -> Json.stringify(cycle)) != null, "cyclic JSON encoding");
        equal(refusal(() -> schema.validate("json", cycle)), "$[0]: expected acyclic JSON", "cyclic JSON validation");
        Object recursive = Map.of("types", Map.of("Tree", Map.of("kind", "record", "fields", List.of(Map.of("name", "children", "type", Map.of("array", "Tree"))))));
        Schema trees = new Schema(Json.object(recursive), Map.of());
        trees.validate("Tree", Json.parse("{\"children\":[{\"children\":[]}]}"));
        Map<String, Object> treeCycle = new LinkedHashMap<>(); treeCycle.put("children", List.of(treeCycle));
        equal(refusal(() -> trees.validate("Tree", treeCycle)), "$.children[0]: expected acyclic JSON", "recursive record refuses a cyclic value");
    }

    private static void bindingTests() {
        Schema scalar = new Schema(Json.object(Json.parse("{\"types\":{\"Count\":{\"kind\":\"alias\",\"type\":\"integer\"}}}")), Map.of());
        Schema box = new Schema(Json.object(Json.parse("{\"types\":{\"Box\":{\"kind\":\"record\",\"parameters\":[{\"name\":\"T\"}],\"fields\":[{\"name\":\"value\",\"type\":\"T\"}]}}}")), Map.of());
        Schema cell = new Schema(Json.object(Json.parse("{\"parameters\":[{\"name\":\"T\"}],\"types\":{\"Request\":{\"kind\":\"record\",\"fields\":[{\"name\":\"value\",\"type\":\"T\"}]}}}")), Map.of());
        Map<String, Object> slots = Map.of("T", new Schema.TypeBinding(box, "Box", Map.of("T", new Schema.TypeBinding(box, "Box", Map.of("T", new Schema.TypeBinding(scalar, "Count"))))));
        cell.validate("Request", Json.parse("{\"value\":{\"value\":{\"value\":7}}}"), slots);
        equal(refusal(() -> cell.validate("Request", Json.parse("{\"value\":{\"value\":{\"value\":\"wrong\"}}}"), slots)), "$.value.value.value: expected integer", "nested lexical bindings");
        Map<String, Object> cycle = new LinkedHashMap<>(); cycle.put("T", new Schema.TypeBinding(box, "Box", cycle));
        equal(refusal(() -> cell.validate("Request", Map.of(), cycle)), "cyclic type argument bindings", "cyclic binding graph");
        Schema family = new Schema(Json.object(Json.parse("{\"types\":{\"Count\":{\"kind\":\"alias\",\"type\":\"integer\"}}}")), Map.of());
        cell.validate("S.Count", Json.parse("7"), Map.of("S", family));
        equal(refusal(() -> cell.validate("S.Count", "bad", Map.of("S", family))), "$: expected integer", "explicit family binding");
        Schema drawn = cell.bind(Map.of("S.Count", "integer"), Map.of());
        drawn.validate("S.Count", Json.parse("7"));
        Schema familyBox = new Schema(Json.object(Json.parse("{\"types\":{\"FamilyBox\":{\"kind\":\"record\",\"parameters\":[{\"name\":\"F\",\"of\":\"protocol\"}],\"fields\":[{\"name\":\"item\",\"type\":\"F.Count\",\"required\":true}]}}}")), Map.of());
        Schema appliedDraw = familyBox.bind(Map.of("S.Count", "integer"), Map.of());
        Object application = Json.parse("{\"apply\":\"FamilyBox\",\"with\":{\"F\":\"S\"}}");
        appliedDraw.validate(application, Json.parse("{\"item\":7}"));
        equal(refusal(() -> appliedDraw.validate(application, Json.parse("{\"item\":\"wrong\"}"))), "$.item: expected integer", "drawn family survives application");
        Schema callable = new Schema(Json.object(Json.parse("{\"types\":{\"Report\":{\"kind\":\"callable\",\"contract\":\"same/Report\"}}}")), "a".repeat(64), Map.of());
        callable.validate("Report", Map.of("binding", "nonce.1", "contract", "same/Report", "digest", "a".repeat(64)));
        check(refusal(() -> callable.validate("Report", Map.of("binding", "nonce.1", "contract", "same/Report", "digest", "b".repeat(64)))).contains("where " + "a".repeat(64) + " is expected"), "callable declaration revision");
    }

    private static String refusal(Runnable action) {
        try { action.run(); return null; } catch (IllegalArgumentException e) { return e.getMessage(); }
    }
    private static void check(boolean condition, String message) {
        assertions++; if (!condition) throw new AssertionError(message);
    }
    private static void equal(Object actual, Object expected, String message) {
        assertions++; if (!Objects.equals(actual, expected)) throw new AssertionError(message + "\nexpected: " + expected + "\nactual: " + actual);
    }
}
