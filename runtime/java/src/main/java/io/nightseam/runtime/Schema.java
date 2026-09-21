package io.nightseam.runtime;

import java.math.BigDecimal;
import java.math.BigInteger;
import java.time.DateTimeException;
import java.time.LocalDate;
import java.util.ArrayList;
import java.util.Collections;
import java.util.HashMap;
import java.util.HashSet;
import java.util.IdentityHashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.TreeSet;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/** Interprets a family descriptor without losing the lexical scope of generic arguments. */
public final class Schema {
    private final Map<String, Object> types;
    private final List<Object> parameters;
    private final Map<String, Schema> imports;
    private final Map<String, Argument> scope;
    private final String digest;

    /** A type supplied by another declaration, optionally with its own nested slots. */
    public record TypeBinding(Schema schema, Object type, Map<String, Object> slots) {
        public TypeBinding(Schema schema, Object type) { this(schema, type, Map.of()); }
    }

    public Schema(Map<String, Object> wire, Map<String, Schema> imports) { this(wire, "", imports); }

    public Schema(Map<String, Object> wire, String digest, Map<String, Schema> imports) {
        if (digest == null || !digest.isEmpty() && !validDigest(digest))
            throw new IllegalArgumentException("schema.digest: expected empty or lowercase SHA-256 digest");
        unicode(wire, identitySet());
        if (!(wire.get("types") instanceof Map<?, ?>)) throw new IllegalArgumentException("expected family descriptor with types");
        this.types = Json.object(wire.get("types"));
        this.parameters = list(wire.get("parameters"));
        // Imports may be filled as a group so mutually visible declarations share one registry.
        this.imports = imports == null ? Map.of() : imports;
        for (String name : this.imports.keySet()) if (!Json.validUnicode(name)) throw error(Json.UNICODE_ERROR);
        this.scope = Map.of();
        this.digest = digest;
        checkPatterns(types, identitySet());
    }

    private Schema(Schema source, Map<String, Argument> scope) {
        this.types = source.types; this.parameters = source.parameters; this.imports = source.imports;
        this.digest = source.digest; this.scope = scope;
    }

    public String digest() { return digest; }
    public void validate(Object type, Object value) { validate(type, value, Map.of()); }

    /** Slots accept TypeBinding, Schema family bindings, or {type: ...}/{family: importName}. */
    public void validate(Object type, Object value, Map<String, Object> slots) {
        validate(type, value, "$", slots);
    }

    public void validate(Object type, Object value, String location, Map<String, Object> slots) {
        unicode(type, identitySet()); unicode(value, identitySet()); acyclic(value, location, identitySet()); checkPatterns(type, identitySet());
        Map<String, Argument> bound = slots(this, slots, identitySet());
        validate(new Expression(this, type, bound, null), value, location);
    }

    /** Returns a bound view; each supplied type is interpreted in this schema's current scope. */
    public Schema bind(Map<String, Object> types, Map<String, Schema> families) {
        Map<String, Argument> bound = new LinkedHashMap<>(scope);
        Set<String> drawn = new HashSet<>();
        for (var entry : types.entrySet()) {
            unicode(entry.getKey(), identitySet()); unicode(entry.getValue(), identitySet()); checkPatterns(entry.getValue(), identitySet());
            bound.put(entry.getKey(), new Argument(expression(entry.getValue()), null, false));
            int dot = entry.getKey().indexOf('.');
            if (dot > 0 && dot < entry.getKey().length() - 1) drawn.add(entry.getKey().substring(0, dot));
        }
        for (String family : drawn) {
            Map<String, Object> members = new LinkedHashMap<>();
            for (var entry : bound.entrySet()) {
                if (!entry.getKey().startsWith(family + ".") || entry.getValue().type == null) continue;
                Expression source = entry.getValue().type;
                members.put(entry.getKey().substring(family.length() + 1), Map.of("kind", "alias", "type",
                    new TypeBinding(new Schema(source.schema, source.scope), source.value)));
            }
            bound.put(family, new Argument(null, new Schema(Map.of("types", members), Map.of()), false));
        }
        for (var entry : families.entrySet()) bound.put(entry.getKey(), new Argument(null, entry.getValue(), false));
        return new Schema(this, bound);
    }

    private Expression expression(Object type) { return new Expression(this, type, scope, null); }
    private record Argument(Expression type, Schema family, boolean unbound) {}
    private record Expression(Schema schema, Object value, Map<String, Argument> scope, Set<Map<String, Object>> aliases) {
        Expression child(Object value) { return new Expression(schema, value, scope, null); }
    }
    private record Resolved(Expression expression, Map<String, Object> definition, String name) {}
    private record Field(Map<String, Object> field, Expression expression) {}

