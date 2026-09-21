#include "driver.hpp"
#include "recorded_wire.hpp"
#include <nightseam/duplex/ws.hpp>
#include <iostream>

namespace nightseam::conformance {
namespace {
using namespace std::chrono_literals;

struct Handle {
    virtual ~Handle() = default;
    virtual void stop() {}
    virtual void join() {}
};

struct Connection : Handle {
    std::shared_ptr<duplex::Conn> conn;
    bool lazy;
    Inbox<duplex::Frame> frames;
    std::mutex mutex;
    std::condition_variable changed;
    std::exception_ptr end;
    std::jthread reader;

    Connection(std::shared_ptr<duplex::Conn> c, bool is_lazy) : conn(std::move(c)), lazy(is_lazy) {
        if (!lazy) reader = std::jthread([this] {
            try { for (;;) frames.put(conn->receive()); }
            catch (...) { finish(std::current_exception()); }
        });
    }
    ~Connection() override { stop(); join(); }
    void finish(std::exception_ptr error) {
        { std::lock_guard lock(mutex); if (!end) end = error; }
        frames.close();
        changed.notify_all();
    }
    void stop() override {
        conn->abort();
        finish(std::make_exception_ptr(duplex::Closed()));
    }
    void join() override { if (reader.joinable()) reader.join(); }
    duplex::Frame receive(Wait wait) {
        if (lazy) {
            try { return conn->receive(wait); }
            catch (const duplex::DeadlineExceeded&) { throw; }
            catch (const duplex::Cancelled&) { throw; }
            catch (...) { finish(std::current_exception()); throw; }
        }
        try { return frames.take(wait, [](const auto&) { return true; }); }
        catch (const Failure&) {
            std::lock_guard lock(mutex);
            if (end) std::rethrow_exception(end);
            throw;
        }
    }
    Value await_close(Wait wait) {
        if (lazy) {
            for (;;) {
                { std::lock_guard lock(mutex); if (end) break; }
                try { conn->receive(wait); }
                catch (const duplex::DeadlineExceeded&) { throw; }
                catch (...) { finish(std::current_exception()); break; }
            }
        }
        std::unique_lock lock(mutex);
        if (!changed.wait_until(lock, wait.deadline, [&] { return static_cast<bool>(end); }))
            throw duplex::DeadlineExceeded();
        try { std::rethrow_exception(end); }
        catch (const duplex::CloseError& e) { return object({{"code", e.code}, {"reason", e.reason}}); }
        catch (...) { return object({{"code", 1006}, {"reason", ""}}); }
    }
};

struct Listener : Handle {
    duplex::ws::Listener listener;
    Arguments options;
    bool peer;
    Listener(duplex::ws::Options ws, Arguments opts, bool is_peer)
        : listener("127.0.0.1", 0, std::move(ws)), options(std::move(opts)), peer(is_peer) {}
    void stop() override { listener.close(); }
};

Value normalize(const runtime::Observation& e, bool with_trace) {
    auto out = object({{"type", e.type}});
    auto put = [&](const std::string& key, const std::string& value) {
        if (!value.empty()) out.insert_or_assign(key, value);
    };
    if (e.type == "connection.opened") out["role"] = e.role == runtime::Role::client ? "client" : "server";
    else if (e.type == "connection.closed") {
        out["code"] = e.code; put("reason", e.reason); out["local"] = e.local;
    } else if (e.type == "frame.sent" || e.type == "frame.received") {
        put("kind", e.kind); put("name", e.name); put("id", e.id); put("family", e.family);
        out["bytes"] = e.bytes > 0;
    } else if (e.type == "request.started" || e.type == "request.ended") {
        put("id", e.id); put("method", e.method); put("family", e.family); out["incoming"] = e.incoming;
        if (e.type == "request.ended") {
            out["duration"] = e.duration >= 0s; put("outcome", e.outcome); put("error_code", e.error_code);
        }
    } else if (e.type == "event.emitted" || e.type == "event.delivered") {
        put("name", e.name); put("family", e.family); out["bytes"] = e.bytes > 0;
    } else if (e.type == "backpressure") {
        out["queued"] = e.queued; out["stalled"] = e.stalled; out["deadline"] = e.deadline >= 0s;
    } else if (e.type == "handler.panic") {
        put("method", e.method); put("value", e.value); put("family", e.family);
    }
    if (with_trace && !e.trace.parent.empty()) {
        const auto& parent = e.trace.parent;
        Value trace;
        if (parent.size() >= 55 && parent[2] == '-' && parent[35] == '-' && parent[52] == '-') {
            trace = object({{"trace_id", parent.substr(3, 32)}, {"span_id", parent.substr(36, 16)},
                            {"flags", parent.substr(53)}});
            if (!e.trace.state.empty()) trace["state"] = e.trace.state;
        } else trace = object({{"traceparent", parent}});
        out["trace"] = std::move(trace);
    }
    return out;
}

// Kept independently of Peer so every callback retains its inboxes until the
// runtime has joined. Reset first releases held callbacks, then joins peers.
struct PeerState {
    Inbox<Value> events, requests;
    Inbox<runtime::Observation> observations;
    std::mutex mutex;
    std::condition_variable_any changed;
    std::map<std::string, std::uint64_t> delivered;
    std::stop_source stopping;
    bool observable = false;
    void stop() {
        stopping.request_stop();
        events.close(); requests.close(); changed.notify_all();
    }
    void observe(const runtime::Observation& event) {
        if (observable) observations.put(event);
        if (event.type == "connection.closed") stop();
    }
    void event(const runtime::RequestContext& context, const Value& value) {
        auto record = object({{"name", context.method}, {"data", value}});
        if (context.meta && !context.meta->empty()) record["meta"] = metadata(*context.meta);
        events.put(std::move(record));
        { std::lock_guard lock(mutex); ++delivered[context.method]; }
        changed.notify_all();
    }
    void phase(const runtime::RequestContext& context, const std::string& method,
               const std::string& phase, const std::string& outcome = {}) {
        auto record = object({{"id", context.id}, {"method", method}, {"phase", phase}});
        if (!outcome.empty()) record["outcome"] = outcome;
        if (phase == "started" && context.meta && !context.meta->empty()) record["meta"] = metadata(*context.meta);
        requests.put(std::move(record));
    }
};

struct Peer : Handle {
    std::shared_ptr<PeerState> state;
    std::unique_ptr<runtime::Peer> peer;
    Peer(std::shared_ptr<duplex::Conn> conn, runtime::Role role, const Arguments& args, std::string subprotocol)
        : state(std::make_shared<PeerState>()) {
        runtime::PeerOptions opts;
        state->observable = args.boolean("observe");
        opts.max_frame_bytes = static_cast<std::size_t>(args.integer("max_frame_bytes", opts.max_frame_bytes));
        opts.max_pending_requests = static_cast<std::size_t>(args.integer("max_pending_requests", opts.max_pending_requests));
        opts.queue_capacity = static_cast<std::size_t>(args.integer("queue_capacity", opts.queue_capacity));
        opts.request_timeout = std::chrono::milliseconds(args.integer("request_timeout_ms", opts.request_timeout.count()));
        opts.write_timeout = std::chrono::milliseconds(args.integer("write_timeout_ms", opts.write_timeout.count()));
        opts.subprotocol = std::move(subprotocol);
        if (args.boolean("propagate")) opts.propagator = runtime::default_propagator();
        if (args.has("families")) {
            auto families = args.value("families");
            if (!families.is_object()) throw Failure("invalid", "families maps names to families");
            for (const auto& m : families.object_range()) {
                if (!runtime::is_text(m.value())) throw Failure("invalid", "families maps names to families");
                opts.families[std::string(m.key())] = m.value().as<std::string>();
            }
        }
        for (const auto& m : args.values().object_range()) {
            const auto key = m.key();
            if (key != "max_frame_bytes" && key != "max_pending_requests" && key != "queue_capacity" &&
                key != "request_timeout_ms" && key != "write_timeout_ms" && key != "observe" &&
                key != "propagate" && key != "families") throw Failure("unsupported", "option " + std::string(key));
        }
        opts.observer = [s = state](const runtime::Observation& event) { s->observe(event); };
        opts.event_listener = [s = state](const runtime::RequestContext& ctx, runtime::Peer&, const Value& data) {
            s->event(ctx, data);
        };
        peer = std::make_unique<runtime::Peer>(std::move(conn), role, std::move(opts));
    }
    ~Peer() override { stop(); join(); }
    void stop() override { state->stop(); if (peer) peer->close(); }
    void join() override { peer.reset(); }
    void handle(const std::string& method, Arguments behavior) {
        const auto kind = behavior.string("kind");
        peer->handle_raw(method, [s = state, method, behavior = std::move(behavior), kind]
            (const runtime::RequestContext& ctx, runtime::Peer& remote, const Value&) -> std::string {
            std::uint64_t held_generation = 0;
            if (kind == "hold") {
                std::lock_guard lock(s->mutex);
                held_generation = s->delivered[behavior.string("until")];
            }
            s->phase(ctx, method, "started");
            try {
                std::string result;
                if (kind == "echo") result = ctx.raw_payload;
                else if (kind == "return") result = behavior.raw("value");
                else if (kind == "fail") {
                    runtime::PublicError error(behavior.string("code"), behavior.string("message"));
                    if (behavior.has("data")) { error.data = behavior.value("data"); error.raw_data = behavior.raw("data"); }
                    throw error;
                } else if (kind == "wait") {
                    std::mutex mutex;
                    std::condition_variable_any changed;
                    std::unique_lock lock(mutex);
                    changed.wait_until(lock, ctx.wait.stop, ctx.wait.deadline, [] { return false; });
                    ctx.check();
                    throw duplex::Cancelled();
                } else if (kind == "hold") {
                    std::unique_lock lock(s->mutex);
                    s->changed.wait(lock, s->stopping.get_token(), [&] {
                        return s->delivered[behavior.string("until")] != held_generation;
                    });
                    result = behavior.raw("value");
                } else if (kind == "panic") {
                    auto value = behavior.value("value", "the handler gave up");
                    throw std::runtime_error(runtime::is_text(value) ? value.as<std::string>() : runtime::stringify(value));
                } else if (kind == "reverse") {
                    runtime::CallOptions opts; opts.context = ctx;
                    result = runtime::stringify(remote.call_raw(behavior.string("method"),
                        behavior.raw("params", ctx.raw_payload), opts));
                } else if (kind == "emit") {
                    runtime::CallOptions opts; opts.context = ctx;
                    remote.emit_raw(behavior.string("event"), behavior.raw("data"), opts);
                    result = behavior.raw("then");
                } else throw runtime::PublicError("internal", "no such behaviour: " + kind);
                s->phase(ctx, method, "ended", "ok");
                return result;
            } catch (const duplex::Cancelled&) {
                s->phase(ctx, method, "ended", "cancelled"); throw;
            } catch (const runtime::PublicError&) {
                s->phase(ctx, method, "ended", ctx.cancelled() ? "cancelled" : "error"); throw;
            } catch (const duplex::DeadlineExceeded&) {
                s->phase(ctx, method, "ended", ctx.cancelled() ? "cancelled" : "error"); throw;
            } catch (...) {
                s->phase(ctx, method, "ended", "panic"); throw;
            }
        });
    }
};

struct Call : Handle {
    std::shared_ptr<Peer> peer;
    std::stop_source cancellation;
    std::mutex mutex;
    std::condition_variable changed;
    Value result;
    std::exception_ptr error;
    bool done = false;
    std::jthread worker;
    Call(std::shared_ptr<Peer> p, std::string method, std::string params, runtime::CallOptions opts)
        : peer(std::move(p)) {
        opts.wait.stop = cancellation.get_token();
        worker = std::jthread([this, method = std::move(method), params = std::move(params), opts] {
            Value value;
            std::exception_ptr failed;
            try { value = peer->peer->call_raw(method, params, opts); }
            catch (...) { failed = std::current_exception(); }
            { std::lock_guard lock(mutex); result = std::move(value); error = failed; done = true; }
            changed.notify_all();
        });
    }
    ~Call() override { stop(); join(); }
    void stop() override { cancellation.request_stop(); }
    void join() override { if (worker.joinable()) worker.join(); }
    Value await(Wait wait) {
        std::unique_lock lock(mutex);
        if (!changed.wait_until(lock, wait.deadline, [&] { return done; })) throw duplex::DeadlineExceeded();
        if (error) return object({{"error", failure(error, true, peer->peer->ended())}});
        return object({{"result", result}});
    }
};

class Testee {
    std::map<std::string, std::shared_ptr<Handle>> handles_;
    std::uint64_t next_ = 0;
    std::string mint(std::string prefix, std::shared_ptr<Handle> value) {
        auto handle = prefix + std::to_string(++next_);
        handles_.emplace(handle, std::move(value));
        return handle;
    }
    template<class T> std::shared_ptr<T> get(const Arguments& r) {
        auto handle = r.required("on");
        auto found = handles_.find(handle);
        if (found == handles_.end()) throw Failure("unknown_handle", handle);
        auto value = std::dynamic_pointer_cast<T>(found->second);
        if (!value) throw Failure("invalid", handle + " is a different kind of handle");
        return value;
    }
    static duplex::ws::Options ws_options(const Arguments& r, bool peer) {
        duplex::ws::Options opts;
        if (peer) {
            Arguments args(r.raw("options", "{}"));
            opts.max_frame_bytes = static_cast<std::size_t>(args.integer("max_frame_bytes", 1 << 20));
            opts.subprotocols = r.strings("subprotocols");
        } else opts.max_frame_bytes = static_cast<std::size_t>(r.integer("limit", 1 << 20));
        return opts;
    }
public:
    bool bye = false;
    ~Testee() { reset(); }
    void reset() {
        // Cancellation precedes joining, so calls and deliberately blocked
        // handlers cannot retain peers or transports across scenarios.
        for (auto& [_, handle] : handles_) handle->stop();
        for (auto& [_, handle] : handles_) if (auto call = std::dynamic_pointer_cast<Call>(handle)) call->join();
        for (auto& [_, handle] : handles_) handle->join();
        handles_.clear();
    }
    Value dispatch(const Arguments& r) {
        const auto op = r.required("op");
        if (op == "hello") return object({{"driver", 1}, {"language", "cpp"},
            {"layers", runtime::parse_value(R"(["seam","peer"])")},
            {"features", runtime::parse_value(R"(["listen","pipe","observer","propagator","lazy"])")}});
        if (op == "bye" || op == "reset") { reset(); bye = op == "bye"; return object(); }
        if (op == "peer.recorded_wire_witness") return recorded_wire_witness(r.wait());
        if (op == "conn.listen" || op == "peer.listen") {
            const bool peer = op == "peer.listen";
            auto listener = std::make_shared<Listener>(ws_options(r, peer), Arguments(r.raw("options", "{}")), peer);
            return object({{"handle", mint("l", listener)}, {"url", listener->listener.url()}});
        }
        if (op == "conn.accept" || op == "peer.accept") {
            auto listener = get<Listener>(r);
            const bool peer = op == "peer.accept";
            if (listener->peer != peer) throw Failure("invalid", "listener has a different kind");
            auto connection = listener->listener.accept(r.wait());
            if (!peer) return object({{"handle", mint("c", std::make_shared<Connection>(connection.conn, r.lazy()))}});
            auto p = std::make_shared<Peer>(connection.conn, runtime::Role::server, listener->options, connection.subprotocol);
            return object({{"handle", mint("p", p)}, {"subprotocol", connection.subprotocol}});
        }
        if (op == "conn.dial" || op == "peer.dial") {
            const bool peer = op == "peer.dial";
            auto connection = duplex::ws::dial(r.required("url"), ws_options(r, peer), r.wait());
            if (!peer) return object({{"handle", mint("c", std::make_shared<Connection>(connection.conn, r.lazy()))}});
            auto p = std::make_shared<Peer>(connection.conn, runtime::Role::client,
                Arguments(r.raw("options", "{}")), connection.subprotocol);
            return object({{"handle", mint("p", p)}, {"subprotocol", connection.subprotocol}});
        }
        if (op == "conn.pipe") {
            auto [a, b] = duplex::pipe(static_cast<std::size_t>(r.integer("limit", 1 << 20)));
            const bool lazy = r.lazy();
            return object({{"a", mint("c", std::make_shared<Connection>(a, lazy))},
                           {"b", mint("c", std::make_shared<Connection>(b, lazy))}});
        }
        if (op.starts_with("conn.")) {
            auto c = get<Connection>(r);
            if (op == "conn.send") {
                const auto kind = r.required("kind");
                if (kind == "text") c->conn->send({duplex::Kind::text, r.string("text")}, r.wait());
                else if (kind == "binary") c->conn->send({duplex::Kind::binary, base64_decode(r.string("base64"))}, r.wait());
                else throw Failure("invalid", "kind is text or binary");
            } else if (op == "conn.receive") {
                auto frame = c->receive(r.wait());
                if (frame.kind == duplex::Kind::text) return object({{"kind", "text"}, {"text", frame.data}});
                return object({{"kind", "binary"}, {"base64", base64_encode(frame.data)}});
            } else if (op == "conn.close") {
                try { c->conn->close(static_cast<int>(r.integer("code", 1000)), r.string("reason"), r.wait()); }
                catch (const duplex::Closed&) {}
                c->finish(std::make_exception_ptr(duplex::Closed()));
            } else if (op == "conn.abort") c->stop();
            else if (op == "conn.await_close") return c->await_close(r.wait());
            else throw Failure("unsupported", op);
            return object();
        }
        if (op == "peer.over") {
            auto c = get<Connection>(r);
            if (!c->lazy) throw Failure("invalid", "a peer takes a lazily consumed connection");
            const auto role = r.required("role");
            if (role != "client" && role != "server") throw Failure("invalid", "role is client or server");
            auto p = std::make_shared<Peer>(c->conn, role == "client" ? runtime::Role::client : runtime::Role::server,
                Arguments(r.raw("options", "{}")), "");
            return object({{"handle", mint("p", p)}});
        }
        if (op == "call.await") return get<Call>(r)->await(r.wait());
        if (op == "call.cancel") { get<Call>(r)->stop(); return object(); }
        if (op.starts_with("peer.")) {
            auto p = get<Peer>(r);
            if (op == "peer.handle") p->handle(r.required("method"), Arguments(r.raw("behavior", "{}")));
            else if (op == "peer.identity") p->peer->identity(r.required("path"), r.string("digest"));
            else if (op == "peer.check_identity") {
                runtime::CallOptions opts; opts.wait = r.wait();
                try { p->peer->check_identity(r.required("path"), r.string("digest"), opts); }
                catch (...) { Failure e("failed", "identity check failed"); e.detail = failure(std::current_exception(), true, p->peer->ended()); throw e; }
            } else if (op == "peer.on_event") {
                Arguments behavior(r.raw("behavior", "{}"));
                const auto kind = behavior.string("kind", "record");
                const auto name = r.required("name");
                if (kind == "block") p->peer->handle_event(name,
                    [](const runtime::RequestContext& ctx, runtime::Peer&, const Value&) {
                        std::mutex mutex; std::condition_variable_any changed; std::unique_lock lock(mutex);
                        changed.wait_until(lock, ctx.wait.stop, ctx.wait.deadline, [] { return false; });
                    });
                else if (kind == "panic") {
                    auto value = behavior.value("value", "the handler gave up");
                    auto message = runtime::is_text(value) ? value.as<std::string>() : runtime::stringify(value);
                    p->peer->handle_event(name, [message](const runtime::RequestContext&, runtime::Peer&, const Value&) {
                        throw std::runtime_error(message);
                    });
                } else if (kind != "record" && !kind.empty()) throw Failure("invalid", "an event handler records, blocks or panics");
            } else if (op == "peer.call") {
                runtime::CallOptions opts;
                opts.timeout = std::chrono::milliseconds(r.integer("timeout_ms", 0)); opts.meta = r.metadata();
                auto call = std::make_shared<Call>(p, r.required("method"), r.raw("params"), opts);
                return object({{"handle", mint("call", call)}});
            } else if (op == "peer.emit") {
                runtime::CallOptions opts; opts.wait = r.wait(); opts.meta = r.metadata();
                p->peer->emit_raw(r.required("event"), r.raw("data"), opts);
            } else if (op == "peer.await_event") {
                auto name = r.required("name");
                auto event = p->state->events.take(r.wait(), [&](const auto& e) {
                    return e.at("name").template as<std::string>() == name;
                }, "disconnected");
                event.erase("name");
                return event;
            } else if (op == "peer.await_request") {
                auto method = r.required("method"), phase = r.required("phase");
                return p->state->requests.take(r.wait(), [&](const auto& e) {
                    return e.at("method").template as<std::string>() == method && e.at("phase").template as<std::string>() == phase;
                });
            } else if (op == "peer.observed") {
                if (!p->state->observable) throw Failure("invalid", "the peer was made without observe");
                Value out(jsoncons::json_array_arg);
                for (const auto& event : p->state->observations.snapshot(r.boolean("drain", true)))
                    out.push_back(normalize(event, r.boolean("trace")));
                return out;
            } else if (op == "peer.close") p->peer->close();
            else if (op == "peer.await_close") {
                auto close = p->peer->await_close(r.wait());
                return object({{"clean", close.code == 1000}, {"code", close.code}});
            } else throw Failure("unsupported", op);
            return object();
        }
        throw Failure("unsupported", "no such op: " + op);
    }
    Value serve(std::string_view line) {
        std::int64_t id = 0;
        try {
            Arguments args(line);
            if (!args.has("id")) throw Failure("invalid", "a request carries an integer id");
            id = args.integer("id", 0);
            return object({{"id", id}, {"ok", dispatch(args)}});
        } catch (...) { return object({{"id", id}, {"error", failure(std::current_exception())}}); }
    }
};
} // namespace
} // namespace nightseam::conformance

int main() {
    nightseam::conformance::Testee testee;
    std::string line;
    while (std::getline(std::cin, line)) {
        if (line.find_first_not_of(" \t\r\n") == std::string::npos) continue;
        std::cout << nightseam::runtime::stringify(testee.serve(line)) << '\n' << std::flush;
        if (testee.bye) break;
    }
}
