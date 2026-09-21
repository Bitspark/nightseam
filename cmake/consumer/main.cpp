#include <nightseam/duplex/conn.hpp>
#include <nightseam/runtime/peer.hpp>
#include <iostream>

int main() {
    using namespace nightseam::runtime;
    try {
        auto [near, far] = nightseam::duplex::pipe();
        PeerOptions options;
        options.handlers["echo"] = [](const RequestContext&, Peer&, const Value& value) { return value; };
        Peer server(far, Role::server, options);
        Peer client(near, Role::client);
        auto expected = parse_value(R"({"present":null,"number":1e3,"text":"outside the checkout"})");
        if (client.call("echo", expected) != expected) throw std::runtime_error("consumer round trip changed the value");
        client.close();
        if (!server.await_close(nightseam::duplex::Wait::after(std::chrono::seconds(2))).clean)
            throw std::runtime_error("consumer close was not clean");
    } catch (const std::exception& error) {
        std::cerr << error.what() << '\n';
        return 1;
    }
}
