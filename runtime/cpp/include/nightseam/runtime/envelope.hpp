#pragma once

#include <nightseam/runtime/value.hpp>
#include <map>
#include <optional>
#include <stdexcept>
#include <string>
#include <vector>

namespace nightseam::runtime {

enum class Role { client, server };
struct Trace { std::string parent, state; };
using Metadata = std::map<std::string, std::string>;
using Path = std::vector<std::string>;

struct PublicError : std::runtime_error {
    std::string code, message;
    std::optional<Value> data;
    std::optional<std::string> raw_data;
    PublicError(std::string code, std::string message, std::optional<Value> data = {})
        : std::runtime_error(code + ": " + message), code(std::move(code)),
          message(std::move(message)), data(std::move(data)) {}
};

struct Envelope {
    std::string kind, id, method, event;
    std::optional<Value> params, result, data;
    std::optional<std::string> raw_params, raw_result, raw_data;
    std::optional<PublicError> error;
    Trace trace;
    std::optional<Metadata> meta;
    std::string name() const { return kind == "event" ? event : method; }
};

bool valid_id(std::string_view id, std::string_view prefix);
bool valid_traceparent(std::string_view parent);
Envelope decode_envelope(std::string_view source, Role recipient);
std::string encode_envelope(const Envelope& frame);
std::string encode_path(const Path& path);
Path decode_path(std::string_view name);

} // namespace nightseam::runtime
