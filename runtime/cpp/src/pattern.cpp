#include "pattern.hpp"
#include <nightseam/runtime/value.hpp>
#include <algorithm>
#include <cstdint>
#include <limits>
#include <iterator>
#include <map>
#include <memory>
#include <stdexcept>
#include <utility>
#include <vector>

namespace nightseam::runtime::detail {
namespace {
using Span = std::pair<char32_t, char32_t>;
using Characters = std::vector<Span>;
using Count = std::uint64_t;
constexpr Count limit = std::numeric_limits<Count>::max();

Characters normalized(Characters set) {
    std::sort(set.begin(), set.end());
    Characters result;
    for (auto span : set) {
        if (!result.empty() && span.first <= result.back().second + 1)
            result.back().second = std::max(result.back().second, span.second);
        else result.push_back(span);
    }
    return result;
}
Characters complement(Characters set) {
    Characters result;
    char32_t next = 0;
    for (auto [lo, hi] : normalized(std::move(set))) {
        if (next < lo) result.emplace_back(next, lo - 1);
        next = hi + 1;
    }
    if (next <= 0x10ffff) result.emplace_back(next, 0x10ffff);
    return result;
}
Characters character(char32_t value) { return {{value, value}}; }
const Characters digits{{U'0', U'9'}};
const Characters words{{U'0', U'9'}, {U'A', U'Z'}, {U'_', U'_'}, {U'a', U'z'}};
const Characters whitespace{{0x9, 0xd}, {0x20, 0x20}, {0xa0, 0xa0}, {0x1680, 0x1680},
    {0x2000, 0x200a}, {0x2028, 0x2029}, {0x202f, 0x202f}, {0x205f, 0x205f},
    {0x3000, 0x3000}, {0xfeff, 0xfeff}};

struct Node {
    char kind;
    Characters set;
    std::vector<std::shared_ptr<Node>> children;
    Count min = 0, max = 0, width = 0;
    bool unlimited = false, nullable = false;
    explicit Node(char kind) : kind(kind) {}
};
using Tree = std::shared_ptr<Node>;
Tree set_node(Characters set) {
    auto node = std::make_shared<Node>('c');
    node->set = normalized(std::move(set));
    node->width = 1;
    return node;
}
Tree join(char kind, std::vector<Tree> children) {
    if (children.size() == 1) return children.front();
    auto node = std::make_shared<Node>(kind);
    node->children = std::move(children);
    if (kind == '|') node->width = limit;
    node->nullable = kind != '|';
    for (const auto& child : node->children) {
        if (kind == '|') node->width = std::min(node->width, child->width);
        else node->width = node->width > limit - child->width ? limit : node->width + child->width;
        node->nullable = kind == '|' ? node->nullable || child->nullable : node->nullable && child->nullable;
    }
    return node;
}
Tree repeat(Tree child, Count min, Count max, bool unlimited) {
    auto node = std::make_shared<Node>('r');
    node->width = min != 0 && child->width > limit / min ? limit : child->width * min;
    node->nullable = min == 0 || child->nullable;
    node->children.push_back(std::move(child));
    node->min = min;
    node->max = max;
    node->unlimited = unlimited;
    return node;
}
[[noreturn]] void invalid() { throw std::invalid_argument("outside Nightseam dialect"); }
bool decimal_digit(char32_t c) { return c >= U'0' && c <= U'9'; }
bool hex_digit(char32_t c) { return decimal_digit(c) || (c >= U'a' && c <= U'f') || (c >= U'A' && c <= U'F'); }
unsigned hex_value(char32_t c) { return decimal_digit(c) ? c - U'0' : (c >= U'a' ? c - U'a' : c - U'A') + 10; }
std::u32string decimal(std::u32string text) {
    auto at = text.find_first_not_of(U'0');
    return at == std::u32string::npos ? U"0" : text.substr(at);
}
Count saturated(const std::u32string& text) {
    Count result = 0;
    for (auto c : text) {
        auto digit = c - U'0';
        if (result > (limit - digit) / 10) return limit;
        result = result * 10 + digit;
    }
    return result;
}

struct Parser {
    std::u32string source;
    std::size_t pos = 0;
    char32_t peek() const { return pos < source.size() ? source[pos] : U'\0'; }
    bool starts(std::u32string_view value) const { return std::u32string_view(source).substr(pos).starts_with(value); }
    void outside() const {
        for (auto prefix : {U"(?P<", U"(?P=", U"(?<=", U"(?<!", U"(?<", U"(?=", U"(?!", U"[[:",
                            U"\\k<", U"\\p{", U"\\P{", U"\\A", U"\\z", U"\\Z", U"\\Q", U"\\C"})
            if (starts(prefix)) invalid();
    }
    std::u32string count() {
        auto start = pos;
        while (pos < source.size() && decimal_digit(source[pos])) ++pos;
        if (pos == start) invalid();
        return decimal(source.substr(start, pos - start));
    }
    Tree disjunction(bool group = false) {
        std::vector<Tree> branches, sequence;
        auto finish = [&] {
            branches.push_back(join('s', std::move(sequence)));
            return join('|', std::move(branches));
        };
        while (pos < source.size()) {
            if (peek() == ')') {
                if (!group) invalid();
                ++pos;
                return finish();
            }
            if (peek() == '|') {
                ++pos;
                branches.push_back(join('s', std::move(sequence)));
                sequence.clear();
                continue;
            }
            auto [node, assertion] = atom();
            if (pos == source.size()) { sequence.push_back(std::move(node)); continue; }
            Count min = 0, max = 1;
            bool unlimited = false;
            if (peek() == '*' || peek() == '+' || peek() == '?') {
                unlimited = peek() != '?';
                min = peek() == '+' ? 1 : 0;
                ++pos;
            } else if (peek() == '{') {
                ++pos;
                auto lower = count();
                auto upper = lower;
                if (peek() == ',') {
                    ++pos;
                    unlimited = true;
                    if (pos < source.size() && decimal_digit(peek())) { upper = count(); unlimited = false; }
                }
                if (peek() != '}' || pos == source.size()) invalid();
                ++pos;
                if (!unlimited && (upper.size() < lower.size() || (upper.size() == lower.size() && upper < lower))) invalid();
                min = saturated(lower);
                max = saturated(upper);
            } else { sequence.push_back(std::move(node)); continue; }
            if (assertion) invalid();
            if (pos < source.size() && peek() == '?') ++pos;
            sequence.push_back(repeat(std::move(node), min, max, unlimited));
        }
        if (group) invalid();
        return finish();
    }
    std::pair<Tree, bool> atom() {
        outside();
        auto c = source[pos++];
        switch (c) {
        case '^': case '$': return {std::make_shared<Node>(static_cast<char>(c)), true};
        case '.': return {set_node(complement({{10, 10}, {13, 13}, {0x2028, 0x2029}})), false};
        case '(':
            if (pos < source.size() && peek() == '?') {
                if (!starts(U"?:")) invalid();
                pos += 2;
            }
            return {disjunction(true), false};
        case '[': return {set_node(character_class()), false};
        case '\\': {
            auto [set, assertion] = escape(false);
            if (assertion != 0) return {std::make_shared<Node>(assertion), true};
            return {set_node(std::move(set)), false};
        }
        case '*': case '+': case '?': case '{': case '}': case ']': invalid();
        default: return {set_node(character(c)), false};
        }
    }
    Characters character_class() {
        bool negated = peek() == '^';
        if (negated) ++pos;
        Characters result;
        while (pos < source.size() && peek() != ']') {
            auto left = class_atom();
            if (pos + 1 < source.size() && peek() == '-' && source[pos + 1] != ']') {
                ++pos;
                auto right = class_atom();
                if (left.size() != 1 || right.size() != 1 || left[0].first != left[0].second ||
                    right[0].first != right[0].second || left[0].first > right[0].first) invalid();
                result.emplace_back(left[0].first, right[0].first);
            } else result.insert(result.end(), left.begin(), left.end());
        }
        if (pos == source.size()) invalid();
        ++pos;
        return negated ? complement(std::move(result)) : result;
    }
    Characters class_atom() {
        auto c = source[pos++];
        return c == '\\' ? escape(true).first : character(c);
    }
    char32_t hex(unsigned count) {
        char32_t value = 0;
        for (unsigned i = 0; i < count; ++i) {
            if (pos == source.size() || !hex_digit(peek())) invalid();
            value = value * 16 + hex_value(source[pos++]);
        }
        return value;
    }
    std::pair<Characters, char> escape(bool in_class) {
        if (pos == source.size()) invalid();
        --pos; outside(); ++pos;
        auto c = source[pos++];
        switch (c) {
        case 'd': return {digits, 0};
        case 'D': return {complement(digits), 0};
        case 'w': return {words, 0};
        case 'W': return {complement(words), 0};
        case 's': return {whitespace, 0};
        case 'S': return {complement(whitespace), 0};
        case 'b': return in_class ? std::pair{character(8), char(0)} : std::pair{Characters{}, 'b'};
        case 'B': if (!in_class) return {{}, 'B'}; break;
        case 'f': return {character(12), 0};
        case 'n': return {character(10), 0};
        case 'r': return {character(13), 0};
        case 't': return {character(9), 0};
        case 'v': return {character(11), 0};
        case '0':
            if (pos < source.size() && decimal_digit(peek())) invalid();
            return {character(0), 0};
        case 'c':
            if (pos < source.size() && ((peek() >= 'A' && peek() <= 'Z') || (peek() >= 'a' && peek() <= 'z')))
                return {character(source[pos++] & 31), 0};
            invalid();
        case 'x': return {character(hex(2)), 0};
        case 'u': {
            if (pos < source.size() && peek() == '{') {
                ++pos;
                auto start = pos;
                char32_t value = 0;
                while (pos < source.size() && hex_digit(peek())) {
                    auto digit = hex_value(source[pos++]);
                    if (value > (0x10ffff - digit) / 16) invalid();
                    value = value * 16 + digit;
                }
                if (pos == start || pos == source.size() || peek() != '}') invalid();
                ++pos;
                return {character(value), 0};
            }
            auto value = hex(4);
            if (value >= 0xd800 && value <= 0xdbff && starts(U"\\u")) {
                auto end = pos;
                pos += 2;
                try {
                    auto trail = hex(4);
                    if (trail >= 0xdc00 && trail <= 0xdfff) value = 0x10000 + (value - 0xd800) * 1024 + trail - 0xdc00;
                    else pos = end;
                } catch (const std::invalid_argument&) { pos = end; }
            }
            return {character(value), 0};
        }
        default:
            if (std::u32string_view(U"^$\\.*+?()[]{}|/").find(c) != std::u32string_view::npos || (in_class && c == '-'))
                return {character(c), 0};
        }
        invalid();
    }
};

// Thompson states merge every candidate start while scanning once. Neither
// backtracking paths nor the endpoints of each possible start are retained.
// A compilation budget chooses an engine, never whether a pattern is valid.
// The direct matcher below handles counted trees whose compact program would
// require more states, without expanding an untrusted decimal count.
struct Nfa {
    using Index = std::size_t;
    struct Instruction {
        char kind;
        const Characters* set = nullptr;
        Index next = 0, alternative = 0;
    };
    static constexpr std::size_t budget = 4096;
    std::vector<Instruction> code;
    std::size_t length;

