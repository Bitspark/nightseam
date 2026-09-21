#include <nightseam/runtime/schema.hpp>
#include <nightseam/runtime/envelope.hpp>
#include "pattern.hpp"
#include <algorithm>
#include <charconv>
#include <cmath>
#include <cstdlib>
#include <functional>
#include <set>
#include <stdexcept>
#include <vector>

namespace nightseam::runtime {
namespace {
const Value null_value = Value::null();
const Value empty_array(jsoncons::json_array_arg);
const Value& member(const Value& value, const std::string& key) {
    return value.is_object() && value.contains(key) ? value.at(key) : null_value;
}
const Value& array(const Value& value) { return value.is_array() ? value : empty_array; }
std::string text(const Value& value) { return is_text(value) ? value.as<std::string>() : ""; }
bool flag(const Value& value) { return value.is_bool() && value.as<bool>(); }
[[noreturn]] void refuse(const std::string& location, const std::string& fact) {
    throw std::invalid_argument(location + ": " + fact);
}
[[noreturn]] void expected(const std::string& location, const std::string& want) { refuse(location, "expected " + want); }
std::u16string utf16(std::string_view text) {
    std::u16string result;
    for (auto c : decode_utf8(text)) {
        if (c > 0xffff) {
            c -= 0x10000;
            result.push_back(static_cast<char16_t>(0xd800 + (c >> 10)));
            result.push_back(static_cast<char16_t>(0xdc00 + (c & 0x3ff)));
        } else result.push_back(static_cast<char16_t>(c));
    }
    return result;
}
std::vector<std::string> keys(const Value& value) {
    std::vector<std::string> result;
    if (value.is_object()) for (const auto& entry : value.object_range()) result.emplace_back(entry.key());
    std::sort(result.begin(), result.end(), [](const auto& a, const auto& b) { return utf16(a) < utf16(b); });
    return result;
}
std::string quoted(const Value& value) {
    auto result = value.to_string();
    for (auto [from, to] : {std::pair{"<", "\\u003c"}, {">", "\\u003e"}, {"&", "\\u0026"},
                           {"\xe2\x80\xa8", "\\u2028"}, {"\xe2\x80\xa9", "\\u2029"}}) {
        std::size_t at = 0;
        while ((at = result.find(from, at)) != std::string::npos) {
            result.replace(at, std::char_traits<char>::length(from), to);
            at += std::char_traits<char>::length(to);
        }
    }
    return result;
}
bool valid_digest(const std::string& digest) {
    return digest.empty() || (digest.size() == 64 && std::all_of(digest.begin(), digest.end(), [](char c) {
        return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f');
    }));
}
void check_patterns(const Value& value) {
    if (value.is_array()) {
        for (const auto& item : value.array_range()) check_patterns(item);
    } else if (value.is_object()) {
        const auto& pattern = member(value, "pattern");
        if (is_text(pattern)) {
            try { detail::check_pattern(text(pattern)); }
            catch (const std::invalid_argument&) { refuse("pattern " + quoted(pattern), "outside Nightseam dialect"); }
        }
        for (const auto& key : keys(value)) check_patterns(value.at(key));
    }
}
std::string number_text(const Value& value) { return value.is_string() ? value.as<std::string>() : value.to_string(); }
double number(const Value& value) {
    if (value.is_double()) return value.as<double>();
    auto source = number_text(value);
    char* end = nullptr;
    auto result = std::strtod(source.c_str(), &end);
    if (end != source.data() + source.size()) return std::numeric_limits<double>::quiet_NaN();
    return result;
}
// Decimal precision is checked before conversion to binary64. Underflow
// cannot turn a fractional token into an admitted integer.
bool safe_integer(std::string source) {
    if (source.starts_with('-')) source.erase(0, 1);
    std::string exponent = "0";
    if (auto at = source.find_first_of("eE"); at != std::string::npos) {
        exponent = source.substr(at + 1);
        source.resize(at);
    }
    std::int64_t fraction = 0;
    if (auto at = source.find('.'); at != std::string::npos) {
        fraction = static_cast<std::int64_t>(source.size() - at - 1);
        source.erase(at, 1);
    }
    auto first = source.find_first_not_of('0');
    if (first == std::string::npos) return true;
    source.erase(0, first);
    if (exponent.starts_with('+')) exponent.erase(0, 1);
    std::int64_t power = 0;
    auto parsed = std::from_chars(exponent.data(), exponent.data() + exponent.size(), power);
    if (parsed.ec != std::errc{} || parsed.ptr != exponent.data() + exponent.size()) return false;
    auto bound = static_cast<std::int64_t>(source.size()) + fraction + 16;
    if (power > bound || power < -bound) return false;
    auto scale = power - fraction;
    if (scale < 0) {
        auto cut = static_cast<std::size_t>(-scale);
        if (cut > source.size() || source.find_first_not_of('0', source.size() - cut) != std::string::npos) return false;
        source.resize(source.size() - cut);
    } else {
        if (source.size() + scale > 16) return false;
        source.append(static_cast<std::size_t>(scale), '0');
    }
    return source.size() < 16 || (source.size() == 16 && source <= "9007199254740991");
}
bool timestamp(const std::string& value) {
    // Validate calendar fields without the host's timezone or normalization.
    if (value.size() < 20) return false;
    auto digits = [&](std::size_t at, std::size_t count) -> int {
        if (at + count > value.size()) return -1;
        int result = 0;
        for (std::size_t i = at; i < at + count; ++i) {
            if (value[i] < '0' || value[i] > '9') return -1;
            result = result * 10 + value[i] - '0';
        }
        return result;
    };
    if (value[4] != '-' || value[7] != '-' || value[10] != 'T' || value[13] != ':' || value[16] != ':') return false;
    auto year = digits(0, 4), month = digits(5, 2), day = digits(8, 2);
    auto hour = digits(11, 2), minute = digits(14, 2), second = digits(17, 2);
    int days[] = {31, year % 4 == 0 && (year % 100 != 0 || year % 400 == 0) ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31};
    if (year < 0 || month < 1 || month > 12 || day < 1 || day > days[month - 1] ||
        hour < 0 || hour > 23 || minute < 0 || minute > 59 || second < 0 || second > 59) return false;
    std::size_t at = 19;
    if (value[at] == '.') {
        ++at;
        auto begin = at;
        while (at < value.size() && value[at] >= '0' && value[at] <= '9') ++at;
        if (begin == at || at == value.size()) return false;
    }
    if (value[at] == 'Z') return at + 1 == value.size();
    if ((value[at] != '+' && value[at] != '-') || value.size() != at + 6 || value[at + 3] != ':') return false;
    auto zone_hour = digits(at + 1, 2), zone_minute = digits(at + 4, 2);
    return zone_hour >= 0 && zone_hour <= 23 && zone_minute >= 0 && zone_minute <= 59;
}
void validate_json(const Value& value, const std::string& location) {
    if (is_number(value)) {
        if (!std::isfinite(number(value))) expected(location, "finite JSON number");
    } else if (value.is_array()) {
        for (std::size_t i = 0; i < value.size(); ++i) validate_json(value.at(i), location + "[" + std::to_string(i) + "]");
    } else if (value.is_object()) {
        for (const auto& key : keys(value)) validate_json(value.at(key), location + "." + key);
    } else if (!value.is_null() && !is_text(value) && !value.is_bool()) expected(location, "JSON value");
}
void constrain(const Value& field, const Value& value, const std::string& location) {
    for (auto facet : {"min", "max"}) {
        if (!field.contains(facet)) continue;
        const auto& bound = field.at(facet);
        bool minimum = std::string_view(facet) == "min";
        if (is_number(value) && is_number(bound)) {
            if (minimum ? number(value) < number(bound) : number(value) > number(bound))
                expected(location, std::string(minimum ? "at least " : "at most ") + number_text(bound));
        } else if (is_text(value) && is_text(bound)) {
            if (minimum ? text(value) < text(bound) : text(value) > text(bound))
                expected(location, std::string(minimum ? "at or after " : "at or before ") + text(bound));
        }
    }
    const auto& length = member(field, "length");
    if (length.is_object() && (is_text(value) || value.is_array())) {
        auto size = is_text(value) ? decode_utf8(text(value)).size() : value.size();
        for (auto facet : {"min", "max"}) {
            if (!length.contains(facet)) continue;
            auto bound = length.at(facet).as<std::size_t>();
            bool minimum = std::string_view(facet) == "min";
            if (minimum ? size < bound : size > bound)
                expected(location, std::string(minimum ? "a length of at least " : "a length of at most ") + std::to_string(bound));
        }
    }
    auto pattern = text(member(field, "pattern"));
    if (!pattern.empty() && is_text(value) && !detail::match_pattern(pattern, text(value))) expected(location, "a match of " + pattern);
}

struct Context;
struct Expression;
struct Argument { std::shared_ptr<Expression> type; std::shared_ptr<Context> family; };
using Scope = std::map<std::string, Argument>;
using Aliases = std::set<const Value*>;
struct Context {
    Value descriptor;
    std::string digest;
    std::map<std::string, std::shared_ptr<Context>> imported;
    Scope scope;
    std::map<std::string, std::shared_ptr<Expression>> drawn;
};
struct Expression {
    std::shared_ptr<Context> schema;
    std::shared_ptr<const Value> value;
    Scope scope;
    Aliases aliases;
    Expression child(const Value& inner) const { return {schema, std::make_shared<Value>(inner), scope, {}}; }
};
Expression expression(std::shared_ptr<Context> schema, const Value& value) {
    return {schema, std::make_shared<Value>(value), schema->scope, {}};
}
struct Resolved { Expression expression; const Value* definition = nullptr; std::string name; };
struct Field { Value field; Expression expression; };
const Value* definition(const std::shared_ptr<Context>& schema, const std::string& name) {
    const auto& types = member(schema->descriptor, "types");
    return types.is_object() && types.contains(name) ? &types.at(name) : nullptr;
}
const Value* single_family_parameter(const std::shared_ptr<Context>& schema) {
    const Value* found = nullptr;
    for (const auto& parameter : array(member(schema->descriptor, "parameters")).array_range()) {
        if (!text(member(parameter, "of")).empty()) {
            if (found) return nullptr;
            found = &parameter;
        }
    }
    return found;
}
std::vector<Value> free_parameters(const std::shared_ptr<Context>& schema, const Value* type, Aliases seen = {}) {
    if (!type || seen.contains(type)) return {};
    seen.insert(type);
    std::set<std::string> used;
    std::function<void(const Value&)> walk;
    auto inherit = [&](const Value* target) {
        for (const auto& parameter : free_parameters(schema, target, seen)) used.insert(text(member(parameter, "name")));
    };
    auto walk_base = [&](const Value& base) {
        if (member(base, "apply").is_string() && member(base, "with").is_object()) {
            for (const auto& filler : base.at("with").object_range()) walk(filler.value());
        } else walk(base);
    };
    walk = [&](const Value& value) {
        if (is_text(value)) {
            auto name = text(value);
            auto dot = name.find('.');
            auto prefix = name.substr(0, dot);
            for (const auto& parameter : array(member(schema->descriptor, "parameters")).array_range()) {
                if (text(member(parameter, "name")) == prefix) { used.insert(prefix); return; }
            }
            if (dot == std::string::npos) { inherit(definition(schema, name)); return; }
            auto imported = schema->imported.find(prefix);
            if (imported == schema->imported.end()) return;
            auto target = definition(imported->second, name.substr(dot + 1));
            if (!target) return;
            auto needed = free_parameters(imported->second, target, seen);
            for (const auto& parameter : array(member(*target, "parameters")).array_range()) needed.push_back(parameter);
            auto source = single_family_parameter(schema);
            if (source && std::any_of(needed.begin(), needed.end(), [](const auto& parameter) { return !text(member(parameter, "of")).empty(); }))
                used.insert(text(member(*source, "name")));
        } else if (value.is_object()) {
            for (auto key : {"array", "map", "nullable"}) if (value.contains(key)) { walk(value.at(key)); return; }
            if (is_text(member(value, "apply"))) {
                auto target = text(value.at("apply"));
                if (target.find('.') == std::string::npos) inherit(definition(schema, target));
                const auto& fillers = member(value, "with");
                if (fillers.is_object()) for (const auto& filler : fillers.object_range()) walk(filler.value());
                return;
            }
            if (is_text(member(value, "ref"))) {
                auto entity = definition(schema, text(value.at("ref")));
                if (entity) for (const auto& field : array(member(*entity, "fields")).array_range())
                    if (member(field, "name") == member(*entity, "key")) walk(member(field, "type"));
                return;
            }
            if (value.contains("kind")) {
                for (const auto& field : array(member(value, "fields")).array_range()) walk(member(field, "type"));
                for (const auto& base : array(member(value, "extends")).array_range()) walk_base(base);
                const auto& variants = member(value, "variants");
                if (variants.is_object()) for (const auto& variant : variants.object_range()) walk(variant.value());
            }
        }
    };
    walk(member(*type, "type"));
    for (const auto& field : array(member(*type, "fields")).array_range()) walk(member(field, "type"));
    for (const auto& base : array(member(*type, "extends")).array_range()) walk_base(base);
    const auto& variants = member(*type, "variants");
    if (variants.is_object()) for (const auto& variant : variants.object_range()) walk(variant.value());
    std::vector<Value> result;
    for (const auto& parameter : array(member(schema->descriptor, "parameters")).array_range())
        if (used.contains(text(member(parameter, "name")))) result.push_back(parameter);
    return result;
}
Resolved named(Expression source, std::string name, const std::string& location) {
    if (auto dot = name.find('.'); dot != std::string::npos) {
        auto family = name.substr(0, dot);
        if (family.empty()) expected(location, "known family");
        auto caller = source;
        std::shared_ptr<Context> owner;
        bool parameter = family.front() >= 'A' && family.front() <= 'Z';
        if (parameter) {
            auto found = source.scope.find(family);
            if (found != source.scope.end()) owner = found->second.family;
            if (!owner) expected(location, "a binding of the parameter " + family);
        } else {
            auto found = source.schema->imported.find(family);
            if (found != source.schema->imported.end()) owner = found->second;
            if (!owner) expected(location, "known family");
        }
        name = name.substr(dot + 1);
        source = expression(owner, name);
        if (!parameter) {
            auto parameter = single_family_parameter(caller.schema);
            auto type = definition(owner, name);
            if (parameter && type) {
                auto binding = caller.scope.find(text(member(*parameter, "name")));
                if (binding != caller.scope.end()) {
                    auto needed = free_parameters(owner, type);
                    for (const auto& item : array(member(*type, "parameters")).array_range()) needed.push_back(item);
                    for (const auto& item : needed) if (!text(member(item, "of")).empty())
                        source.scope[text(member(item, "name"))] = binding->second;
                }
            }
        }
    }
    auto type = definition(source.schema, name);
    if (!type) expected(location, "known type");
    return {source.child(name), type, name};
}
Resolved resolve(Expression source, const std::string& location, bool inheritance = false) {
    auto aliases = source.aliases;
    for (;;) {
        const auto& value = *source.value;
        Resolved resolved;
        if (is_text(value)) {
            auto name = text(value);
            if (auto argument = source.scope.find(name); argument != source.scope.end()) {
                if (!argument->second.type) expected(location, "type argument");
                auto captured = argument->second.type;
                source = *captured;
                aliases = source.aliases;
                continue;
            }
            if (name == "json" || name == "string" || name == "boolean" || name == "number" || name == "integer" || name == "timestamp")
                return {source, nullptr, {}};
            resolved = named(source, name, location);
        } else if (value.is_object()) {
            if (is_text(member(value, "apply"))) {
                auto reference = text(value.at("apply"));
                resolved = named(source, reference, location);
                const auto& fillers = member(value, "with");
                if (!fillers.is_object()) expected(location, "application arguments");
                auto scope = resolved.expression.scope;
                std::vector<Value> parameters;
                if (inheritance || reference.find('.') != std::string::npos) parameters = free_parameters(resolved.expression.schema, resolved.definition);
                for (const auto& parameter : array(member(*resolved.definition, "parameters")).array_range()) parameters.push_back(parameter);
                std::set<std::string> allowed;
                for (const auto& parameter : parameters) {
                    auto name = text(member(parameter, "name"));
                    allowed.insert(name);
                    if (!fillers.contains(name)) expected(location, "an argument for " + name);
                    const auto& filler = fillers.at(name);
                    if (text(member(parameter, "of")).empty()) {
                        auto captured = source.child(filler);
                        captured.aliases = aliases;
                        scope[name] = {std::make_shared<Expression>(std::move(captured)), nullptr};
                    } else {
                        if (!is_text(filler)) expected(location, "family argument for " + name);
                        auto family = text(filler);
                        std::shared_ptr<Context> owner;
                        if (auto found = source.scope.find(family); found != source.scope.end()) owner = found->second.family;
                        else if (auto found = source.schema->imported.find(family); found != source.schema->imported.end()) owner = found->second;
                        if (!owner) expected(location, "known family argument for " + name);
                        scope[name] = {nullptr, owner};
                    }
                }
                for (const auto& parameter : keys(fillers)) if (!allowed.contains(parameter)) expected(location, "known parameter " + parameter);
                resolved.expression.scope = std::move(scope);
            } else if (value.contains("kind")) resolved = {source, source.value.get(), text(value.at("kind"))};
            else return {source, nullptr, {}};
        } else expected(location, "type expression");
        inheritance = false;
        if (auto drawn = resolved.expression.schema->drawn.find(resolved.name); drawn != resolved.expression.schema->drawn.end()) {
            auto captured = drawn->second;
            source = *captured;
            aliases = source.aliases;
            continue;
        }
        if (text(member(*resolved.definition, "kind")) == "alias") {
            // Inline expression trees are finite and have no declaration
            // identity. Only named aliases can form an expansion cycle.
            if (resolved.definition != resolved.expression.value.get()) {
                if (aliases.contains(resolved.definition)) expected(location, "acyclic type expression");
                aliases.insert(resolved.definition);
            }
            source = resolved.expression.child(member(*resolved.definition, "type"));
            continue;
        }
        return resolved;
    }
}
Resolved inherited(const Expression& source, const Value& base, const std::string& location) {
    if (is_text(base)) {
        auto owner = named(source, text(base), location);
        if (!array(member(*owner.definition, "parameters")).empty() || !free_parameters(owner.expression.schema, owner.definition).empty())
            expected(location, "explicit application of generic base " + text(base));
    }
    return resolve(source.child(base), location, true);
}
std::vector<Field> fields(const Resolved& source, const std::string& location, Aliases seen = {}) {
    if (!source.definition) expected(location, "record");
    if (seen.contains(source.definition)) expected(location, "acyclic inheritance");
    seen.insert(source.definition);
    std::vector<Field> result;
    for (const auto& base : array(member(*source.definition, "extends")).array_range()) {
        auto parent = fields(inherited(source.expression, base, location), location, seen);
        result.insert(result.end(), parent.begin(), parent.end());
    }
    for (const auto& field : array(member(*source.definition, "fields")).array_range()) result.push_back({field, source.expression.child(member(field, "type"))});
    return result;
}
std::map<std::string, Expression> variants(const Resolved& source, const std::string& location, Aliases seen = {}) {
    if (!source.definition || text(member(*source.definition, "kind")) != "union") expected(location, "union");
    if (seen.contains(source.definition)) expected(location, "acyclic inheritance");
    seen.insert(source.definition);
    std::map<std::string, Expression> result;
    for (const auto& base : array(member(*source.definition, "extends")).array_range()) {
        auto parent = variants(inherited(source.expression, base, location), location, seen);
        for (auto& [tag, value] : parent) result.insert_or_assign(tag, std::move(value));
    }
    const auto& own = member(*source.definition, "variants");
    for (const auto& key : keys(own)) result.insert_or_assign(key, source.expression.child(own.at(key)));
    return result;
}
void validate_expression(const Expression& source, const Value& value, const std::string& location) {
    auto resolved = resolve(source, location);
    const auto& current = resolved.expression;
    if (resolved.definition) {
        const auto& type = *resolved.definition;
        auto kind = text(member(type, "kind"));
        if (kind == "callable") {
            auto contract = text(member(type, "contract"));
            if (!value.is_object()) expected(location, "a live reference to " + contract);
            if (text(member(value, "binding")).empty()) refuse(location + ".binding", "a live reference names the binding it refers to");
            if (!is_text(member(value, "contract"))) refuse(location + ".contract", "a live reference carries the declaration it implements");
            if (text(value.at("contract")) != contract) refuse(location + ".contract", "the reference carries " + text(value.at("contract")) + " where " + contract + " is expected");
            auto digest = text(member(value, "digest"));
            if (value.contains("digest") && (digest.empty() || !valid_digest(digest))) expected(location + ".digest", "lowercase SHA-256 digest");
            if (!digest.empty() && !current.schema->digest.empty() && digest != current.schema->digest)
                throw PublicError("contract_mismatch", location + ".digest: the reference to " + contract + " carries declaration digest " + digest + " where " + current.schema->digest + " is expected");
            for (const auto& key : keys(value)) if (key != "binding" && key != "contract" && key != "digest") refuse(location + "." + key, "unknown field");
            return;
        }
        if (kind == "enum") {
            if (is_text(value)) for (const auto& option : array(member(type, "values")).array_range()) if (option == value) return;
            expected(location, resolved.name);
        }
        if (kind == "record" || kind == "entity") {
            if (!value.is_object()) expected(location, resolved.name + " object");
            std::set<std::string> allowed;
            for (const auto& scoped : fields(resolved, location)) {
                const auto& field = scoped.field;
                auto name = text(member(field, "name"));
                allowed.insert(name);
                auto at = location + "." + name;
                if (!value.contains(name)) {
                    if (flag(member(field, "required"))) refuse(at, "required field missing");
                    continue;
                }
                const auto& child = value.at(name);
                if (child.is_null()) {
                    if (flag(member(field, "nullable"))) continue;
                    auto underlying = resolve(scoped.expression, at);
                    if (!underlying.expression.value->is_object() || !underlying.expression.value->contains("nullable")) refuse(at, "null is not permitted");
                }
                validate_expression(scoped.expression, child, at);
                if (!child.is_null()) constrain(field, child, at);
            }
            for (const auto& key : keys(value)) {
                if (allowed.contains(key)) continue;
                if (!flag(member(type, "open"))) refuse(location + "." + key, "unknown field");
                validate_json(value.at(key), location + "." + key);
            }
            return;
        }
        if (kind == "union") {
            if (!value.is_object()) expected(location, resolved.name + " object");
            auto tag = text(member(type, "tag"));
            if (!value.contains(tag)) refuse(location + "." + tag, "required field missing");
            auto all = variants(resolved, location);
            auto found = all.find(text(value.at(tag)));
            if (!is_text(value.at(tag)) || found == all.end()) expected(location + "." + tag, "known variant");
            const auto& variant = found->second;
            auto name = text(member(type, "value"));
            if (name.empty()) name = "value";
            const auto& marker = *variant.value;
            bool empty = marker.is_object() && marker.size() == 1 && flag(member(marker, "empty"));
            if (!empty) {
                if (!value.contains(name)) refuse(location + "." + name, "required field missing");
                validate_expression(variant, value.at(name), location + "." + name);
            }
            for (const auto& key : keys(value)) if (key != tag && (empty || key != name)) refuse(location + "." + key, "unknown field");
            return;
        }
        expected(location, "supported type");
    }
    const auto& type = *current.value;
    if (type.is_object()) {
        if (type.contains("nullable")) {
            if (!value.is_null()) validate_expression(current.child(type.at("nullable")), value, location);
            return;
        }
        if (type.contains("literal")) {
            const auto& literal = type.at("literal");
            if (text(literal).empty()) expected(location, "nonempty string literal");
            if (!is_text(value) || value != literal) expected(location, "literal " + quoted(literal));
            return;
        }
        if (type.contains("array")) {
            if (!value.is_array()) expected(location, "array");
            for (std::size_t i = 0; i < value.size(); ++i)
                validate_expression(current.child(type.at("array")), value.at(i), location + "[" + std::to_string(i) + "]");
            return;
        }
        if (type.contains("map")) {
            if (!value.is_object()) expected(location, "object");
            for (const auto& key : keys(value)) validate_expression(current.child(type.at("map")), value.at(key), location + "." + key);
            return;
        }
        if (is_text(member(type, "ref"))) {
            Resolved entity;
            try { entity = named(current, text(type.at("ref")), location); }
            catch (const std::invalid_argument&) { expected(location, "known entity"); }
            for (const auto& field : fields(resolve(entity.expression, location), location)) {
                if (member(field.field, "name") == member(*entity.definition, "key")) {
                    validate_expression(field.expression, value, location);
                    return;
                }
            }
            expected(location, "entity with a key");
        }
        if (type.contains("empty")) {
            if (!value.is_object() || !value.empty()) expected(location, "empty object");
            return;
        }
        expected(location, "supported type expression");
    }
    auto name = text(type);
    if (name == "json") validate_json(value, location);
    else if (name == "string") { if (!is_text(value)) expected(location, "string"); }
    else if (name == "boolean") { if (!value.is_bool()) expected(location, "boolean"); }
    else if (name == "number" || name == "integer") {
        if (!is_number(value)) expected(location, name);
        if (!std::isfinite(number(value))) expected(location, "finite number");
        if (name == "integer" && !safe_integer(number_text(value))) expected(location, "JavaScript-safe integer");
    } else if (name == "timestamp") {
        if (!is_text(value)) expected(location, "timestamp");
        if (!timestamp(text(value))) expected(location, "RFC3339 timestamp");
    } else expected(location, "known type");
}
}

struct Schema::Data { std::shared_ptr<Context> context; };

Schema::Schema(Value descriptor, std::string digest, Imports imported) : data_(std::make_shared<Data>()) {
    if (!valid_digest(digest)) expected("schema.digest", "empty or lowercase SHA-256 digest");
    validate_unicode(descriptor);
    if (!member(descriptor, "types").is_object()) throw std::invalid_argument("expected family descriptor with types");
    check_patterns(descriptor.at("types"));
    data_->context = std::make_shared<Context>();
    data_->context->descriptor = std::move(descriptor);
    data_->context->digest = std::move(digest);
    for (const auto& [name, schema] : imported) {
        decode_utf8(name);
        if (!schema) throw std::invalid_argument("expected imported schema for " + name);
        data_->context->imported[name] = schema->data_->context;
    }
}
void Schema::validate(const Value& type, const Value& value, const Value& bindings, std::string location) const {
    validate_unicode(type);
    validate_unicode(value);
    validate_unicode(bindings);
    check_patterns(type);
    if (!bindings.is_object()) expected(location, "parameter bindings");
    auto source = expression(data_->context, type);
    for (const auto& name : keys(bindings)) {
        const auto& binding = bindings.at(name);
        if (binding.is_object() && binding.contains("type")) {
            check_patterns(binding.at("type"));
            source.scope[name] = {std::make_shared<Expression>(expression(data_->context, binding.at("type"))), nullptr};
        } else if (is_text(member(binding, "family"))) {
            auto family = data_->context->imported.find(text(binding.at("family")));
            if (family == data_->context->imported.end()) expected(location, "known family argument for " + name);
            source.scope[name] = {nullptr, family->second};
        } else expected(location, "parameter binding for " + name);
    }
    for (const auto& [name, argument] : source.scope) if (argument.type) {
        validate_unicode(*argument.type->value);
        check_patterns(*argument.type->value);
    }
    validate_expression(source, value, location);
}
Schema Schema::bind(const std::map<std::string, Value>& types, const Imports& families) const {
    auto bound = *this;
    bound.data_ = std::make_shared<Data>();
    bound.data_->context = std::make_shared<Context>(*data_->context);
    auto& scope = bound.data_->context->scope;
    std::map<std::string, std::shared_ptr<Context>> drawn;
    for (const auto& [name, value] : types) {
        decode_utf8(name);
        validate_unicode(value);
        check_patterns(value);
        scope[name] = {std::make_shared<Expression>(expression(data_->context, value)), nullptr};
        if (auto dot = name.find('.'); dot != std::string::npos && dot != 0 && dot + 1 < name.size()) {
            auto family = name.substr(0, dot);
            if (!drawn.contains(family)) {
                auto context = std::make_shared<Context>();
                context->descriptor = parse_value(R"({"types":{}})");
                drawn[family] = std::move(context);
            }
        }
    }
    for (const auto& [name, argument] : scope) {
        auto dot = name.find('.');
        auto family = name.substr(0, dot);
        if (dot != std::string::npos && drawn.contains(family) && argument.type) {
            auto member = name.substr(dot + 1);
            drawn[family]->descriptor["types"][member] = parse_value(R"({"kind":"alias"})");
            drawn[family]->drawn[member] = argument.type;
        }
    }
    for (const auto& [name, family] : drawn) scope[name] = {nullptr, family};
    for (const auto& [name, schema] : families) {
        decode_utf8(name);
        if (!schema) throw std::invalid_argument("expected family argument for " + name);
        scope[name] = {nullptr, schema->data_->context};
    }
    return bound;
}
} // namespace nightseam::runtime
