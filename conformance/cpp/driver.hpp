#pragma once

#include <nightseam/runtime/peer.hpp>
#include <algorithm>
#include <condition_variable>
#include <deque>
#include <exception>
#include <mutex>
#include <thread>

namespace nightseam::conformance {
using runtime::Value;
using duplex::Wait;

inline Value object(std::initializer_list<std::pair<std::string, Value>> fields = {}) {
    Value out(jsoncons::json_object_arg);
    for (const auto& [key, value] : fields) out.insert_or_assign(key, value);
    return out;
}

struct Failure : std::runtime_error {
    Value detail;
    Failure(std::string code, std::string message)
        : std::runtime_error(message), detail(object({{"code", code}, {"message", message}})) {}
};

// These spans are driver framing, not profile decoding. The complete line
// is validated first; payloads subsequently cross the runtime boundary with
// their original whitespace, numeric tokens and duplicate object members.
class Arguments {
    Value values_;
    std::map<std::string, std::string> raw_;
    static void space(std::string_view s, std::size_t& at) {
        while (at < s.size() && (s[at] == ' ' || s[at] == '\t' || s[at] == '\r' || s[at] == '\n')) ++at;
    }
    static void token(std::string_view s, std::size_t& at) {
        if (s.at(at) == '"') {
            ++at;
            while (at < s.size()) {
                const char c = s[at++];
                if (c == '\\') ++at;
                else if (c == '"') return;
            }
        } else if (s.at(at) == '{' || s.at(at) == '[') {
            const char end = s[at++] == '{' ? '}' : ']';
            for (;;) {
                space(s, at);
                if (s.at(at) == end) { ++at; return; }
                if (s.at(at) == ',' || s.at(at) == ':') { ++at; continue; }
                token(s, at);
            }
        } else {
            while (at < s.size() && s[at] != ',' && s[at] != '}' && s[at] != ']' &&
                   s[at] != ' ' && s[at] != '\n' && s[at] != '\r' && s[at] != '\t') ++at;
        }
    }
public:
    explicit Arguments(std::string_view source = "{}") : values_(runtime::parse_value(source)) {
        if (!values_.is_object()) throw Failure("invalid", "arguments must be an object");
        std::size_t at = 0;
        space(source, at);
        ++at;
        for (;;) {
            space(source, at);
            if (source.at(at) == '}') break;
            auto start = at;
            token(source, at);
            auto key = runtime::parse_value(source.substr(start, at-start)).as<std::string>();
            space(source, at);
            ++at; // colon, already validated
            space(source, at);
            start = at;
            token(source, at);
            raw_[key] = std::string(source.substr(start, at-start));
            space(source, at);
            if (source.at(at) == ',') ++at;
        }
    }
    bool has(const std::string& key) const { return values_.contains(key); }
    std::string raw(const std::string& key, std::string fallback = "null") const {
        auto found = raw_.find(key);
        return found == raw_.end() ? std::move(fallback) : found->second;
    }
    Value value(const std::string& key, Value fallback = Value::null()) const {
        return has(key) ? values_.at(key) : fallback;
    }
    std::string string(const std::string& key, std::string fallback = {}) const {
        if (!has(key)) return fallback;
        if (!runtime::is_text(values_.at(key))) throw Failure("invalid", key + " is a string");
        return values_.at(key).as<std::string>();
    }
    std::string required(const std::string& key) const {
        auto out = string(key);
        if (out.empty()) throw Failure("invalid", key + " is required");
        return out;
    }
    std::int64_t integer(const std::string& key, std::int64_t fallback) const {
        if (!has(key)) return fallback;
        auto source = raw(key);
        if (source.find_first_not_of("-0123456789") != std::string::npos)
            throw Failure("invalid", key + " is an integer");
        try { return std::stoll(source); }
        catch (...) { throw Failure("invalid", key + " is an integer"); }
    }
    bool boolean(const std::string& key, bool fallback = false) const {
        if (!has(key)) return fallback;
        if (!values_.at(key).is_bool()) throw Failure("invalid", key + " is a boolean");
        return values_.at(key).as<bool>();
    }
    std::vector<std::string> strings(const std::string& key) const {
        if (!has(key)) return {};
        const auto& array = values_.at(key);
        if (!array.is_array()) throw Failure("invalid", key + " is an array of strings");
        std::vector<std::string> out;
        for (const auto& item : array.array_range()) {
            if (!runtime::is_text(item)) throw Failure("invalid", key + " is an array of strings");
            out.push_back(item.as<std::string>());
        }
        return out;
    }
    std::optional<runtime::Metadata> metadata() const {
        if (!has("meta")) return {};
        const auto& map = values_.at("meta");
        if (!map.is_object()) throw Failure("invalid", "meta is an object of strings");
        runtime::Metadata out;
        for (const auto& member : map.object_range()) {
            if (!runtime::is_text(member.value())) throw Failure("invalid", "meta is an object of strings");
            out[std::string(member.key())] = member.value().as<std::string>();
        }
        return out;
    }
    Wait wait() const { return Wait::after(std::chrono::milliseconds(integer("within_ms", 5000))); }
    bool lazy() const {
        auto mode = string("consume", "eager");
        if (mode == "lazy") return true;
        if (mode == "eager" || mode.empty()) return false;
        throw Failure("invalid", "consume is eager or lazy");
    }
    const Value& values() const { return values_; }
};

inline Value metadata(const runtime::Metadata& fields) {
    Value out = object();
    for (const auto& [key, value] : fields) out.insert_or_assign(key, value);
    return out;
}

template<class T> class Inbox {
    std::mutex mutex_;
    std::condition_variable_any changed_;
    std::deque<T> items_;
    bool closed_ = false;
public:
    void put(T item) {
        { std::lock_guard lock(mutex_); items_.push_back(std::move(item)); }
        changed_.notify_all();
    }
    void close() {
        { std::lock_guard lock(mutex_); closed_ = true; }
        changed_.notify_all();
    }
    template<class Match> T take(Wait wait, Match match, std::string ended = "timeout") {
        std::unique_lock lock(mutex_);
        for (;;) {
            auto it = std::find_if(items_.begin(), items_.end(), match);
            if (it != items_.end()) {
                auto item = std::move(*it);
                items_.erase(it);
                return item;
            }
            if (closed_) throw Failure(ended, "nothing further can arrive");
            wait.check();
            changed_.wait_until(lock, wait.stop, wait.deadline, [&] {
                return closed_ || std::find_if(items_.begin(), items_.end(), match) != items_.end();
            });
        }
    }
    std::vector<T> snapshot(bool drain) {
        std::lock_guard lock(mutex_);
        std::vector<T> out(items_.begin(), items_.end());
        if (drain) items_.clear();
        return out;
    }
};

inline Value failure(std::exception_ptr error, bool call = false, bool disconnected = false) {
    try { std::rethrow_exception(error); }
    catch (const Failure& e) { return e.detail; }
    catch (const runtime::PublicError& e) {
        auto out = object({{"code", e.code}, {"message", e.message}});
        if (e.data) out["data"] = *e.data;
        return out;
    }
    catch (const duplex::Cancelled& e) { return object({{"code", "cancelled"}, {"message", e.what()}}); }
    catch (const duplex::DeadlineExceeded& e) {
        return object({{"code", call ? "request_timeout" : "timeout"}, {"message", e.what()}});
    }
    catch (const duplex::CloseError& e) {
        if (call) return object({{"code", "disconnected"}, {"message", e.what()}});
        return object({{"code", "closed"}, {"message", e.what()}, {"close_code", e.code}, {"reason", e.reason}});
    }
    catch (const duplex::Closed& e) {
        return object({{"code", call ? "disconnected" : "closed"}, {"message", e.what()}});
    }
    catch (const std::invalid_argument& e) { return object({{"code", "invalid"}, {"message", e.what()}}); }
    catch (const std::exception& e) {
        return object({{"code", disconnected ? "disconnected" : "failed"}, {"message", e.what()}});
    }
    catch (...) { return object({{"code", "internal"}, {"message", "unknown exception"}}); }
}

inline std::string base64_encode(std::string_view bytes) {
    constexpr auto chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    std::string out;
    for (std::size_t at = 0; at < bytes.size(); at += 3) {
        const auto left = bytes.size()-at;
        const auto n = (static_cast<unsigned char>(bytes[at]) << 16) |
            (left > 1 ? static_cast<unsigned char>(bytes[at+1]) << 8 : 0) |
            (left > 2 ? static_cast<unsigned char>(bytes[at+2]) : 0);
        out += chars[(n >> 18) & 63]; out += chars[(n >> 12) & 63];
        out += left > 1 ? chars[(n >> 6) & 63] : '=';
        out += left > 2 ? chars[n & 63] : '=';
    }
    return out;
}
inline std::string base64_decode(std::string_view text) {
    constexpr std::string_view chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    if (text.size() % 4) throw Failure("invalid", "invalid base64");
    std::string out;
    for (std::size_t at = 0; at < text.size(); at += 4) {
        unsigned n = 0, padding = 0;
        for (unsigned k = 0; k < 4; ++k) {
            auto c = text[at+k];
            if (c == '=' && k >= 2 && at+4 == text.size()) { ++padding; n <<= 6; }
            else {
                auto digit = chars.find(c);
                if (digit == chars.npos || padding) throw Failure("invalid", "invalid base64");
                n = (n << 6) | static_cast<unsigned>(digit);
            }
        }
        out += static_cast<char>((n >> 16) & 255);
        if (padding < 2) out += static_cast<char>((n >> 8) & 255);
        if (padding < 1) out += static_cast<char>(n & 255);
    }
    return out;
}
} // namespace nightseam::conformance
