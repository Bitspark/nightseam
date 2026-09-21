#pragma once

#include <nightseam/duplex/conn.hpp>
#include <functional>
#include <map>
#include <optional>
#include <string_view>
#include <vector>

namespace nightseam::duplex {

using Path = std::vector<std::string>;
using Detach = std::function<void()>;

struct InvalidPath : std::runtime_error { InvalidPath() : std::runtime_error("invalid wire path") {} };
struct NoRoute : std::runtime_error { NoRoute() : std::runtime_error("wire path has no destination") {} };
struct ReceiverExists : std::runtime_error { ReceiverExists() : std::runtime_error("wire path already has a receiver") {} };

enum class ProfileKind { request, response, event, cancel };

struct ProfileError {
    std::string code;
    std::string message;
    std::optional<std::string> data;
    bool operator==(const ProfileError&) const = default;
};

// Payloads are their original JSON, including numeric lexemes. Routing is
// supplied separately by Send; a message cannot contain a conflicting path.
struct ProfileFrame {
    int version = 1;
    ProfileKind kind = ProfileKind::event;
    std::string id;
    std::optional<std::string> params;
    std::optional<std::string> result;
    std::optional<ProfileError> error;
    std::optional<std::string> data;
    std::string traceparent;
    std::string tracestate;
    std::map<std::string, std::string> meta;
    bool operator==(const ProfileFrame&) const = default;
};

class Wire;
using WirePtr = std::shared_ptr<Wire>;

// Local capability identity survives routing and is never serialized.
struct ReturnAddress { WirePtr wire; };
struct Message {
    ProfileFrame frame;
    std::shared_ptr<ReturnAddress> return_address;
    bool operator==(const Message&) const = default;
};

struct Receiver {
    bool namespace_ = false;
    std::function<void(const Path&, const Message&)> message;
    std::function<void(int, const std::string&)> closed;
};

// Send admits or refuses without executing a destination handler. A root owns
// asynchronous dispatch and bounds; views only compose routing. Receive's
// idempotent detach prevents new dispatch, preserving captured cancellations.
class Wire {
public:
    virtual ~Wire() = default;
    virtual void send(const Path& path, const Message& message) = 0;
    virtual Detach receive(const Path& path, Receiver receiver) = 0;
    virtual void close(int code = 1000, std::string reason = {}) = 0;
};

// Canonical UTF-8 byte-length segments: [] is "", [""] is "0:".
std::string encode_path(const Path& path);
Path decode_path(std::string_view encoded);

// Selecting owns no queue. Closing a selection closes its selected endpoint.
WirePtr at(WirePtr root, Path path);
// A mount consumes one opaque segment; closing it detaches its own receivers
// while leaving its borrowed children usable. The children map is copied.
WirePtr mount(std::map<std::string, WirePtr> children);

} // namespace nightseam::duplex
