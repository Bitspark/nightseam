#include <nightseam/runtime/envelope.hpp>
#include <fstream>
#include <iostream>
#include <sstream>
#include <stdexcept>
using namespace nightseam::runtime;
int main(int argc, char** argv) {
    try {
        if (argc != 2) throw std::runtime_error("expected tables directory");
        std::ifstream input(std::string(argv[1]) + "/frames.json");
        std::stringstream text; text << input.rdbuf();
        auto table = parse_value(text.str());
        for (const auto& row : table.at("rows").array_range()) {
            bool accepted = false;
            try {
                auto role = row.at("to").as<std::string>() == "client" ? Role::client : Role::server;
                auto frame = decode_envelope(row.at("frame").as<std::string>(), role);
                decode_envelope(encode_envelope(frame), role);
                accepted = true;
            } catch (const std::exception&) {}
            if (accepted != row.at("valid").as<bool>()) throw std::runtime_error(row.at("name").as<std::string>());
        }
        Path path{"", "a.b/c", "\xf0\x9f\x98\x80"};
        if (decode_path(encode_path(path)) != path) throw std::runtime_error("path round trip");
        for (auto invalid : {"01:x", "1:", "-1:x", "1:xjunk", "99999999999999999999999999:x"}) {
            bool rejected = false;
            try { decode_path(invalid); } catch (...) { rejected = true; }
            if (!rejected) throw std::runtime_error("noncanonical path accepted");
        }
        std::cout << "all envelope table rows and canonical paths passed\n";
    } catch (const std::exception& e) { std::cerr << e.what() << '\n'; return 1; }
}
