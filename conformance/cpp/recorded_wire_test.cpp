#include "recorded_wire.hpp"
#include <fstream>
#include <iostream>
#include <iterator>

int main(int argc, char** argv) {
    try {
        if (argc != 2) throw std::runtime_error("expected shared recorded-wire scenario path");
        std::ifstream input(argv[1], std::ios::binary);
        if (!input) throw std::runtime_error("could not read shared recorded-wire scenario");
        const std::string source{std::istreambuf_iterator<char>(input), std::istreambuf_iterator<char>()};
        const auto scenario = nightseam::runtime::parse_value(source);
        const auto expected = scenario.at("steps").at(0).at("expect");
        const auto actual = nightseam::conformance::recorded_wire_witness();
        if (actual != expected) {
            std::cerr << "actual: " << nightseam::runtime::stringify(actual) << '\n';
            throw std::runtime_error("recorded Wire witness differs from shared scenario");
        }
    } catch (const std::exception& error) {
        std::cerr << error.what() << '\n'; return 1;
    }
}
