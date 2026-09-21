#pragma once
#include <string_view>

namespace nightseam::runtime::detail {
// Nightseam's ECMAScript Unicode subset, shared with internal/pattern in
// the Go reference. Counts are never expanded into repeated syntax nodes.
void check_pattern(std::string_view source);
bool match_pattern(std::string_view source, std::string_view value);
}
