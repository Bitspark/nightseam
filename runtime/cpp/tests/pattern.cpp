#include "../src/pattern.hpp"
#include <algorithm>
#include <chrono>
#include <cstddef>
#include <cstdlib>
#include <iostream>
#include <new>
#include <set>
#include <string>

// Count retained allocations, not cumulative allocation traffic. A regression
// fails with bad_alloc inside the test's budget rather than exhausting its host.
namespace allocation_probe {
struct alignas(std::max_align_t) Header { std::size_t size; bool tracked; };
bool enabled = false;
std::size_t held = 0, peak = 0;
constexpr std::size_t budget = 16 * 1024 * 1024;
void* allocate(std::size_t size) {
    if (enabled && size > budget - held) throw std::bad_alloc();
    auto block = static_cast<Header*>(std::malloc(sizeof(Header) + size));
    if (!block) throw std::bad_alloc();
    block->size = size;
    block->tracked = enabled;
    if (enabled) { held += size; peak = std::max(held, peak); }
    return block + 1;
}
void release(void* pointer) noexcept {
    if (!pointer) return;
    auto block = static_cast<Header*>(pointer) - 1;
    if (block->tracked) held -= block->size;
    std::free(block);
}
}
void* operator new(std::size_t size) { return allocation_probe::allocate(size); }
void* operator new[](std::size_t size) { return allocation_probe::allocate(size); }
void operator delete(void* pointer) noexcept { allocation_probe::release(pointer); }
void operator delete[](void* pointer) noexcept { allocation_probe::release(pointer); }
void operator delete(void* pointer, std::size_t) noexcept { allocation_probe::release(pointer); }
void operator delete[](void* pointer, std::size_t) noexcept { allocation_probe::release(pointer); }

using nightseam::runtime::detail::match_pattern;
int main() {
    int failures = 0;
    auto check = [&](bool okay, const std::string& message) {
        if (!okay) { ++failures; std::cerr << message << '\n'; }
    };
    const std::string unmatched(20000, 'a');
    for (auto pattern : {"a*b", "(?:a+)+b", "(?:a?)*b", "a{0,999999999999999999999999}b"}) {
        bool matched = false, exceeded = false;
        allocation_probe::peak = 0;
        allocation_probe::enabled = true;
        auto before = std::chrono::steady_clock::now();
        try { matched = match_pattern(pattern, unmatched); }
        catch (const std::bad_alloc&) { exceeded = true; }
        allocation_probe::enabled = false;
        auto elapsed = std::chrono::steady_clock::now() - before;
        check(!exceeded, std::string(pattern) + " exceeded the 16 MiB retained-memory budget");
        check(!matched, std::string(pattern) + " matched an input without b");
        check(elapsed < std::chrono::seconds(8), std::string(pattern) + " exceeded the bounded ordinary-match time");
        check(allocation_probe::held == 0, "matching retained allocations after returning");
        std::cout << pattern << ": peak " << allocation_probe::peak << " bytes\n";
    }
    if (failures != 0) return 1;
    check(match_pattern("a*b", unmatched + 'b'), "streaming search lost its final match");
    check(match_pattern("^(?:a?)*$", unmatched), "nullable loop lost its consuming match");
    check(match_pattern("^(?:a?){999999999999999999999999}$", unmatched), "huge nullable lower bound changed meaning");
    check(match_pattern("^a{5000}$", std::string(5000, 'a')), "large exact count was truncated");
    check(!match_pattern("^a{5000}$", std::string(4999, 'a')), "large exact count accepted too few characters");
    check(!match_pattern("^a{2,4}$", "aaaaa"), "bounded maximum was discarded");
    check(match_pattern("^(?:^|a){999999999999999999999999}$", "aaa"), "conditional empty repetitions changed meaning");
    check(!match_pattern("^(?:b|$){999999999999999999999999}$", "aaa"), "conditional empty repetition bypassed a mismatch");
    check(match_pattern("^\\bword\\b$", "word"), "word-boundary assertions lost position");
    check(!match_pattern("a$", "a\n"), "end assertion accepted a trailing line terminator");
    // Exhaustively hold bounded alternation to its finite language, so input-
    // dependent count simplification cannot admit too few or too many copies.
    std::set<std::string> language, generation{""};
    for (int copies = 1; copies <= 4; ++copies) {
        std::set<std::string> next;
        for (const auto& prefix : generation) {
            next.insert(prefix + "a");
            next.insert(prefix + "bb");
        }
        generation = std::move(next);
        if (copies >= 2) language.insert(generation.begin(), generation.end());
    }
    for (unsigned length = 0; length <= 8; ++length) {
        for (unsigned bits = 0; bits < (1u << length); ++bits) {
            std::string value(length, 'a');
            for (unsigned i = 0; i < length; ++i) if ((bits >> i) & 1u) value[i] = 'b';
            check(match_pattern("^(?:a|bb){2,4}$", value) == language.contains(value), "bounded language differs on " + value);
            check(match_pattern("^(?:a|b)?(?:a|b){0,3}$", value) == (length <= 4), "optional bounded language differs on " + value);
        }
    }
    return failures == 0 ? 0 : 1;
}