    private static Map<String, Argument> slots(Schema owner, Map<String, Object> slots, Set<Object> seen) {
        Map<String, Argument> result = new LinkedHashMap<>(owner.scope);
        if (slots == null || slots.isEmpty()) return result;
        if (!seen.add(slots)) throw new IllegalArgumentException("cyclic type argument bindings");
        for (String name : keys(slots)) {
            if (!Json.validUnicode(name)) throw new IllegalArgumentException(Json.UNICODE_ERROR);
            Object value = slots.get(name);
            if (value instanceof Schema family) result.put(name, new Argument(null, family, false));
            else if (value instanceof TypeBinding binding) {
                if (binding.schema == null) throw bad("$", "schema for type argument");
                unicode(binding.type, identitySet()); checkPatterns(binding.type, identitySet());
                result.put(name, new Argument(new Expression(binding.schema, binding.type,
                    slots(binding.schema, binding.slots, seen), null), null, false));
            } else {
                Map<String, Object> item = map(value);
                if (item.containsKey("family")) {
                    Object family = item.get("family");
                    Schema schema = family instanceof Schema s ? s : owner.imports.get(family);
                    if (schema == null) throw bad("$", "known family argument for " + name);
                    result.put(name, new Argument(null, new Schema(schema, slots(schema, map(item.get("slots")), seen)), false));
                } else if (item.containsKey("type")) {
                    Schema schema = item.get("schema") instanceof Schema s ? s : owner;
                    Object type = item.get("type"); unicode(type, identitySet()); checkPatterns(type, identitySet());
                    result.put(name, new Argument(new Expression(schema, type,
                        slots(schema, map(item.get("slots")), seen), null), null, false));
                } else throw bad("$", "type argument");
            }
        }
        seen.remove(slots);
        return result;
    }

    private static Resolved named(Expression e, String name, String at) {
        int dot = name.indexOf('.');
        if (dot >= 0) {
            String family = name.substring(0, dot); name = name.substring(dot + 1);
            if (family.isEmpty()) throw bad(at, "known family");
            Expression caller = e; Schema schema;
            if (family.charAt(0) >= 'A' && family.charAt(0) <= 'Z') {
                Argument arg = e.scope.get(family); schema = arg == null ? null : arg.family;
                if (schema == null) throw bad(at, "a binding of the parameter " + family);
            } else {
                schema = e.schema.imports.get(family);
                if (schema == null) throw bad(at, "known family");
            }
            Map<String, Argument> scope = new LinkedHashMap<>(schema.scope);
            Map<String, Object> source = caller.schema.singleFamilyParameter();
            Map<String, Object> target = map(schema.types.get(name));
            if (family.charAt(0) >= 'a' && family.charAt(0) <= 'z' && source != null && !target.isEmpty()) {
                String sourceName = string(source.get("name"));
                Argument binding = caller.scope.get(sourceName);
                if (binding == null && unbound(caller, sourceName)) binding = new Argument(null, null, true);
                if (binding != null) {
                    List<Object> needed = new ArrayList<>(schema.freeParameters(target, identitySet()));
                    needed.addAll(list(target.get("parameters")));
                    for (Object item : needed) {
                        Map<String, Object> p = map(item);
                        if (!string(p.get("of")).isEmpty()) scope.put(string(p.get("name")), binding);
                    }
                }
            }
            e = new Expression(schema, name, scope, null);
        }
        Object definition = e.schema.types.get(name);
        if (!(definition instanceof Map<?, ?>)) throw bad(at, "known type");
        return new Resolved(e.child(name), map(definition), name);
    }

    private static Resolved resolve(Expression e, String at, boolean inheritance) {
        Set<Map<String, Object>> aliases = identitySet();
        if (e.aliases != null) aliases.addAll(e.aliases);
        for (;;) {
            Map<String, Object> definition; String name;
            if (e.value instanceof TypeBinding binding) {
                if (binding.schema == null) throw bad(at, "schema for type argument");
                e = new Expression(binding.schema, binding.type, slots(binding.schema, binding.slots, identitySet()), null);
                continue;
            }
            if (e.value instanceof String text) {
                Argument arg = e.scope.get(text);
                if (arg != null) {
                    if (arg.type == null) throw bad(at, "type argument");
                    e = arg.type; aliases = identitySet();
                    if (e.aliases != null) aliases.addAll(e.aliases);
                    continue;
                }
                int dot = text.indexOf('.');
                if (dot >= 0 && unbound(e, text.substring(0, dot))) return new Resolved(e.child("json"), null, "");
                if (Set.of("json", "string", "boolean", "number", "integer", "timestamp").contains(text)) return new Resolved(e, null, "");
                Resolved found = named(e, text, at); e = found.expression; definition = found.definition; name = found.name;
            } else if (e.value instanceof Map<?, ?>) {
                Map<String, Object> value = map(e.value);
                if (value.get("apply") instanceof String reference) {
                    Resolved target = named(e, reference, at);
                    if (!(value.get("with") instanceof Map<?, ?>)) throw bad(at, "application arguments");
                    Map<String, Object> fillers = map(value.get("with"));
                    Map<String, Argument> scope = new LinkedHashMap<>(target.expression.scope);
                    List<Object> parameters = new ArrayList<>();
                    if (inheritance || reference.contains(".")) parameters.addAll(target.expression.schema.freeParameters(target.definition, identitySet()));
                    parameters.addAll(list(target.definition.get("parameters")));
                    Set<String> allowed = new HashSet<>();
                    for (Object item : parameters) {
                        Map<String, Object> p = map(item); String parameter = string(p.get("name"));
                        allowed.add(parameter);
                        if (!fillers.containsKey(parameter)) throw bad(at, "an argument for " + parameter);
                        Object filler = fillers.get(parameter);
                        if (string(p.get("of")).isEmpty()) {
                            Set<Map<String, Object>> ancestry = identitySet(); ancestry.addAll(aliases);
                            scope.put(parameter, new Argument(new Expression(e.schema, filler, e.scope, ancestry), null, false));
                        } else {
                            if (!(filler instanceof String family)) throw bad(at, "family argument for " + parameter);
                            if (unbound(e, family)) { scope.put(parameter, new Argument(null, null, true)); continue; }
                            Argument arg = e.scope.get(family);
                            Schema schema = arg == null ? e.schema.imports.get(family) : arg.family;
                            if (schema == null) throw bad(at, "known family argument for " + parameter);
                            scope.put(parameter, new Argument(null, schema, false));
                        }
                    }
                    for (String parameter : keys(fillers)) if (!allowed.contains(parameter)) throw bad(at, "known parameter " + parameter);
                    e = new Expression(target.expression.schema, target.expression.value, scope, null);
                    definition = target.definition; name = target.name;
                } else if (value.containsKey("kind")) { definition = value; name = string(value.get("kind")); }
                else return new Resolved(e, null, "");
            } else throw bad(at, "type expression");
            inheritance = false;
            if ("alias".equals(definition.get("kind"))) {
                if (!aliases.add(definition)) throw bad(at, "acyclic type expression");
                e = e.child(definition.get("type"));
            } else return new Resolved(e, definition, name);
        }
    }

