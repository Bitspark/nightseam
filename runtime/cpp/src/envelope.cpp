#include <nightseam/runtime/envelope.hpp>
#include <jsoncons/json_cursor.hpp>
#include <charconv>
#include <set>

namespace nightseam::runtime {
namespace {
[[noreturn]] void invalid() { throw std::invalid_argument("invalid duplex frame shape"); }
// Syntax is checked before this scanner runs. It retains each outer member's
// exact value bytes, including duplicate payload keys and numeric spellings.
std::map<std::string, std::string> raw_members(std::string_view source) {
    std::map<std::string, std::string> result;
    std::size_t at = source.find('{') + 1;
    auto space = [&] { while (at < source.size() && (source[at] == ' ' || source[at] == '\n' || source[at] == '\r' || source[at] == '\t')) ++at; };
    space();
    while (source[at] != '}') {
        auto key_start = at++;
        while (source[at] != '"') { if (source[at] == '\\') ++at; ++at; }
        ++at;
        auto key = parse_value(source.substr(key_start, at-key_start)).as<std::string>();
        space(); ++at; space();
        auto start = at;
        bool string = false; int depth = 0;
        for (; at < source.size(); ++at) {
            auto c = source[at];
            if (string) { if (c == '\\') ++at; else if (c == '"') string = false; continue; }
            if (c == '"') string = true;
            else if (c == '[' || c == '{') ++depth;
            else if ((c == '}' || c == ',') && depth == 0) break;
            else if (c == ']' || c == '}') --depth;
        }
        auto end = at;
        while (end > start && (source[end-1] == ' ' || source[end-1] == '\n' || source[end-1] == '\r' || source[end-1] == '\t')) --end;
        result.emplace(std::move(key), source.substr(start,end-start));
        if (source[at] == '}') break;
        ++at; space();
    }
    return result;
}
std::string text_member(const Value& object, const char* name, bool nonempty = true) {
    if (!object.contains(name) || !is_text(object.at(name))) invalid();
    auto text = object.at(name).as<std::string>();
    if (nonempty && text.empty()) invalid();
    return text;
}
}

bool valid_id(std::string_view id, std::string_view prefix) {
    if (!id.starts_with(prefix)) return false;
    id.remove_prefix(prefix.size());
    if (id.empty() || id.size() > 20 || id.front() == '0') return false;
    for (auto c : id) if (c < '0' || c > '9') return false;
    return true;
}
bool valid_traceparent(std::string_view parent) {
    if (parent.size() != 55) return false;
    for (std::size_t i = 0; i < parent.size(); ++i) {
        if (i == 2 || i == 35 || i == 52) { if (parent[i] != '-') return false; }
        else if (!((parent[i] >= '0' && parent[i] <= '9') || (parent[i] >= 'a' && parent[i] <= 'f'))) return false;
    }
    return true;
}

Envelope decode_envelope(std::string_view source, Role recipient) {
    auto value = parse_value(source);
    if (!value.is_object()) invalid();
    auto raw = raw_members(source);
    // Payload duplicates follow JSON's last-wins behavior; envelope members
    // cannot disappear into the DOM. Inspect their original token stream.
    jsoncons::json_string_cursor cursor(source, jsoncons::json_options{}.lossless_number(true));
    int depth = 0;
    std::set<std::string> names;
    using E = jsoncons::staj_events;
    for (; !cursor.done(); cursor.next()) {
        auto type = cursor.current().event_type();
        if (type == E::begin_object || type == E::begin_array) ++depth;
        else if (type == E::end_object || type == E::end_array) --depth;
        else if (type == E::key && depth == 1 && !names.insert(cursor.current().get<std::string>()).second) invalid();
    }
    if (!value.contains("version") || stringify(value.at("version")) != "1") invalid();
    Envelope f;
    f.kind = text_member(value, "kind");
    std::set<std::string> allowed{"version", "kind", "traceparent", "tracestate"};
    if (f.kind == "request") {
        allowed.insert({"id", "method", "params", "meta"});
        f.id = text_member(value, "id"); f.method = text_member(value, "method");
        if (!value.contains("params")) invalid();
        f.params = value.at("params");
        f.raw_params = raw.at("params");
    } else if (f.kind == "response") {
        allowed.insert({"id", "result", "error"});
        f.id = text_member(value, "id");
        if (value.contains("result") == value.contains("error")) invalid();
        if (value.contains("result")) { f.result = value.at("result"); f.raw_result = raw.at("result"); }
        else {
            const auto& error = value.at("error");
            if (!error.is_object()) invalid();
            for (const auto& member : error.object_range()) {
                if (member.key() != "code" && member.key() != "message" && member.key() != "data") invalid();
            }
            f.error.emplace(text_member(error, "code"), text_member(error, "message"));
            if (error.contains("data")) f.error->data = error.at("data");
        }
    } else if (f.kind == "event") {
        allowed.insert({"event", "data", "meta"});
        f.event = text_member(value, "event");
        if (!value.contains("data")) invalid();
        f.data = value.at("data");
        f.raw_data = raw.at("data");
    } else if (f.kind == "cancel") {
        allowed.insert("id"); f.id = text_member(value, "id");
    } else invalid();
    for (const auto& name : names) if (!allowed.contains(name)) invalid();
    if (!f.id.empty()) {
        auto own = recipient == Role::client ? "c:" : "s:";
        auto remote = recipient == Role::client ? "s:" : "c:";
        if (!valid_id(f.id, f.kind == "response" ? own : remote)) invalid();
    }
    if (value.contains("traceparent")) {
        f.trace.parent = text_member(value, "traceparent");
        if (!valid_traceparent(f.trace.parent)) invalid();
    }
    if (value.contains("tracestate")) f.trace.state = text_member(value, "tracestate", false);
    if (value.contains("meta")) {
        const auto& meta = value.at("meta");
        if (!meta.is_object()) invalid();
        f.meta.emplace();
        for (const auto& member : meta.object_range()) {
            if (member.key().starts_with("nightseam.") || !is_text(member.value())) invalid();
            f.meta->emplace(member.key(), member.value().as<std::string>());
        }
    }
    return f;
}

std::string encode_envelope(const Envelope& f) {
    std::string out = "{\"version\":1";
    auto raw = [&](std::string_view key, const std::string& text) {
        parse_value(text);
        out += "," + stringify(Value(key)) + ":" + text;
    };
    auto add = [&](std::string_view key, const Value& value) { raw(key, stringify(value)); };
    add("kind", f.kind);
    if (!f.id.empty()) add("id", f.id);
    if (!f.method.empty()) add("method", f.method);
    if (f.raw_params) raw("params", *f.raw_params); else if (f.params) add("params", *f.params);
    if (f.raw_result) raw("result", *f.raw_result); else if (f.result) add("result", *f.result);
    if (f.error) {
        Value error(jsoncons::json_object_arg);
        error["code"] = f.error->code; error["message"] = f.error->message;
        if (f.error->data) error["data"] = *f.error->data;
        if (f.error->raw_data) {
            parse_value(*f.error->raw_data);
            raw("error", "{\"code\":" + stringify(Value(f.error->code)) + ",\"message\":" + stringify(Value(f.error->message)) + ",\"data\":" + *f.error->raw_data + "}");
        } else add("error", error);
    }
    if (!f.event.empty()) add("event", f.event);
    if (f.raw_data) raw("data", *f.raw_data); else if (f.data) add("data", *f.data);
    if (!f.trace.parent.empty()) add("traceparent", f.trace.parent);
    if (!f.trace.state.empty()) add("tracestate", f.trace.state);
    if (f.meta) {
        Value meta(jsoncons::json_object_arg);
        for (const auto& [key, value] : *f.meta) if (!key.starts_with("nightseam.")) meta[key] = value;
        add("meta", meta);
    }
    out += "}";
    return out;
}

std::string encode_path(const Path& path) {
    std::string name;
    for (const auto& segment : path) { decode_utf8(segment); name += std::to_string(segment.size()) + ":" + segment; }
    return name;
}
Path decode_path(std::string_view name) {
    Path path;
    while (!name.empty()) {
        auto colon = name.find(':');
        if (colon == std::string_view::npos || colon == 0 || (colon > 1 && name[0] == '0')) throw std::invalid_argument("invalid Wire path");
        std::size_t size = 0;
        auto [last, error] = std::from_chars(name.data(), name.data() + colon, size);
        if (error != std::errc{} || last != name.data() + colon) throw std::invalid_argument("invalid Wire path");
        name.remove_prefix(colon + 1);
        if (size > name.size()) throw std::invalid_argument("invalid Wire path");
        auto segment = name.substr(0, size); decode_utf8(segment);
        path.emplace_back(segment); name.remove_prefix(size);
    }
    return path;
}
} // namespace nightseam::runtime
