#include <nightseam/duplex/ws.hpp>
#include <future>
#include <iostream>

using namespace nightseam::duplex;
using namespace std::chrono_literals;

void require(bool ok, const char* reason) { if (!ok) throw std::runtime_error(reason); }

void exchange() {
    ws::Listener listener("127.0.0.1", 0, {.subprotocols = {"one", "two"}});
    auto client = ws::dial(listener.url(), {.subprotocols = {"two"}});
    auto server = listener.accept(Wait::after(2s));
    require(client.subprotocol == "two" && server.subprotocol == "two", "subprotocol negotiation changed");
    client.conn->send({Kind::text, "first"});
    client.conn->send({Kind::binary, std::string("\0\xff", 2)});
    auto first = server.conn->receive(Wait::after(2s));
    auto binary = server.conn->receive(Wait::after(2s));
    require(first.kind == Kind::text && first.data == "first", "text frame changed");
    require(binary.kind == Kind::binary && binary.data == std::string("\0\xff", 2), "binary frame changed");
    server.conn->send({Kind::text, "reverse"});
    require(client.conn->receive(Wait::after(2s)).data == "reverse", "reverse frame changed");
    auto closed = std::async(std::launch::async, [&] {
        try { server.conn->receive(Wait::after(2s)); throw std::runtime_error("close was not delivered"); }
        catch (const CloseError& error) { require(error.code == 4012 && error.reason == "finished", "close reason changed"); }
    });
    client.conn->close(4012, "finished", Wait::after(2s));
    closed.get();

    // The listener still accepts a second connection and negotiates nothing
    // when that client offers nothing, despite the server's advertised list.
    auto plain = ws::dial(listener.url());
    auto accepted = listener.accept(Wait::after(2s));
    require(plain.subprotocol.empty() && accepted.subprotocol.empty(), "default subprotocol was invented");
    plain.conn->abort();
    try { accepted.conn->receive(Wait::after(2s)); throw std::runtime_error("abort did not end the remote"); }
    catch (const CloseError& error) { require(error.code == 1006, "abort was not abnormal closure"); }
}

void limit_and_shutdown() {
    ws::Listener listener("127.0.0.1", 0, {.max_frame_bytes = 3});
    auto client = ws::dial(listener.url());
    auto server = listener.accept(Wait::after(2s));
    client.conn->send({Kind::text, "\xf0\x9f\x98\x80"});
    auto ack = std::async(std::launch::async, [&] { try { client.conn->receive(Wait::after(2s)); } catch (...) {} });
    try { server.conn->receive(Wait::after(2s)); throw std::runtime_error("oversized UTF-8 frame arrived"); }
    catch (const FrameTooLarge&) {}
    ack.get();
    auto accepting = std::async(std::launch::async, [&] {
        try { listener.accept(); throw std::runtime_error("closed listener accepted"); }
        catch (const Closed&) {}
    });
    listener.close();
    require(accepting.wait_for(1s) == std::future_status::ready, "listener close left accept waiting");
    accepting.get();
}

int main() {
    try { exchange(); limit_and_shutdown(); }
    catch (const std::exception& error) { std::cerr << error.what() << '\n'; return 1; }
}
