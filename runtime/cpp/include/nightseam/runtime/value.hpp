#pragma once

#include <jsoncons/json.hpp>
#include <string>
#include <string_view>

namespace nightseam::runtime {

using Value = jsoncons::json;

// Numeric lexemes, including values outside binary64, survive parsing.
// The schema validator decides their admitted numeric domain; the profile
// must not silently round, replace or discard a payload before that check.
Value parse_value(std::string_view source);
std::string stringify(const Value& value);
std::u32string decode_utf8(std::string_view text);
void validate_unicode(const Value& value);
bool is_number(const Value& value);
bool is_text(const Value& value);

} // namespace nightseam::runtime
