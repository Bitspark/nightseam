#include <nightseam/duplex/ws.hpp>

#include <boost/asio.hpp>
#include <boost/beast/core.hpp>
#include <boost/beast/http.hpp>
#include <boost/beast/websocket.hpp>

#include <algorithm>
#include <atomic>
#include <condition_variable>
#include <deque>
#include <functional>
#include <limits>
#include <mutex>
#include <optional>
#include <thread>

namespace nightseam::duplex::ws {
namespace {
namespace asio = boost::asio;
namespace beast = boost::beast;
namespace http = beast::http;
namespace websocket = beast::websocket;
using tcp = asio::ip::tcp;
using Error = boost::system::error_code;
using namespace std::chrono_literals;

template<class T> struct Completion {
    std::mutex mutex;
    std::condition_variable_any changed;
    bool ready = false;
    std::optional<T> value;
    std::exception_ptr error;

    void finish(T result) {
        { std::lock_guard lock(mutex); if (ready) return; value = std::move(result); ready = true; }
        changed.notify_all();
    }
    void fail(std::exception_ptr failure) {
        { std::lock_guard lock(mutex); if (ready) return; error = failure; ready = true; }
        changed.notify_all();
    }
    bool done() { std::lock_guard lock(mutex); return ready; }
    T get(Wait wait, std::function<void()> cancel = {}) {
        std::unique_lock lock(mutex);
        bool reached = wait.deadline == std::chrono::steady_clock::time_point::max()
            ? changed.wait(lock, wait.stop, [&] { return ready; })
            : changed.wait_until(lock, wait.stop, wait.deadline, [&] { return ready; });
        if (!reached) {
            lock.unlock();
            if (cancel) cancel();
            wait.check();
            throw DeadlineExceeded();
        }
        if (error) std::rethrow_exception(error);
        return std::move(*value);
    }
};

// Each connection owns one executor thread. Callbacks retain socket state,
// never the connection that owns the executor, so destruction cannot join
// that same thread or leave a socket/executor ownership cycle behind.
struct Engine {
    asio::io_context io;
    asio::executor_work_guard<asio::io_context::executor_type> work{asio::make_work_guard(io)};
    std::thread thread{[this] { io.run(); }};
    ~Engine() { work.reset(); io.stop(); thread.join(); }
};

struct Socket {
    websocket::stream<tcp::socket> ws;
    beast::flat_buffer buffer;
    std::atomic<bool> local_closed{false};
    std::exception_ptr remote_error;
    explicit Socket(asio::io_context& io, std::size_t limit) : ws(io) {
        ws.read_message_max(limit == 0 ? std::numeric_limits<std::uint64_t>::max() : limit);
    }
    std::exception_ptr translate(Error error) {
        if (local_closed) return std::make_exception_ptr(Closed());
        if (error == websocket::error::message_too_big) {
            local_closed = true;
            return std::make_exception_ptr(FrameTooLarge("duplex frame exceeds the receive byte limit"));
        }
        if (!remote_error) {
            if (error == websocket::error::closed) {
                auto reason = ws.reason();
                remote_error = std::make_exception_ptr(CloseError(reason.code == 0 ? 1005 : reason.code, std::string(reason.reason)));
            } else {
                remote_error = std::make_exception_ptr(CloseError(1006, ""));
            }
        }
        return remote_error;
    }
    void terminate() {
        Error ignored;
        ws.next_layer().cancel(ignored);
        ws.next_layer().shutdown(tcp::socket::shutdown_both, ignored);
        ws.next_layer().close(ignored);
    }
};

std::vector<std::string> tokens(std::string_view list) {
    std::vector<std::string> out;
    while (!list.empty()) {
        auto comma = list.find(',');
        auto token = list.substr(0, comma);
        while (!token.empty() && (token.front() == ' ' || token.front() == '\t')) token.remove_prefix(1);
        while (!token.empty() && (token.back() == ' ' || token.back() == '\t')) token.remove_suffix(1);
        if (!token.empty()) out.emplace_back(token);
        if (comma == std::string_view::npos) break;
        list.remove_prefix(comma+1);
    }
    return out;
}

struct Address { std::string host, port, authority, target; };
Address address(const std::string& url) {
    if (!url.starts_with("ws://")) throw std::invalid_argument("expected a ws:// URL");
    auto rest = std::string_view(url).substr(5);
    auto slash = rest.find_first_of("/?");
    auto authority = rest.substr(0, slash);
    Address a;
    a.authority = authority;
    a.target = slash == std::string_view::npos ? "/" : std::string(rest.substr(slash));
    if (a.target.starts_with('?')) a.target.insert(a.target.begin(), '/');
    a.port = "80";
    if (authority.starts_with('[')) {
        auto end = authority.find(']');
        if (end == std::string_view::npos) throw std::invalid_argument("invalid IPv6 URL");
        a.host = authority.substr(1, end-1);
        if (end+1 < authority.size()) {
            if (authority[end+1] != ':') throw std::invalid_argument("invalid URL authority");
            a.port = authority.substr(end+2);
        }
    } else {
        auto colon = authority.find(':');
        a.host = authority.substr(0, colon);
        if (colon != std::string_view::npos) a.port = authority.substr(colon+1);
    }
    if (a.host.empty() || a.port.empty() || authority.find('@') != std::string_view::npos || rest.find('#') != std::string_view::npos) {
        throw std::invalid_argument("invalid WebSocket URL");
    }
    return a;
}

class WebSocketConn final : public Conn {
    std::unique_ptr<Engine> engine_ = std::make_unique<Engine>();
    std::shared_ptr<Socket> socket_;
public:
    explicit WebSocketConn(std::size_t limit) : socket_(std::make_shared<Socket>(engine_->io, limit)) {}
    ~WebSocketConn() override { abort(); }

