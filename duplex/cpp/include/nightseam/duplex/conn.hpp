#pragma once

#include <chrono>
#include <cstddef>
#include <memory>
#include <stdexcept>
#include <stop_token>
#include <string>
#include <utility>

namespace nightseam::duplex {

enum class Kind { text, binary };

struct Frame {
    Kind kind;
    std::string data; // bytes, including zero bytes, owned by this frame
};

struct Cancelled : std::runtime_error { Cancelled() : std::runtime_error("operation cancelled") {} };
struct DeadlineExceeded : std::runtime_error { DeadlineExceeded() : std::runtime_error("operation deadline exceeded") {} };
struct Closed : std::runtime_error { Closed() : std::runtime_error("duplex connection closed") {} };
struct FrameTooLarge : std::runtime_error { using std::runtime_error::runtime_error; };

struct CloseError : std::runtime_error {
    int code;
    std::string reason;
    CloseError(int code, std::string reason)
        : std::runtime_error("duplex connection closed by the remote side (" + std::to_string(code) + "): " + reason),
          code(code), reason(std::move(reason)) {}
};

// Every blocking operation is cancellable and may have its own deadline.
// A receive cancellation does not itself close an otherwise usable pipe.
struct Wait {
    std::stop_token stop;
    std::chrono::steady_clock::time_point deadline = std::chrono::steady_clock::time_point::max();

    static Wait after(std::chrono::steady_clock::duration duration) {
        return {{}, std::chrono::steady_clock::now() + duration};
    }
    void check() const {
        if (stop.stop_requested()) throw Cancelled();
        if (std::chrono::steady_clock::now() >= deadline) throw DeadlineExceeded();
    }
};

// The protocol depends only on Conn. One send and one receive may run
// concurrently; Close and Abort release blocked operations. Receive owns
// the complete frame it returns and never delivers an oversized frame.
class Conn {
public:
    virtual ~Conn() = default;
    virtual void send(const Frame& frame, Wait wait = {}) = 0;
    virtual Frame receive(Wait wait = {}) = 0;
    virtual void close(int code = 1000, std::string reason = {}, Wait wait = {}) = 0;
    virtual void abort() noexcept = 0;
};

// The byte limit applies independently to what each endpoint receives.
// Each direction is bounded by capacity frames; zero bytes means unlimited.
std::pair<std::shared_ptr<Conn>, std::shared_ptr<Conn>> pipe(std::size_t byte_limit = 1 << 20, std::size_t capacity = 8);

} // namespace nightseam::duplex