    private static boolean unbound(Expression e, String name) {
        Argument arg = e.scope.get(name);
        if (arg != null) return arg.unbound;
        return e.schema.parameters.stream().map(Schema::map)
            .anyMatch(p -> name.equals(p.get("name")) && !string(p.get("of")).isEmpty());
    }

    private Map<String, Object> singleFamilyParameter() {
        Map<String, Object> result = null;
        for (Object value : parameters) {
            Map<String, Object> p = map(value);
            if (!string(p.get("of")).isEmpty()) { if (result != null) return null; result = p; }
        }
        return result;
    }

    private List<Object> freeParameters(Map<String, Object> type, Set<Map<String, Object>> seen) {
        if (type.isEmpty() || !seen.add(type)) return List.of();
        Set<String> used = new HashSet<>();
        walkFree(type.get("type"), used, seen);
        for (Object field : list(type.get("fields"))) walkFree(map(field).get("type"), used, seen);
        for (Object base : list(type.get("extends"))) walkBase(base, used, seen);
        for (Object variant : map(type.get("variants")).values()) walkFree(variant, used, seen);
        seen.remove(type);
        return parameters.stream().filter(p -> used.contains(map(p).get("name"))).toList();
    }

    private void inheritFree(Object type, Set<String> used, Set<Map<String, Object>> seen) {
        for (Object p : freeParameters(map(type), seen)) used.add(string(map(p).get("name")));
    }
    private void walkBase(Object base, Set<String> used, Set<Map<String, Object>> seen) {
        if (base instanceof Map<?, ?> && map(base).get("with") instanceof Map<?, ?>)
            for (Object filler : map(map(base).get("with")).values()) walkFree(filler, used, seen);
        else walkFree(base, used, seen);
    }
    private void walkFree(Object value, Set<String> used, Set<Map<String, Object>> seen) {
        if (value instanceof String text) {
            int dot = text.indexOf('.'); String prefix = dot < 0 ? text : text.substring(0, dot);
            if (parameters.stream().map(Schema::map).anyMatch(p -> prefix.equals(p.get("name")))) { used.add(prefix); return; }
            if (dot < 0) { inheritFree(types.get(text), used, seen); return; }
            Schema imported = imports.get(prefix);
            if (imported != null) {
                Map<String, Object> target = map(imported.types.get(text.substring(dot + 1)));
                List<Object> needed = new ArrayList<>(imported.freeParameters(target, seen));
                needed.addAll(list(target.get("parameters")));
                Map<String, Object> source = singleFamilyParameter();
                if (source != null && needed.stream().map(Schema::map).anyMatch(p -> !string(p.get("of")).isEmpty()))
                    used.add(string(source.get("name")));
            }
        } else if (value instanceof Map<?, ?>) {
            Map<String, Object> v = map(value);
            for (String key : List.of("array", "map", "nullable")) if (v.containsKey(key)) { walkFree(v.get(key), used, seen); return; }
            if (v.get("apply") instanceof String name) {
                if (!name.contains(".")) inheritFree(types.get(name), used, seen);
                for (Object filler : map(v.get("with")).values()) walkFree(filler, used, seen);
            } else if (v.get("ref") instanceof String name) {
                Map<String, Object> entity = map(types.get(name));
                for (Object field : list(entity.get("fields"))) if (map(field).get("name").equals(entity.get("key"))) walkFree(map(field).get("type"), used, seen);
            } else if (v.containsKey("kind")) {
                for (Object field : list(v.get("fields"))) walkFree(map(field).get("type"), used, seen);
                for (Object base : list(v.get("extends"))) walkBase(base, used, seen);
                for (Object variant : map(v.get("variants")).values()) walkFree(variant, used, seen);
            }
        }
    }

    private static Resolved inherited(Expression e, Object base, String at) {
        if (base instanceof String name) {
            Resolved owner = named(e, name, at);
            if (!list(owner.definition.get("parameters")).isEmpty() || !owner.expression.schema.freeParameters(owner.definition, identitySet()).isEmpty())
                throw bad(at, "explicit application of generic base " + name);
        }
        return resolve(e.child(base), at, true);
    }