    std::shared_ptr<Completion<std::string>> connect(Address a, Options options) {
        auto done = std::make_shared<Completion<std::string>>();
        auto s = socket_;
        auto resolver = std::make_shared<tcp::resolver>(engine_->io);
        asio::post(engine_->io, [s, resolver, done, a, options] {
            resolver->async_resolve(a.host, a.port, [s, resolver, done, a, options](Error ec, tcp::resolver::results_type endpoints) {
                if (ec) { done->fail(std::make_exception_ptr(std::runtime_error(ec.message()))); return; }
                asio::async_connect(s->ws.next_layer(), endpoints, [s, done, a, options](Error ec, const tcp::endpoint&) {
                    if (ec) { done->fail(std::make_exception_ptr(std::runtime_error(ec.message()))); return; }
                    std::string offered;
                    for (const auto& token : options.subprotocols) { if (!offered.empty()) offered += ", "; offered += token; }
                    s->ws.set_option(websocket::stream_base::decorator([offered](websocket::request_type& request) {
                        if (!offered.empty()) request.set(http::field::sec_websocket_protocol, offered);
                    }));
                    auto response = std::make_shared<websocket::response_type>();
                    s->ws.async_handshake(*response, a.authority, a.target, [s, done, response, options](Error ec) {
                        if (ec) { done->fail(std::make_exception_ptr(std::runtime_error(ec.message()))); return; }
                        auto selected = std::string((*response)[http::field::sec_websocket_protocol]);
                        if (!selected.empty() && std::find(options.subprotocols.begin(), options.subprotocols.end(), selected) == options.subprotocols.end()) {
                            s->terminate();
                            done->fail(std::make_exception_ptr(std::runtime_error("server selected an unoffered subprotocol")));
                            return;
                        }
                        done->finish(std::move(selected));
                    });
                });
            });
        });
        return done;
    }

