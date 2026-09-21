#include <nightseam/runtime/value.hpp>
#include <jsoncons/json_cursor.hpp>

#include <cmath>
#include <stdexcept>
#include <optional>
#include <vector>

namespace nightseam::runtime {

std::u32string decode_utf8(std::string_view text) {
    std::u32string out;
    for (std::size_t at = 0; at < text.size();) {
        auto first = static_cast<unsigned char>(text[at++]);
        char32_t value;
        unsigned extra;
        char32_t minimum;
        if (first <= 0x7f) { out.push_back(first); continue; }
        if (first >= 0xc2 && first <= 0xdf) { value = first & 0x1f; extra = 1; minimum = 0x80; }
        else if (first >= 0xe0 && first <= 0xef) { value = first & 0x0f; extra = 2; minimum = 0x800; }
        else if (first >= 0xf0 && first <= 0xf4) { value = first & 0x07; extra = 3; minimum = 0x10000; }
        else throw std::invalid_argument("expected Unicode scalar strings");
        if (text.size()-at < extra) throw std::invalid_argument("expected Unicode scalar strings");
        while (extra-- != 0) {
            auto next = static_cast<unsigned char>(text[at++]);
            if (next < 0x80 || next > 0xbf) throw std::invalid_argument("expected Unicode scalar strings");
            value = (value << 6) | (next & 0x3f);
        }
        if (value < minimum || value > 0x10ffff || (value >= 0xd800 && value <= 0xdfff)) {
            throw std::invalid_argument("expected Unicode scalar strings");
        }
        out.push_back(value);
    }
    return out;
}

bool is_number(const Value& value) {
    return value.is_number() || (value.is_string() &&
        (value.tag() == jsoncons::semantic_tag::bigint || value.tag() == jsoncons::semantic_tag::bigdec));
}

bool is_text(const Value& value) { return value.is_string() && !is_number(value); }

void validate_unicode(const Value& value) {
    if (value.is_string()) { decode_utf8(value.as_string_view()); }
    else if (value.is_array()) {
        for (const auto& child : value.array_range()) validate_unicode(child);
    } else if (value.is_object()) {
        for (const auto& member : value.object_range()) {
            decode_utf8(member.key());
            validate_unicode(member.value());
        }
    }
}

namespace {
// Check escaped code units before a decoder can replace or combine them.
// This pass sees every string token, including duplicate member values.
void scalar_escapes(std::string_view source) {
    auto unit = [&](std::size_t at) {
        if (source.size()-at < 4) throw std::invalid_argument("incomplete Unicode escape");
        unsigned value = 0;
        for (unsigned n = 0; n != 4; ++n) {
            auto c = source[at+n];
            unsigned digit;
            if (c >= '0' && c <= '9') digit = c-'0';
            else if (c >= 'a' && c <= 'f') digit = c-'a'+10;
            else if (c >= 'A' && c <= 'F') digit = c-'A'+10;
            else throw std::invalid_argument("invalid Unicode escape");
            value = (value << 4) | digit;
        }
        return value;
    };
    bool in_string = false;
    for (std::size_t at = 0; at < source.size();) {
        auto c = source[at++];
        if (c == '"') { in_string = !in_string; continue; }
        if (!in_string || c != '\\') continue;
        if (at == source.size()) throw std::invalid_argument("incomplete JSON escape");
        if (source[at++] != 'u') continue;
        auto high = unit(at);
        at += 4;
        if (high >= 0xdc00 && high <= 0xdfff) throw std::invalid_argument("expected Unicode scalar strings");
        if (high < 0xd800 || high > 0xdbff) continue;
        if (source.size()-at < 6 || source[at] != '\\' || source[at+1] != 'u') {
            throw std::invalid_argument("expected Unicode scalar strings");
        }
        auto low = unit(at+2);
        if (low < 0xdc00 || low > 0xdfff) throw std::invalid_argument("expected Unicode scalar strings");
        at += 6;
    }
}
}

Value parse_value(std::string_view source) {
    decode_utf8(source);
    scalar_escapes(source);
    if (source.starts_with("\xef\xbb\xbf")) throw std::invalid_argument("JSON must not start with a byte order mark");
    auto options = jsoncons::json_options{}.allow_comments(false).allow_trailing_comma(false).lossless_number(true);
    jsoncons::json_string_cursor cursor(source, options);
    struct Level { Value value; std::string key; };
    std::vector<Level> stack;
    std::optional<Value> root;
    auto add = [&](Value value) {
        if (stack.empty()) { root = std::move(value); }
        else if (stack.back().value.is_array()) stack.back().value.push_back(std::move(value));
        else stack.back().value.insert_or_assign(stack.back().key, std::move(value));
    };
    // Build from events so duplicate payload members follow both reference
    // runtimes' last-wins rule, instead of the library DOM's first-wins rule.
    for (; !cursor.done(); cursor.next()) {
        const auto& event = cursor.current();
        using E = jsoncons::staj_events;
        switch (event.event_type()) {
        case E::begin_object: stack.push_back({Value(jsoncons::json_object_arg), {}}); break;
        case E::begin_array: stack.push_back({Value(jsoncons::json_array_arg), {}}); break;
        case E::end_object:
        case E::end_array: {
            auto value = std::move(stack.back().value);
            stack.pop_back();
            add(std::move(value));
            break;
        }
        case E::key: stack.back().key = event.get<std::string>(); break;
        case E::string_value: add(Value(event.get<std::string>(), event.tag())); break;
        case E::int64_value: add(Value(event.get<std::int64_t>())); break;
        case E::uint64_value: add(Value(event.get<std::uint64_t>())); break;
        case E::double_value: add(Value(event.get<double>())); break;
        case E::bool_value: add(Value(event.get<bool>())); break;
        case E::null_value: add(Value::null()); break;
        default: throw std::invalid_argument("expected a JSON value");
        }
    }
    cursor.check_done();
    if (!root || !stack.empty()) throw std::invalid_argument("incomplete JSON value");
    validate_unicode(*root);
    return std::move(*root);
}

namespace {
void encodable(const Value& value) {
    if (value.is_double() && !std::isfinite(value.as<double>())) throw std::invalid_argument("non-finite JSON number");
    if (value.is_byte_string()) throw std::invalid_argument("expected a JSON value");
    if (value.is_array()) {
        for (const auto& child : value.array_range()) encodable(child);
    } else if (value.is_object()) {
        for (const auto& member : value.object_range()) encodable(member.value());
    }
}
}

std::string stringify(const Value& value) {
    validate_unicode(value);
    encodable(value);
    auto source = value.to_string();
    // A caller can construct a tagged big number manually. Refuse invalid
    // numeric text instead of allowing that tag to bypass JSON syntax.
    parse_value(source);
    return source;
}

} // namespace nightseam::runtime