    private static List<Field> fields(Resolved r, String at, Set<Map<String, Object>> seen) {
        if (r.definition == null) throw bad(at, "record");
        if (!seen.add(r.definition)) throw bad(at, "acyclic inheritance");
        List<Field> result = new ArrayList<>();
        for (Object base : list(r.definition.get("extends"))) result.addAll(fields(inherited(r.expression, base, at), at, seen));
        for (Object field : list(r.definition.get("fields"))) result.add(new Field(map(field), r.expression.child(map(field).get("type"))));
        seen.remove(r.definition); return result;
    }

    private static Map<String, Expression> variants(Resolved r, String at, Set<Map<String, Object>> seen) {
        if (r.definition == null || !"union".equals(r.definition.get("kind"))) throw bad(at, "union");
        if (!seen.add(r.definition)) throw bad(at, "acyclic inheritance");
        Map<String, Expression> result = new LinkedHashMap<>();
        for (Object base : list(r.definition.get("extends"))) result.putAll(variants(inherited(r.expression, base, at), at, seen));
        for (var entry : map(r.definition.get("variants")).entrySet()) result.put(entry.getKey(), r.expression.child(entry.getValue()));
        seen.remove(r.definition); return result;
    }

    private static void validate(Expression e, Object value, String at) {
        Resolved r = resolve(e, at, false); e = r.expression;
        if (r.definition != null) {
            Map<String, Object> type = r.definition;
            switch (string(type.get("kind"))) {
                case "callable" -> {
                    String contract = string(type.get("contract"));
                    if (!(value instanceof Map<?, ?>)) throw bad(at, "a live reference to " + contract);
                    Map<String, Object> obj = map(value);
                    if (!(obj.get("binding") instanceof String binding) || binding.isEmpty()) throw error(at + ".binding: a live reference names the binding it refers to");
                    if (!(obj.get("contract") instanceof String actual)) throw error(at + ".contract: a live reference carries the declaration it implements");
                    if (!actual.equals(contract)) throw error(at + ".contract: the reference carries " + actual + " where " + contract + " is expected");
                    if (obj.containsKey("digest")) {
                        if (!(obj.get("digest") instanceof String digest) || !validDigest(digest)) throw error(at + ".digest: expected lowercase SHA-256 digest");
                        if (!e.schema.digest.isEmpty() && !e.schema.digest.equals(digest))
                            throw error(at + ".digest: the reference to " + contract + " carries declaration digest " + digest + " where " + e.schema.digest + " is expected");
                    }
                    for (String key : keys(obj)) if (!Set.of("binding", "contract", "digest").contains(key)) throw error(at + "." + key + ": unknown field");
                    return;
                }
                case "enum" -> {
                    if (!list(type.get("values")).contains(value)) throw bad(at, r.name);
                    return;
                }
                case "record", "entity" -> {
                    if (!(value instanceof Map<?, ?>)) throw bad(at, r.name + " object");
                    Map<String, Object> obj = map(value); Set<String> allowed = new HashSet<>();
                    for (Field f : fields(r, at, identitySet())) {
                        String name = string(f.field.get("name")), fieldAt = at + "." + name;
                        allowed.add(name);
                        if (!obj.containsKey(name)) {
                            if (Boolean.TRUE.equals(f.field.get("required"))) throw error(fieldAt + ": required field missing");
                            continue;
                        }
                        Object member = obj.get(name);
                        if (member == null) {
                            if (Boolean.TRUE.equals(f.field.get("nullable"))) continue;
                            Resolved resolved = resolve(f.expression, fieldAt, false);
                            if (!map(resolved.expression.value).containsKey("nullable")) throw error(fieldAt + ": null is not permitted");
                        }
                        validate(f.expression, member, fieldAt);
                        if (member != null) constrain(f.field, member, fieldAt);
                    }
                    for (String key : keys(obj)) if (!allowed.contains(key)) {
                        if (!Boolean.TRUE.equals(type.get("open"))) throw error(at + "." + key + ": unknown field");
                        jsonValue(obj.get(key), at + "." + key, identitySet());
                    }
                    return;
                }
                case "union" -> {
                    if (!(value instanceof Map<?, ?>)) throw bad(at, r.name + " object");
                    Map<String, Object> obj = map(value); String tag = string(type.get("tag"));
                    if (!obj.containsKey(tag)) throw error(at + "." + tag + ": required field missing");
                    if (!(obj.get(tag) instanceof String tagName)) throw bad(at + "." + tag, "known variant");
                    Expression variant = variants(r, at, identitySet()).get(tagName);
                    if (variant == null) throw bad(at + "." + tag, "known variant");
                    String member = string(type.get("value")); if (member.isEmpty()) member = "value";
                    Map<String, Object> marker = map(variant.value);
                    boolean empty = marker.size() == 1 && Boolean.TRUE.equals(marker.get("empty"));
                    if (!empty) {
                        if (!obj.containsKey(member)) throw error(at + "." + member + ": required field missing");
                        validate(variant, obj.get(member), at + "." + member);
                    }
                    for (String key : keys(obj)) if (!key.equals(tag) && (empty || !key.equals(member))) throw error(at + "." + key + ": unknown field");
                    return;
                }
                default -> throw bad(at, "supported type");
            }
        }
        if (e.value instanceof Map<?, ?>) {
            Map<String, Object> type = map(e.value);
            if (type.containsKey("nullable")) { if (value != null) validate(e.child(type.get("nullable")), value, at); return; }
            if (type.containsKey("literal")) {
                if (!(type.get("literal") instanceof String literal) || literal.isEmpty()) throw bad(at, "nonempty string literal");
                if (!literal.equals(value)) throw bad(at, "literal " + diagnostic(literal));
                return;
            }
            if (type.containsKey("array")) {
                if (!(value instanceof List<?> items)) throw bad(at, "array");
                for (int i = 0; i < items.size(); i++) validate(e.child(type.get("array")), items.get(i), at + "[" + i + "]");
                return;
            }
            if (type.containsKey("map")) {
                if (!(value instanceof Map<?, ?>)) throw bad(at, "object");
                for (String key : keys(map(value))) validate(e.child(type.get("map")), map(value).get(key), at + "." + key);
                return;
            }
            if (type.get("ref") instanceof String entity) {
                Resolved target;
                try { target = named(e, entity, at); } catch (IllegalArgumentException ex) { throw bad(at, "known entity"); }
                for (Field field : fields(resolve(target.expression, at, false), at, identitySet()))
                    if (field.field.get("name").equals(target.definition.get("key"))) { validate(field.expression, value, at); return; }
                throw bad(at, "entity with a key");
            }
            if (type.containsKey("empty")) {
                if (!(value instanceof Map<?, ?> obj) || !obj.isEmpty()) throw bad(at, "empty object");
                return;
            }
            throw bad(at, "supported type expression");
        }
        switch ((String) e.value) {
            case "json" -> jsonValue(value, at, identitySet());
            case "string" -> { if (!(value instanceof String)) throw bad(at, "string"); }
            case "boolean" -> { if (!(value instanceof Boolean)) throw bad(at, "boolean"); }
            case "number", "integer" -> {
                if (!(value instanceof Number number)) throw bad(at, (String) e.value);
                if (!Double.isFinite(number.doubleValue())) throw bad(at, "finite number");
                if (e.value.equals("integer") && !safeInteger(number)) throw bad(at, "JavaScript-safe integer");
            }
            case "timestamp" -> {
                if (!(value instanceof String text)) throw bad(at, "timestamp");
                if (!timestamp(text)) throw bad(at, "RFC3339 timestamp");
            }
            default -> throw bad(at, "known type");
        }
    }