    std::shared_ptr<Completion<std::string>> handshake(tcp::socket& source, Options options) {
        auto protocol = source.local_endpoint().protocol();
        auto native = source.release();
        Error assign_error;
        socket_->ws.next_layer().assign(protocol, native, assign_error);
        if (assign_error) {
            source.assign(protocol, native); // restore ownership before reporting failure
            throw boost::system::system_error(assign_error);
        }
        auto done = std::make_shared<Completion<std::string>>();
        auto s = socket_;
        asio::post(engine_->io, [s, done, options] {
            auto request = std::make_shared<http::request<http::empty_body>>();
            http::async_read(s->ws.next_layer(), s->buffer, *request, [s, done, request, options](Error ec, std::size_t) {
                if (ec) { done->fail(std::make_exception_ptr(std::runtime_error(ec.message()))); return; }
                auto offered = tokens(std::string((*request)[http::field::sec_websocket_protocol]));
                std::string selected;
                for (const auto& preferred : options.subprotocols) {
                    if (std::find(offered.begin(), offered.end(), preferred) != offered.end()) { selected = preferred; break; }
                }
                s->ws.set_option(websocket::stream_base::decorator([selected](websocket::response_type& response) {
                    if (!selected.empty()) response.set(http::field::sec_websocket_protocol, selected);
                }));
                s->ws.async_accept(*request, [s, done, request, selected](Error ec) {
                    if (ec) done->fail(std::make_exception_ptr(std::runtime_error(ec.message())));
                    else done->finish(selected);
                });
            });
        });
        return done;
    }

    void send(const Frame& frame, Wait wait) override {
        if (socket_->local_closed) throw Closed();
        if (frame.kind != Kind::text && frame.kind != Kind::binary) throw std::invalid_argument("duplex frame of no kind");
        wait.check();
        auto done = std::make_shared<Completion<int>>();
        auto s = socket_;
        auto owned = std::make_shared<Frame>(frame);
        asio::post(engine_->io, [s, done, owned] {
            if (s->local_closed) { done->fail(std::make_exception_ptr(Closed())); return; }
            if (s->remote_error) { done->fail(s->remote_error); return; }
            s->ws.text(owned->kind == Kind::text);
            s->ws.async_write(asio::buffer(owned->data), [s, done, owned](Error ec, std::size_t) {
                if (ec) done->fail(s->translate(ec)); else done->finish(0);
            });
        });
        done->get(wait, [this] { abort(); });
    }

    Frame receive(Wait wait) override {
        if (socket_->local_closed) throw Closed();
        wait.check();
        auto done = std::make_shared<Completion<Frame>>();
        auto s = socket_;
        asio::post(engine_->io, [s, done] {
            if (s->local_closed) { done->fail(std::make_exception_ptr(Closed())); return; }
            if (s->remote_error) { done->fail(s->remote_error); return; }
            s->ws.async_read(s->buffer, [s, done](Error ec, std::size_t) {
                if (ec) { done->fail(s->translate(ec)); return; }
                Frame frame{s->ws.got_text() ? Kind::text : Kind::binary, beast::buffers_to_string(s->buffer.data())};
                s->buffer.consume(s->buffer.size());
                done->finish(std::move(frame));
            });
        });
        return done->get(wait, [this] { abort(); });
    }

    void close(int code, std::string reason, Wait wait) override {
        websocket::close_reason close;
        close.code = static_cast<websocket::close_code>(code);
        close.reason = reason;
        if (socket_->local_closed.exchange(true)) throw Closed();
        auto done = std::make_shared<Completion<int>>();
        auto s = socket_;
        asio::post(engine_->io, [s, done, close] {
            s->ws.async_close(close, [s, done](Error ec) {
                if (ec && ec != websocket::error::closed) done->fail(s->translate(ec)); else done->finish(0);
            });
        });
        done->get(wait, [this] { abort(); });
    }

    void abort() noexcept override {
        socket_->local_closed = true;
        asio::post(engine_->io, [s=socket_] { s->terminate(); });
    }
};

} // namespace

struct Listener::Impl {
    asio::io_context io;
    tcp::acceptor acceptor{io};
    asio::steady_timer timer{io};
    Options options;
    std::string location;
    std::atomic<bool> closed{false};
    std::mutex mutex;
    std::condition_variable_any changed;
    std::deque<Connection> accepted;
    struct Handshake {
        std::shared_ptr<WebSocketConn> conn;
        std::shared_ptr<Completion<std::string>> result;
        std::chrono::steady_clock::time_point deadline = std::chrono::steady_clock::now() + 30s;
    };
    std::vector<Handshake> pending;
    std::thread thread;

