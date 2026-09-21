#pragma once

#include <nightseam/duplex/conn.hpp>
#include <nightseam/runtime/envelope.hpp>
#include <chrono>
#include <functional>
#include <memory>

namespace nightseam::runtime {

class Peer;

struct RequestContext {
    duplex::Wait wait;
    Trace trace;
    std::optional<Metadata> meta;
    std::string id, method;
    std::string raw_payload;
    bool cancelled() const;
    void check() const;
};

class Propagator {
public:
    virtual ~Propagator() = default;
    virtual void extract(RequestContext& context, const Trace& trace) = 0;
    virtual Trace inject(const RequestContext& context) = 0;
};
std::shared_ptr<Propagator> default_propagator();

// A concrete, payload-free record: neither values nor metadata can enter it.
struct Observation {
    std::string type, kind, name, id, method, family, outcome, error_code, reason, value;
    Trace trace;
    Role role = Role::client;
    bool incoming = false, local = true, stalled = false;
    int code = 0;
    std::size_t bytes = 0, queued = 0;
    std::chrono::steady_clock::duration duration{}, deadline{};
};
using Observer = std::function<void(const Observation&)>;
using Handler = std::function<Value(const RequestContext&, Peer&, const Value&)>;
using RawHandler = std::function<std::string(const RequestContext&, Peer&, const Value&)>;
using EventHandler = std::function<void(const RequestContext&, Peer&, const Value&)>;

struct CallOptions {
    duplex::Wait wait;
    std::chrono::milliseconds timeout{0};
    // Context propagates trace and cancellation; metadata is always explicit.
    std::optional<RequestContext> context;
    std::optional<Metadata> meta;
};

struct PeerOptions {
    std::size_t max_concurrent_handlers = 64, max_pending_requests = 128;
    std::size_t queue_capacity = 128, max_frame_bytes = 1 << 20;
    std::chrono::milliseconds request_timeout{30000}, write_timeout{10000};
    std::map<std::string, Handler> handlers;
    std::map<std::string, EventHandler> events;
    EventHandler event_listener;
    std::map<std::string, std::string> families;
    std::shared_ptr<Propagator> propagator;
    Observer observer;
    std::function<void(Peer&)> prepare;
    std::string subprotocol;
};

struct CloseInfo { bool clean = false; int code = 1006; bool local = true; std::string reason; };

// A Peer owns its Conn and starts reading after prepare has installed routes.
// Calls block and can run concurrently; event handlers run serially.
class Peer {
public:
    Peer(std::shared_ptr<duplex::Conn> connection, Role role, PeerOptions options = {});
    ~Peer();
    Peer(const Peer&) = delete;
    Peer& operator=(const Peer&) = delete;

    void handle(std::string method, Handler handler);
    void handle_raw(std::string method, RawHandler handler);
    void handle_event(std::string name, EventHandler handler);
    Value call(std::string method, Value params = Value(jsoncons::json_object_arg), CallOptions options = {});
    void emit(std::string event, Value data = Value::null(), CallOptions options = {});
    Value call_raw(std::string method, std::optional<std::string> params, CallOptions options = {});
    void emit_raw(std::string event, std::optional<std::string> data, CallOptions options = {});
    void identity(std::string path, std::string digest = {});
    void check_identity(std::string path, std::string digest = {}, CallOptions options = {});
    Value call_path(const Path& path, Value params = Value(jsoncons::json_object_arg), CallOptions options = {});
    void emit_path(const Path& path, Value data = Value::null(), CallOptions options = {});
    void handle_path(const Path& path, Handler handler);
    void handle_event_path(const Path& path, EventHandler handler);
    void close();
    bool ended() const;
    CloseInfo await_close(duplex::Wait wait = {}) const;
    std::exception_ptr error() const;
    Role role() const;
    const std::string& subprotocol() const;
    duplex::Wait context() const;
    void observe(const Observation& event) const;

private:
    struct Impl;
    std::unique_ptr<Impl> impl_;
};

} // namespace nightseam::runtime