    private static final BigDecimal MAX_SAFE_INTEGER = new BigDecimal("9007199254740991");
    private static boolean safeInteger(Number number) {
        try {
            BigDecimal decimal = number instanceof BigDecimal d ? d : new BigDecimal(number.toString());
            return decimal.abs().compareTo(MAX_SAFE_INTEGER) <= 0 && decimal.stripTrailingZeros().scale() <= 0;
        } catch (NumberFormatException | ArithmeticException ex) {
            // A zero mantissa is an integer even with an exponent outside BigDecimal's
            // range. Every nonzero value at that magnitude is too large or fractional.
            String token = number.toString();
            int exponent = Math.max(token.indexOf('e'), token.indexOf('E'));
            return exponent >= 0 && token.substring(0, exponent).matches("-?0(?:\\.0+)?");
        }
    }

    private static final Pattern TIMESTAMP = Pattern.compile("^(\\d{4})-(\\d\\d)-(\\d\\d)T(\\d\\d):(\\d\\d):(\\d\\d)(?:[.,]\\d+)?(?:Z|[+-](\\d\\d):(\\d\\d))$");
    private static boolean timestamp(String value) {
        Matcher m = TIMESTAMP.matcher(value);
        if (!m.matches()) return false;
        try {
            LocalDate.of(Integer.parseInt(m.group(1)), Integer.parseInt(m.group(2)), Integer.parseInt(m.group(3)));
            return Integer.parseInt(m.group(4)) <= 23 && Integer.parseInt(m.group(5)) <= 59 && Integer.parseInt(m.group(6)) <= 59
                && (m.group(7) == null || Integer.parseInt(m.group(7)) <= 24 && Integer.parseInt(m.group(8)) <= 60);
        } catch (DateTimeException ex) { return false; }
    }

    private static void constrain(Map<String, Object> field, Object value, String at) {
        for (String bound : List.of("min", "max")) {
            Object limit = field.get(bound); if (limit == null) continue;
            boolean minimum = bound.equals("min");
            if (value instanceof Number n && limit instanceof Number l) {
                if (minimum ? n.doubleValue() < l.doubleValue() : n.doubleValue() > l.doubleValue())
                    throw error(at + ": expected at " + (minimum ? "least " : "most ") + limit);
            } else if (value instanceof String text) {
                int comparison = text.compareTo(limit.toString());
                if (minimum ? comparison < 0 : comparison > 0) throw error(at + ": expected at or " + (minimum ? "after " : "before ") + limit);
            }
        }
        Map<String, Object> length = map(field.get("length"));
        int count = value instanceof String text ? text.codePointCount(0, text.length()) : value instanceof List<?> items ? items.size() : -1;
        if (count >= 0) {
            if (length.get("min") instanceof Number min && count < min.intValue()) throw error(at + ": expected a length of at least " + min);
            if (length.get("max") instanceof Number max && count > max.intValue()) throw error(at + ": expected a length of at most " + max);
        }
        if (value instanceof String text && field.get("pattern") instanceof String pattern && !pattern.isEmpty()
            && !new Dialect(pattern).matches(text)) throw error(at + ": expected a match of " + pattern);
    }