    Impl(const std::string& host, std::uint16_t port, Options options) : options(std::move(options)) {
        auto endpoint = tcp::endpoint(asio::ip::make_address(host), port);
        acceptor.open(endpoint.protocol());
        acceptor.set_option(tcp::acceptor::reuse_address(true));
        acceptor.bind(endpoint);
        acceptor.listen();
        location = "ws://" + (endpoint.address().is_v6() ? "["+host+"]" : host) + ":" + std::to_string(acceptor.local_endpoint().port());
        listen();
        thread = std::thread([this] { io.run(); });
    }
    ~Impl() { shutdown(); }

    void listen() {
        auto socket = std::make_shared<tcp::socket>(io);
        acceptor.async_accept(*socket, [this, socket](Error ec) {
            if (closed) return;
            if (!ec && pending.size() < 8) {
                try {
                    auto conn = std::make_shared<WebSocketConn>(options.max_frame_bytes);
                    auto result = conn->handshake(*socket, options);
                    auto was_empty = pending.empty();
                    pending.push_back({std::move(conn), std::move(result)});
                    if (was_empty) watch();
                } catch (...) { /* One failed handshake does not close the listener. */ }
            }
            listen();
        });
    }

    // Only incomplete handshakes need a timer. Their callbacks belong to
    // other executor threads and retain no listener or owning connection.
    void watch() {
        timer.expires_after(5ms);
        timer.async_wait([this](Error ec) {
            if (ec || closed) return;
            for (auto at = pending.begin(); at != pending.end();) {
                if (at->result->done()) {
                    try {
                        auto protocol = at->result->get({});
                        std::lock_guard lock(mutex);
                        if (accepted.size() < 8) accepted.push_back({at->conn, std::move(protocol)});
                    } catch (...) {}
                    at = pending.erase(at);
                    changed.notify_all();
                } else if (std::chrono::steady_clock::now() >= at->deadline) {
                    at = pending.erase(at);
                } else ++at;
            }
            if (!pending.empty()) watch();
        });
    }

    void shutdown() noexcept {
        if (closed.exchange(true)) return;
        changed.notify_all();
        io.stop();
        if (thread.joinable()) thread.join();
        Error ignored;
        acceptor.close(ignored);
        pending.clear();
        std::lock_guard lock(mutex);
        accepted.clear();
    }
};

Connection dial(const std::string& url, Options options, Wait wait) {
    wait.check();
    auto target = address(url);
    auto conn = std::make_shared<WebSocketConn>(options.max_frame_bytes);
    auto protocol = conn->connect(std::move(target), std::move(options))->get(wait, [&] { conn->abort(); });
    return {std::move(conn), std::move(protocol)};
}

Listener::Listener(std::string address, std::uint16_t port, Options options)
    : impl_(std::make_unique<Impl>(address, port, std::move(options))) {}
Listener::~Listener() = default;
std::string Listener::url() const { return impl_->location; }
void Listener::close() noexcept { impl_->shutdown(); }
Connection Listener::accept(Wait wait) {
    std::unique_lock lock(impl_->mutex);
    auto ready = [&] { return impl_->closed || !impl_->accepted.empty(); };
    bool reached = wait.deadline == std::chrono::steady_clock::time_point::max()
        ? impl_->changed.wait(lock, wait.stop, ready)
        : impl_->changed.wait_until(lock, wait.stop, wait.deadline, ready);
    if (!reached) { wait.check(); throw DeadlineExceeded(); }
    if (impl_->closed) throw Closed();
    auto connection = std::move(impl_->accepted.front());
    impl_->accepted.pop_front();
    return connection;
}

} // namespace nightseam::duplex::ws
