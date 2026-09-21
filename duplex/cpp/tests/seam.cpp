#include <nightseam/duplex/conn.hpp>

#include <chrono>
#include <future>
#include <iostream>
#include <stdexcept>

using namespace nightseam::duplex;
using namespace std::chrono_literals;

void require(bool condition, const char* message) {
    if (!condition) throw std::runtime_error(message);
}

template<class Error, class F> void refuses(F&& operation) {
    try { operation(); } catch (const Error&) { return; }
    throw std::runtime_error("operation did not refuse with the expected error");
}

void seam() {
    auto [a, b] = pipe();
    a->send({Kind::text, "first"});
    a->send({Kind::binary, std::string("\0\xff", 2)});
    b->send({Kind::text, "reverse"});
    auto first = b->receive();
    auto second = b->receive();
    require(first.kind == Kind::text && first.data == "first", "first frame changed");
    require(second.kind == Kind::binary && second.data == std::string("\0\xff", 2), "binary frame changed");
    require(a->receive().data == "reverse", "reverse direction did not arrive");
    a->send({Kind::text, "before close"});
    a->close(4012, "finished");
    require(b->receive().data == "before close", "close discarded an earlier frame");
    try { b->receive(); throw std::runtime_error("close was not delivered"); }
    catch (const CloseError& error) {
        require(error.code == 4012 && error.reason == "finished", "close changed code or reason");
    }
    refuses<Closed>([&] { a->send({Kind::text, "late"}); });
    refuses<Closed>([&] { a->receive(); });
}

void pacing() {
    auto [a, b] = pipe(1024, 1);
    a->send({Kind::text, "one"});
    auto sending = std::async(std::launch::async, [&] { a->send({Kind::text, "two"}, Wait::after(2s)); });
    require(sending.wait_for(25ms) == std::future_status::timeout, "full transport did not pace the sender");
    require(b->receive().data == "one", "queued frame changed");
    sending.get();
    require(b->receive().data == "two", "paced frame did not arrive");
    refuses<DeadlineExceeded>([&] { b->receive(Wait::after(10ms)); });
    a->send({Kind::text, "still usable"});
    require(b->receive().data == "still usable", "a receive timeout killed the connection");
}

void cancellation_and_abort() {
    auto [a, b] = pipe(1024, 1);
    std::stop_source source;
    auto receiving = std::async(std::launch::async, [&] {
        refuses<Cancelled>([&] { b->receive(Wait{source.get_token()}); });
    });
    source.request_stop();
    receiving.get();
    a->send({Kind::text, "one"});
    auto sending = std::async(std::launch::async, [&] {
        refuses<Closed>([&] { a->send({Kind::text, "blocked"}); });
    });
    a->abort();
    require(sending.wait_for(1s) == std::future_status::ready, "abort left send blocked");
    sending.get();
    require(b->receive().data == "one", "abort discarded a previously sent frame");
    try { b->receive(); throw std::runtime_error("abort was not delivered"); }
    catch (const CloseError& error) { require(error.code == 1006, "abort was not abnormal closure"); }
}

void byte_limit() {
    auto [a, b] = pipe(3);
    a->send({Kind::text, "\xf0\x9f\x98\x80"});
    refuses<FrameTooLarge>([&] { b->receive(); });
    refuses<Closed>([&] { b->receive(); });
    refuses<CloseError>([&] { a->send({Kind::text, "late"}); });
}

int main() {
    try { seam(); pacing(); cancellation_and_abort(); byte_limit(); }
    catch (const std::exception& error) { std::cerr << error.what() << '\n'; return 1; }
}