    private static void jsonValue(Object value, String at, Set<Object> seen) {
        if (value == null || value instanceof String || value instanceof Boolean) return;
        if (value instanceof Number n) { if (!Double.isFinite(n.doubleValue())) throw bad(at, "finite JSON number"); return; }
        if (!seen.add(value)) throw bad(at, "acyclic JSON");
        if (value instanceof List<?> items) {
            for (int i = 0; i < items.size(); i++) jsonValue(items.get(i), at + "[" + i + "]", seen);
        } else if (value instanceof Map<?, ?>) {
            for (String key : keys(map(value))) jsonValue(map(value).get(key), at + "." + key, seen);
        } else throw bad(at, "acyclic JSON");
        seen.remove(value);
    }

    private static void unicode(Object value, Set<Object> seen) {
        if (value instanceof String text) { if (!Json.validUnicode(text)) throw error(Json.UNICODE_ERROR); return; }
        if (value == null || !seen.add(value)) return;
        if (value instanceof Map<?, ?> map) for (var entry : map.entrySet()) { unicode(entry.getKey(), seen); unicode(entry.getValue(), seen); }
        else if (value instanceof List<?> items) for (Object item : items) unicode(item, seen);
    }
    private static void acyclic(Object value, String at, Set<Object> active) {
        if (!(value instanceof Map<?, ?>) && !(value instanceof List<?>)) return;
        if (!active.add(value)) throw bad(at, "acyclic JSON");
        if (value instanceof Map<?, ?>) {
            for (String key : keys(map(value))) acyclic(map(value).get(key), at + "." + key, active);
        } else {
            List<?> items = (List<?>) value;
            for (int i = 0; i < items.size(); i++) acyclic(items.get(i), at + "[" + i + "]", active);
        }
        active.remove(value);
    }
    private static void checkPatterns(Object value, Set<Object> seen) {
        if (value == null || !seen.add(value)) return;
        if (value instanceof Map<?, ?>) {
            Map<String, Object> obj = map(value);
            if (obj.get("pattern") instanceof String pattern) new Dialect(pattern);
            for (String key : keys(obj)) checkPatterns(obj.get(key), seen);
        } else if (value instanceof List<?> items) for (Object item : items) checkPatterns(item, seen);
    }
    private static boolean validDigest(String value) { return value.matches("[0-9a-f]{64}"); }
    private static String diagnostic(String value) {
        return Json.stringify(value).replace("<", "\\u003c").replace(">", "\\u003e").replace("&", "\\u0026")
            .replace("\u2028", "\\u2028").replace("\u2029", "\\u2029");
    }
    private static IllegalArgumentException bad(String at, String want) { return error(at + ": expected " + want); }
    private static IllegalArgumentException error(String message) { return new IllegalArgumentException(message); }
    private static String string(Object value) { return value instanceof String s ? s : ""; }
    private static Map<String, Object> map(Object value) { return value instanceof Map<?, ?> ? Json.object(value) : Map.of(); }
    private static List<Object> list(Object value) { return value instanceof List<?> ? Json.array(value) : List.of(); }
    private static List<String> keys(Map<String, ?> value) { List<String> keys = new ArrayList<>(value.keySet()); Collections.sort(keys); return keys; }
    private static <T> Set<T> identitySet() { return Collections.newSetFromMap(new IdentityHashMap<>()); }

