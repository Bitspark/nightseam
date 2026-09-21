#include <nightseam/runtime/value.hpp>

#include <fstream>
#include <iostream>
#include <limits>
#include <sstream>

using namespace nightseam::runtime;

void require(bool ok, const std::string& reason) { if (!ok) throw std::runtime_error(reason); }

template<class F> void refuses(F&& operation) {
    try { operation(); } catch (const std::exception&) { return; }
    throw std::runtime_error("invalid value was accepted");
}

int main(int argc, char** argv) {
    try {
        require(argc == 2, "provide the conformance tables directory");
        std::ifstream input(std::string(argv[1])+"/unicode.json");
        require(input.good(), "missing Unicode conformance table");
        std::stringstream bytes; bytes << input.rdbuf();
        auto table = parse_value(bytes.str());
        for (const auto& row : table.at("rows").array_range()) {
            bool valid = true;
            try { parse_value(row.at("raw").as<std::string>()); } catch (const std::exception&) { valid = false; }
            require(valid == row.at("valid").as<bool>(), "Unicode row: "+row.at("name").as<std::string>());
        }
        auto exact = parse_value(R"({"integer":9007199254740993,"exponent":1e400,"decimal":1.234567890123456789})");
        auto text = stringify(exact);
        require(text.find("9007199254740993") != std::string::npos, "large integer was rounded");
        require(text.find("1e400") != std::string::npos, "large exponent was replaced");
        require(text.find("1.234567890123456789") != std::string::npos, "decimal was rounded");
        require(is_number(exact.at("exponent")) && !is_text(exact.at("exponent")), "numeric token became a string");
        require(parse_value(R"({"same":1,"same":2})").at("same").as<int>() == 2, "duplicate payload member is not last-wins");
        for (const auto& bad : {"{} {}", "[1,]", "/* comment */null", "\xef\xbb\xbf{}"}) refuses([&] { parse_value(bad); });
        for (const auto& bad : {std::string("\xff", 1), std::string("\xc0\x80", 2), std::string("\xed\xa0\x80", 3), std::string("\xf4\x90\x80\x80", 4)}) {
            refuses([&] { stringify(Value(bad)); });
            refuses([&] { parse_value('"'+bad+'"'); });
        }
        Value object(jsoncons::json_object_arg);
        object.insert_or_assign(std::string("\xff", 1), 1);
        refuses([&] { stringify(object); });
        refuses([&] { stringify(Value(std::numeric_limits<double>::infinity())); });
        require(decode_utf8("\xf0\x9f\x98\x80").size() == 1, "emoji did not count as one code point");
    } catch (const std::exception& error) { std::cerr << error.what() << '\n'; return 1; }
}
