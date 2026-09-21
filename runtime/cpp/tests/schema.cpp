#include <nightseam/runtime/schema.hpp>
#include <nightseam/runtime/envelope.hpp>
#include <fstream>
#include <iostream>
#include <sstream>
#include <functional>
#include <clocale>

using namespace nightseam::runtime;

namespace {
int failures = 0;
int checks = 0;
std::string failure(const std::function<void()>& call) {
    try { call(); return {}; }
    catch (const std::exception& error) { return error.what(); }
}
void check(bool condition, const std::string& message) {
    ++checks;
    if (!condition) { ++failures; std::cerr << message << '\n'; }
}
Value read(const std::string& path) {
    std::ifstream input(path, std::ios::binary);
    if (!input) throw std::runtime_error("cannot read " + path);
    std::ostringstream out;
    out << input.rdbuf();
    return parse_value(out.str());
}
Value probe(const Value& pattern) {
    auto descriptor = parse_value(R"({"types":{"Probe":{"kind":"record","fields":[{"name":"text","type":"string","required":true}]}}})");
    descriptor["types"]["Probe"]["fields"][0]["pattern"] = pattern;
    return descriptor;
}
}

int main(int argc, char** argv) {
    if (argc != 2) return 2;
    const auto table = read(std::string(argv[1]) + "/validator.json");
    for (const auto& row : table.at("patterns").array_range()) {
        // A malformed descriptor is refused before any value is supplied,
        // including constraints hidden in optional and empty shapes.
        auto descriptor = probe(row.at("pattern"));
        descriptor["types"]["Probe"]["fields"][0]["required"] = false;
        auto nested = parse_value(R"({"types":{"Hidden":{"kind":"alias","type":{"array":{"nullable":null}}}}})");
        nested["types"]["Hidden"]["type"]["array"]["nullable"] = descriptor.at("types").at("Probe");
        auto error = failure([&] { Schema schema(nested); });
        check(error.empty() == row.at("valid").as<bool>(), "pattern syntax " + stringify(row) + ": " + error);
        if (!row.at("valid").as<bool>()) check(error.find("outside Nightseam dialect") != std::string::npos, error);
    }
    for (const auto& row : table.at("patternValues").array_range()) {
        auto error = failure([&] {
            Schema schema(probe(row.at("pattern")));
            Value value(jsoncons::json_object_arg);
            value["text"] = row.at("value");
            schema.validate("Probe", value);
        });
        check(error.empty() == row.at("valid").as<bool>(), "pattern value " + stringify(row) + ": " + error);
        if (!row.at("valid").as<bool>())
            check(error == "$.text: expected a match of " + row.at("pattern").as<std::string>(), error);
    }
    Schema::Imports imports;
    // The imported descriptors deliberately refer to each other. Each
    // construction receives the declarations its own lexical scope uses.
    for (const auto& name : {"peer", "wide", "implicit"})
        imports.emplace(name, std::make_shared<Schema>(table.at("imported").at(name), "", imports));
    Schema schema(table.at("wire"), "", imports);
    for (const auto& row : table.at("cases").array_range()) {
        auto bindings = row.contains("slots") ? row.at("slots") : Value(jsoncons::json_object_arg);
        auto error = failure([&] { schema.validate(row.at("expression"), row.at("value"), bindings); });
        check(error.empty() == row.at("valid").as<bool>(), "validator " + stringify(row) + ": " + error);
        if (row.contains("message")) check(error == row.at("message").as<std::string>(), "diagnostic " + stringify(row) + ": " + error);
    }
    for (const auto& row : table.at("equivalence").array_range()) {
        for (const auto& value : row.at("values").array_range()) {
            auto generic = failure([&] { schema.validate(row.at("generic"), value); });
            auto bound = failure([&] { schema.validate(row.at("bound"), value); });
            check(generic.empty() == bound.empty(), "equivalence " + stringify(row) + " value " + stringify(value));
        }
    }
    const auto unicode = read(std::string(argv[1]) + "/unicode.json");
    for (const auto& row : unicode.at("rows").array_range()) {
        auto error = failure([&] { schema.validate("json", parse_value(row.at("raw").as<std::string>())); });
        check(error.empty() == row.at("valid").as<bool>(), "Unicode " + stringify(row) + ": " + error);
    }
    for (auto source : {"9007199254740991.1", "9007199254740990.9", "1.0000000000000000001", "1e-9999", "9007199254740992", "-9007199254740992"}) {
        auto error = failure([&] { schema.validate("integer", parse_value(source)); });
        check(error == "$: expected JavaScript-safe integer", std::string("numeric precision ") + source + ": " + error);
    }
    for (auto source : {"9007199254740991", "-9007199254740991", "12300e-2", "0e9999999999999999999", "-0.0000e-9999999999999999"}) {
        auto error = failure([&] { schema.validate("integer", parse_value(source)); });
        check(error.empty(), std::string("exact integer ") + source + ": " + error);
    }
    for (auto type : {"number", "integer", "json"}) {
        auto error = failure([&] { schema.validate(type, parse_value("1e9999")); });
        check(error == std::string("$: expected finite ") + (std::string(type) == "json" ? "JSON number" : "number"), error);
    }
    for (auto source : {"1e-9999", "-1e-9999", "1e-324", "-1e-324"}) {
        check(failure([&] { schema.validate("number", parse_value(source)); }).empty(),
              std::string("finite numeric underflow ") + source);
    }
    // A host may select a decimal-comma locale; JSON remains decimal-dot.
    const std::string previous_locale = std::setlocale(LC_NUMERIC, nullptr);
    for (auto locale : {"de_DE.UTF-8", "de_DE.utf8", "German_Germany.1252", "de-DE"}) {
        if (!std::setlocale(LC_NUMERIC, locale)) continue;
        check(failure([&] { schema.validate("number", parse_value("1.5")); }).empty(), "decimal validation ignores the host locale");
        check(failure([&] { schema.validate("integer", parse_value("123.00")); }).empty(), "integer validation ignores the host locale");
        check(failure([&] { schema.validate("number", parse_value("1e-9999")); }).empty(), "underflow ignores the host locale");
        break;
    }
    std::setlocale(LC_NUMERIC, previous_locale.c_str());
    auto bound = schema.bind({{"T", "integer"}}, {{"S", imports.at("peer")}});
    check(failure([&] { bound.validate("T", 7); }).empty(), "bound type parameter");
    check(failure([&] { bound.validate("S.Envelope", parse_value(R"({"version":1})")); }).empty(), "bound family parameter");
    check(!failure([&] { schema.validate("S.Envelope", parse_value("{}")); }).empty(), "an explicit family is required");
    check(!failure([&] { schema.validate("json", Value(std::string("\xed\xa0\x80"))); }).empty(), "in-memory strings are scalar too");
    auto inline_alias = parse_value(R"({"kind":"alias","type":"integer"})");
    for (int i = 0; i < 12; ++i) {
        auto next = parse_value(R"({"kind":"alias"})");
        next["type"] = inline_alias;
        inline_alias = std::move(next);
    }
    check(failure([&] { schema.validate(inline_alias, 7); }).empty(), "inline aliases retain distinct finite syntax");
    auto recursive = Schema(parse_value(R"({"types":{
      "Node":{"kind":"record","fields":[{"name":"next","type":{"nullable":"Node"}}]},
      "Cycle":{"kind":"alias","type":"Cycle"},
      "Inherited":{"kind":"record","extends":["Inherited"]}
    }})"));
    check(failure([&] { recursive.validate("Node", parse_value(R"({"next":{"next":null}})")); }).empty(), "record recursion progresses through values");
    check(failure([&] { recursive.validate("Cycle", 1); }) == "$: expected acyclic type expression", "alias cycles are refused");
    check(failure([&] { recursive.validate("Inherited", parse_value("{}")); }) == "$: expected acyclic inheritance", "inheritance cycles are refused");
    auto dotted = schema.bind({{"S.Envelope", "Base"}});
    dotted = dotted.bind({{"S.Handle", "integer"}});
    check(failure([&] { dotted.validate("S.Envelope", parse_value(R"({"text":"ok"})")); }).empty(), "drawn members survive a second bind");
    check(failure([&] { dotted.validate(parse_value(R"({"apply":"peer.Box","with":{"S":"S"}})"), parse_value(R"({"item":{"text":"ok"}})")); }).empty(), "drawn families are usable as application arguments");
    check(failure([&] { dotted.validate(parse_value(R"({"apply":"S.Envelope","with":{}})"), parse_value(R"({"text":"ok"})")); }).empty(), "an application of a drawn type keeps its lexical scope");
    // Count saturation retains both zero-width and consuming semantics.
    for (auto pattern : {"^(?:){999999999999999999999999}$", "^(a?){999999999999999999999999}$"}) {
        Schema repeat(probe(Value(pattern)));
        check(failure([&] { repeat.validate("Probe", parse_value(R"({"text":""})")); }).empty(), "huge nullable repetition reaches its fixed point");
    }
    Schema long_repeat(probe(Value("^a{1001}$")));
    auto long_text = Value(jsoncons::json_object_arg);
    long_text["text"] = std::string(1001, 'a');
    check(failure([&] { long_repeat.validate("Probe", long_text); }).empty(), "counts beyond RE2's ceiling match without expansion");
    const std::string digest(64, 'a'), other_digest(64, 'b');
    auto declared = std::make_shared<Schema>(parse_value(R"({"types":{"Report":{"kind":"callable","contract":"source/Report"}}})"), digest);
    Schema consumer(parse_value(R"({"types":{}})"), other_digest, {{"source", declared}});
    auto reference = parse_value(R"({"binding":"nonce.1","contract":"source/Report"})");
    reference["digest"] = digest;
    check(failure([&] { consumer.validate("source.Report", reference); }).empty(), "imported callable uses its own digest");
    auto supplied = consumer.bind({}, {{"S", declared}});
    check(failure([&] { supplied.validate("S.Report", reference); }).empty(), "family binding keeps its declaration digest");
    reference["digest"] = other_digest;
    bool mismatch = false;
    try { consumer.validate("source.Report", reference); }
    catch (const PublicError& error) { mismatch = error.code == "contract_mismatch"; }
    check(mismatch, "callable digest mismatch is a public contract_mismatch");
    for (const auto& invalid_digest : {std::string("short"), std::string(64, 'A'), std::string(64, 'g'), std::string(63, 'a'), std::string(65, 'a')}) {
        check(failure([&] { Schema invalid(parse_value(R"({"types":{}})"), invalid_digest); }) ==
              "schema.digest: expected empty or lowercase SHA-256 digest", "schema digest syntax");
    }
    std::cout << checks << " schema checks, " << failures << " failures\n";
    return failures == 0 ? 0 : 1;
}