    // The declaration dialect is parsed directly: Java's regular-expression dialect has
    // different whitespace, word boundaries, escapes and counted-repetition limits.
    private static final class Dialect {
        private final String source;
        private int pos;
        private final Node root;
        Dialect(String source) {
            this.source = source;
            try { root = disjunction(false); }
            catch (IllegalArgumentException ex) { throw error("pattern " + diagnostic(source) + ": outside Nightseam dialect"); }
        }
        boolean matches(String value) {
            Match state = new Match(value.codePoints().toArray());
            for (int start = 0; start <= state.input.length; start++) if (!state.ends(root, start).isEmpty()) return true;
            return false;
        }
        private Node disjunction(boolean group) {
            List<Node> branches = new ArrayList<>(), sequence = new ArrayList<>();
            while (pos < source.length()) {
                if (take(')')) { if (!group) throw error("unmatched group"); branches.add(Node.join('s', sequence)); return Node.join('|', branches); }
                if (take('|')) { branches.add(Node.join('s', sequence)); sequence = new ArrayList<>(); continue; }
                Node atom = atom();
                long min = 0, max = 1; boolean repeat = true, unlimited = false;
                if (take('*')) unlimited = true;
                else if (take('+')) { min = 1; unlimited = true; }
                else if (take('?')) { /* zero or one */ }
                else if (take('{')) {
                    BigInteger lower = decimal(), upper = lower; min = saturated(lower);
                    if (take(',')) {
                        if (pos < source.length() && digit(source.charAt(pos))) upper = decimal();
                        else unlimited = true;
                    }
                    if (!take('}') || !unlimited && lower.compareTo(upper) > 0) throw error("invalid repetition");
                    max = saturated(upper);
                } else repeat = false;
                if (repeat) {
                    if (atom.kind == '^' || atom.kind == '$' || atom.kind == 'b' || atom.kind == 'B') throw error("repeated assertion");
                    take('?'); atom = Node.repeat(atom, min, max, unlimited);
                }
                sequence.add(atom);
            }
            if (group) throw error("unclosed group");
            branches.add(Node.join('s', sequence)); return Node.join('|', branches);
        }
        private Node atom() {
            if (source.startsWith("[[:", pos)) throw error("POSIX class");
            int c = source.codePointAt(pos); pos += Character.charCount(c);
            return switch (c) {
                case '^', '$' -> new Node((char) c, List.of(), List.of(), 0, 0, false, 0);
                case '.' -> Node.set(complement(List.of(new Span(10, 10), new Span(13, 13), new Span(0x2028, 0x2029))));
                case '(' -> {
                    if (take('?') && !take(':')) throw error("unsupported group");
                    yield disjunction(true);
                }
                case '[' -> Node.set(characterClass());
                case '\\' -> escape(false);
                case '*', '+', '?', '{', '}', ']' -> throw error("unescaped syntax");
                default -> Node.set(List.of(new Span(c, c)));
            };
        }
        private List<Span> characterClass() {
            boolean negate = take('^'); List<Span> result = new ArrayList<>();
            while (pos < source.length() && source.charAt(pos) != ']') {
                List<Span> left = classAtom();
                if (pos + 1 < source.length() && source.charAt(pos) == '-' && source.charAt(pos + 1) != ']') {
                    pos++; List<Span> right = classAtom();
                    if (!single(left) || !single(right) || left.getFirst().lo > right.getFirst().lo) throw error("invalid class range");
                    result.add(new Span(left.getFirst().lo, right.getFirst().lo));
                } else result.addAll(left);
            }
            if (!take(']')) throw error("unclosed class");
            return negate ? complement(result) : normalize(result);
        }
        private List<Span> classAtom() {
            int c = source.codePointAt(pos); pos += Character.charCount(c);
            return c == '\\' ? escape(true).set : List.of(new Span(c, c));
        }
        private Node escape(boolean inClass) {
            if (pos == source.length()) throw error("trailing escape");
            char c = source.charAt(pos++);
            List<Span> set = switch (c) {
                case 'd', 'D' -> DIGITS;
                case 'w', 'W' -> WORDS;
                case 's', 'S' -> WHITESPACE;
                default -> null;
            };
            if (set != null) return Node.set(Character.isUpperCase(c) ? complement(set) : set);
            if (!inClass && (c == 'b' || c == 'B')) return new Node(c, List.of(), List.of(), 0, 0, false, 0);
            int scalar;
            switch (c) {
                case 'b' -> { if (!inClass) throw error("invalid escape"); scalar = 8; }
                case 'f' -> scalar = 12;
                case 'n' -> scalar = 10;
                case 'r' -> scalar = 13;
                case 't' -> scalar = 9;
                case 'v' -> scalar = 11;
                case '0' -> { if (pos < source.length() && digit(source.charAt(pos))) throw error("octal escape"); scalar = 0; }
                case 'c' -> {
                    if (pos == source.length() || !asciiLetter(source.charAt(pos))) throw error("invalid control escape");
                    scalar = source.charAt(pos++) & 31;
                }
                case 'x' -> scalar = hex(2);
                case 'u' -> {
                    if (take('{')) {
                        int start = pos; while (pos < source.length() && hexDigit(source.charAt(pos)) >= 0) pos++;
                        if (pos == start || !take('}')) throw error("invalid Unicode escape");
                        BigInteger code = new BigInteger(source.substring(start, pos - 1), 16);
                        if (code.compareTo(BigInteger.valueOf(0x10ffff)) > 0) throw error("Unicode escape out of range");
                        scalar = code.intValue();
                    } else {
                        scalar = hex(4);
                        if (scalar >= 0xd800 && scalar <= 0xdbff && source.startsWith("\\u", pos)) {
                            int saved = pos; pos += 2;
                            try {
                                int low = hex(4);
                                if (low >= 0xdc00 && low <= 0xdfff) scalar = Character.toCodePoint((char) scalar, (char) low);
                                else pos = saved;
                            } catch (IllegalArgumentException ex) { pos = saved; }
                        }
                    }
                }
                default -> {
                    if ("^$\\.*+?()[]{}|/".indexOf(c) < 0 && !(inClass && c == '-')) throw error("invalid escape");
                    scalar = c;
                }
            }
            return Node.set(List.of(new Span(scalar, scalar)));
        }
        private int hex(int count) {
            int value = 0;
            for (int i = 0; i < count; i++) {
                if (pos == source.length()) throw error("invalid hex escape");
                int digit = hexDigit(source.charAt(pos++)); if (digit < 0) throw error("invalid hex escape");
                value = value * 16 + digit;
            }
            return value;
        }
        private BigInteger decimal() {
            int start = pos; while (pos < source.length() && digit(source.charAt(pos))) pos++;
            if (pos == start) throw error("missing repetition bound");
            return new BigInteger(source.substring(start, pos));
        }
        private boolean take(char c) { if (pos < source.length() && source.charAt(pos) == c) { pos++; return true; } return false; }
        private static boolean digit(char c) { return c >= '0' && c <= '9'; }
        private static boolean asciiLetter(char c) { return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'; }
        private static int hexDigit(char c) { return digit(c) ? c - '0' : c >= 'a' && c <= 'f' ? c - 'a' + 10 : c >= 'A' && c <= 'F' ? c - 'A' + 10 : -1; }
        private static long saturated(BigInteger n) { return n.bitLength() > 63 ? Long.MAX_VALUE : n.longValue(); }
        private record Span(int lo, int hi) {}
        private static final List<Span> DIGITS = List.of(new Span('0', '9'));
        private static final List<Span> WORDS = List.of(new Span('0', '9'), new Span('A', 'Z'), new Span('_', '_'), new Span('a', 'z'));
        private static final List<Span> WHITESPACE = List.of(new Span(9, 13), new Span(32, 32), new Span(0xa0, 0xa0),
            new Span(0x1680, 0x1680), new Span(0x2000, 0x200a), new Span(0x2028, 0x2029), new Span(0x202f, 0x202f),
            new Span(0x205f, 0x205f), new Span(0x3000, 0x3000), new Span(0xfeff, 0xfeff));
        private static boolean single(List<Span> spans) { return spans.size() == 1 && spans.getFirst().lo == spans.getFirst().hi; }
        private static List<Span> normalize(List<Span> spans) {
            List<Span> sorted = new ArrayList<>(spans); sorted.sort((a, b) -> Integer.compare(a.lo, b.lo));
            List<Span> result = new ArrayList<>();
            for (Span span : sorted) {
                if (!result.isEmpty() && span.lo <= result.getLast().hi + 1) {
                    Span last = result.removeLast(); result.add(new Span(last.lo, Math.max(last.hi, span.hi)));
                } else result.add(span);
            }
            return result;
        }
        private static List<Span> complement(List<Span> spans) {
            List<Span> result = new ArrayList<>(); int next = 0;
            for (Span span : normalize(spans)) { if (next < span.lo) result.add(new Span(next, span.lo - 1)); next = span.hi + 1; }
            if (next <= 0x10ffff) result.add(new Span(next, 0x10ffff)); return result;
        }
        private static final class Node {
            final char kind;
            final List<Span> set;
            final List<Node> children;
            final long min, max, width;
            final boolean unlimited;
            Node(char kind, List<Span> set, List<Node> children, long min, long max, boolean unlimited, long width) {
                this.kind = kind; this.set = set; this.children = children; this.min = min; this.max = max; this.unlimited = unlimited; this.width = width;
            }
            static Node set(List<Span> spans) { return new Node('c', normalize(spans), List.of(), 0, 0, false, 1); }
            static Node join(char kind, List<Node> children) {
                if (children.size() == 1) return children.getFirst();
                long width = kind == '|' ? Long.MAX_VALUE : 0;
                for (Node n : children) width = kind == '|' ? Math.min(width, n.width) : add(width, n.width);
                return new Node(kind, List.of(), children, 0, 0, false, width);
            }
            static Node repeat(Node child, long min, long max, boolean unlimited) {
                long width = min != 0 && child.width > Long.MAX_VALUE / min ? Long.MAX_VALUE : child.width * min;
                return new Node('r', List.of(), List.of(child), min, max, unlimited, width);
            }
            static long add(long a, long b) { return a > Long.MAX_VALUE - b ? Long.MAX_VALUE : a + b; }
        }
        private record MatchKey(Node node, int start) {}
        private static final class Match {
            final int[] input;
            final Map<MatchKey, Set<Integer>> memo = new HashMap<>();
            Match(int[] input) { this.input = input; }
            Set<Integer> ends(Node node, int start) {
                if (node.width > input.length - start) return Set.of();
                MatchKey key = new MatchKey(node, start); Set<Integer> cached = memo.get(key); if (cached != null) return cached;
                Set<Integer> result = new TreeSet<>();
                switch (node.kind) {
                    case 'c' -> { if (start < input.length && node.set.stream().anyMatch(s -> input[start] >= s.lo && input[start] <= s.hi)) result.add(start + 1); }
                    case '^' -> { if (start == 0) result.add(start); }
                    case '$' -> { if (start == input.length) result.add(start); }
                    case 'b', 'B' -> { if ((word(start - 1) != word(start)) == (node.kind == 'b')) result.add(start); }
                    case 's' -> {
                        result.add(start);
                        for (Node child : node.children) { result = advance(child, result); if (result.isEmpty()) break; }
                    }
                    case '|' -> { for (Node child : node.children) result.addAll(ends(child, start)); }
                    case 'r' -> {
                        Set<Integer> current = Set.of(start); long count = 0;
                        while (count < node.min && !current.isEmpty()) {
                            Set<Integer> next = advance(node.children.getFirst(), current); count++;
                            if (next.equals(current)) { count = node.min; current = next; break; }
                            current = next;
                        }
                        if (!current.isEmpty()) {
                            result.addAll(current);
                            while (node.unlimited || count < node.max) {
                                Set<Integer> next = advance(node.children.getFirst(), current);
                                if (next.isEmpty() || next.equals(current)) break;
                                result.addAll(next); current = next; count++;
                            }
                        }
                    }
                    default -> throw new AssertionError(node.kind);
                }
                memo.put(key, result); return result;
            }
            Set<Integer> advance(Node child, Set<Integer> positions) {
                Set<Integer> result = new TreeSet<>(); for (int start : positions) result.addAll(ends(child, start)); return result;
            }
            boolean word(int index) {
                if (index < 0 || index >= input.length) return false;
                int c = input[index]; return c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c == '_' || c >= 'a' && c <= 'z';
            }
        }
    }
}
