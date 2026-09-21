#include "driver.hpp"
#include <iostream>

using namespace nightseam::conformance;

void require(bool condition, const std::string& reason) {
    if (!condition) throw std::runtime_error(reason);
}

int main() {
    try {
        Arguments request(R"({"id":1,"op":"peer.call","params": { "x": 1e3, "x":1.2300, "escaped":"},:\"[]" },"behavior":{"kind":"return","value": [ 1e400, { "a": 2 } ]}})");
        require(request.raw("params") == R"({ "x": 1e3, "x":1.2300, "escaped":"},:\"[]" })",
                "the driver rewrote original payload bytes");
        Arguments behavior(request.raw("behavior"));
        require(behavior.raw("value") == "[ 1e400, { \"a\": 2 } ]", "nested canned payload bytes changed");
        require(request.raw("absent") == "null" && request.raw("absent", "{}") == "{}", "absent payload defaults changed");
        Arguments duplicate(R"({"params":1,"params": 2e3})");
        require(duplicate.raw("params") == "2e3", "raw field extraction did not follow last-wins JSON parsing");
        std::string bytes;
        for (unsigned i = 0; i < 256; ++i) bytes += static_cast<char>(i);
        for (std::size_t i = 0; i <= bytes.size(); ++i)
            require(base64_decode(base64_encode(bytes.substr(0, i))) == bytes.substr(0, i), "base64 lost binary bytes");
        Inbox<int> inbox;
        inbox.put(1); inbox.put(2); inbox.close();
        require(inbox.take(Wait::after(std::chrono::seconds(1)), [](int n) { return n == 2; }) == 2,
                "a filtered read lost a held item at close");
        require(inbox.take(Wait::after(std::chrono::seconds(1)), [](int) { return true; }) == 1,
                "a filtered read reordered unmatched items");
    } catch (const std::exception& e) { std::cerr << e.what() << '\n'; return 1; }
}