    struct Repetition { Count min, max; bool unlimited; };
    Repetition repetition(const Tree& node) const {
        const auto& child = node->children.front();
        // If the child always admits epsilon, empty iterations can fill any
        // lower bound. A maximum >= the input's possible consuming iterations
        // is irrelevant for this input, including when nullable is conditional.
        auto minimum = child->nullable ? 0 : node->min;
        auto consuming = child->width > 0 ? length / child->width : length;
        return {minimum, node->max, node->unlimited || node->max >= consuming};
    }
    static std::size_t add(std::size_t a, std::size_t b) {
        return a > budget || b > budget - a ? budget + 1 : a + b;
    }
    static std::size_t multiply(Count count, std::size_t size) {
        return size != 0 && count > budget / size ? budget + 1 : static_cast<std::size_t>(count) * size;
    }
    std::size_t size(const Tree& node) const {
        if (node->width > length) return 1;
        if (node->kind == 'r') {
            auto repeat = repetition(node);
            auto child = size(node->children.front());
            if (repeat.unlimited) return add(add(multiply(repeat.min, child), child), 1);
            return add(multiply(repeat.max, child), multiply(repeat.max - repeat.min, 1));
        }
        if (node->kind == 's' || node->kind == '|') {
            std::size_t result = node->kind == '|' && !node->children.empty() ? node->children.size() - 1 : 0;
            for (const auto& child : node->children) result = add(result, size(child));
            return result;
        }
        return 1;
    }
    Index emit(Instruction instruction) { code.push_back(instruction); return code.size() - 1; }
    Index compile(const Tree& node, Index next) {
        if (node->width > length) return emit({'!'});
        if (node->kind == 's') {
            for (auto child = node->children.rbegin(); child != node->children.rend(); ++child) next = compile(*child, next);
            return next;
        }
        if (node->kind == '|') {
            if (node->children.empty()) return emit({'!'});
            auto start = compile(node->children.front(), next);
            for (std::size_t i = 1; i < node->children.size(); ++i) start = emit({'|', nullptr, start, compile(node->children[i], next)});
            return start;
        }
        if (node->kind == 'r') {
            auto repeat = repetition(node);
            const auto& child = node->children.front();
            if (repeat.unlimited) {
                auto loop = emit({'|', nullptr, 0, next});
                auto body = compile(child, loop);
                code[loop].next = body;
                next = loop;
            } else {
                for (Count i = repeat.min; i < repeat.max; ++i) next = emit({'|', nullptr, compile(child, next), next});
            }
            for (Count i = 0; i < repeat.min; ++i) next = compile(child, next);
            return next;
        }
        return emit({node->kind, node->kind == 'c' ? &node->set : nullptr, next});
    }
    bool matches(const Tree& tree, const std::u32string& input) {
        auto start = compile(tree, emit({'m'}));
        std::vector<Index> next, pending, active;
        std::vector<std::size_t> visited(code.size(), std::numeric_limits<std::size_t>::max());
        auto word = [&](std::size_t at) {
            if (at >= input.size()) return false;
            auto c = input[at];
            return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || c == '_' || (c >= 'a' && c <= 'z');
        };
        for (std::size_t at = 0; at <= input.size(); ++at) {
            pending.swap(next);
            next.clear();
            pending.push_back(start);
            active.clear();
            while (!pending.empty()) {
                auto pc = pending.back();
                pending.pop_back();
                if (visited[pc] == at) continue;
                visited[pc] = at;
                const auto& instruction = code[pc];
                switch (instruction.kind) {
                case 'm': return true;
                case 'c': active.push_back(pc); break;
                case '|': pending.push_back(instruction.next); pending.push_back(instruction.alternative); break;
                case '^': if (at == 0) pending.push_back(instruction.next); break;
                case '$': if (at == input.size()) pending.push_back(instruction.next); break;
                case 'b': case 'B':
                    if (((at > 0 && word(at - 1)) != word(at)) == (instruction.kind == 'b')) pending.push_back(instruction.next);
                    break;
                }
            }
            if (at == input.size()) break;
            for (auto pc : active) {
                const auto& instruction = code[pc];
                for (auto [lo, hi] : *instruction.set) {
                    if (input[at] >= lo && input[at] <= hi) { next.push_back(instruction.next); break; }
                }
            }
        }
        return false;
    }
};

using Positions = std::vector<std::size_t>;
void merge_positions(Positions& positions, const Positions& other) {
    Positions merged;
    std::set_union(positions.begin(), positions.end(), other.begin(), other.end(), std::back_inserter(merged));
    positions = std::move(merged);
}
struct Matcher {
    std::u32string input;
    std::map<std::pair<const Node*, std::size_t>, Positions> memo;
    Positions advance(const Tree& child, const Positions& positions) {
        Positions result;
        for (auto start : positions) {
            auto next = ends(child, start);
            merge_positions(result, next);
        }
        return result;
    }
    Positions ends(const Tree& node, std::size_t start) {
        if (node->width > input.size() - start) return {};
        auto key = std::pair{node.get(), start};
        if (auto found = memo.find(key); found != memo.end()) return found->second;
        Positions result;
        switch (node->kind) {
        case 'c':
            if (start < input.size()) {
                for (auto [lo, hi] : node->set) {
                    if (input[start] >= lo && input[start] <= hi) { result.push_back(start + 1); break; }
                }
            }
            break;
        case '^': if (start == 0) result.push_back(start); break;
        case '$': if (start == input.size()) result.push_back(start); break;
        case 'b': case 'B': {
            auto word = [&](std::size_t at) {
                if (at >= input.size()) return false;
                auto c = input[at];
                return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || c == '_' || (c >= 'a' && c <= 'z');
            };
            if (((start > 0 && word(start - 1)) != word(start)) == (node->kind == 'b')) result.push_back(start);
            break;
        }
        case 's':
            result.push_back(start);
            for (const auto& child : node->children) { result = advance(child, result); if (result.empty()) break; }
            break;
        case '|':
            for (const auto& child : node->children) {
                auto next = ends(child, start);
                merge_positions(result, next);
            }
            break;
        case 'r': {
            Positions current{start};
            Count count = 0;
            while (count < node->min && !current.empty()) {
                auto next = advance(node->children.front(), current);
                ++count;
                if (next == current) { count = node->min; current = std::move(next); break; }
                current = std::move(next);
            }
            if (!current.empty()) {
                result = current;
                while (node->unlimited || count < node->max) {
                    auto next = advance(node->children.front(), current);
                    if (next.empty() || next == current) break;
                    merge_positions(result, next);
                    current = std::move(next);
                    ++count;
                }
            }
            break;
        }
        }
        // Cache only a constant-sized answer per node/position. Retaining a
        // variable-sized endpoint set for every start takes quadratic memory.
        // Every intermediate union above also stays a set of input positions.
        if (result.size() <= 1) memo.emplace(key, result);
        return result;
    }
};
}

void check_pattern(std::string_view source) { Parser{decode_utf8(source)}.disjunction(); }
bool match_pattern(std::string_view source, std::string_view value) {
    auto tree = Parser{decode_utf8(source)}.disjunction();
    auto input = decode_utf8(value);
    if (tree->width > input.size()) return false;
    Nfa nfa{{}, input.size()};
    if (nfa.size(tree) < Nfa::budget) return nfa.matches(tree, input);
    Matcher matcher{std::move(input), {}};
    for (std::size_t start = 0; start <= matcher.input.size(); ++start)
        if (!matcher.ends(tree, start).empty()) return true;
    return false;
}
} // namespace nightseam::runtime::detail
