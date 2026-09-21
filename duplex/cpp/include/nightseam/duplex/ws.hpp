#pragma once

#include <nightseam/duplex/conn.hpp>
#include <cstdint>
#include <vector>

namespace nightseam::duplex::ws {

struct Options {
    std::size_t max_frame_bytes = 1 << 20;
    std::vector<std::string> subprotocols;
};

struct Connection {
    std::shared_ptr<Conn> conn;
    std::string subprotocol;
};

Connection dial(const std::string& url, Options options = {}, Wait wait = Wait::after(std::chrono::seconds(30)));

// A listener owns its accept loop. Closing it releases waiting accepts and
// unfinished handshakes; connections already accepted belong to their owners.
class Listener {
    struct Impl;
    std::unique_ptr<Impl> impl_;
public:
    Listener(std::string address = "127.0.0.1", std::uint16_t port = 0, Options options = {});
    ~Listener();
    Listener(const Listener&) = delete;
    Listener& operator=(const Listener&) = delete;
    std::string url() const;
    Connection accept(Wait wait = {});
    void close() noexcept;
};

} // namespace nightseam::duplex::ws
