#pragma once

#include <nightseam/duplex/conn.hpp>
#include <nightseam/runtime/value.hpp>

namespace nightseam::conformance {

// Test-only consumer composition. All observations precede fixture teardown.
runtime::Value recorded_wire_witness(duplex::Wait wait = duplex::Wait::after(std::chrono::seconds(5)));

} // namespace nightseam::conformance
