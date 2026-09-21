# C++ runtime

`Nightseam::runtime` provides the C++20 peer and descriptor validator. The peer
uses the transport interface from `Nightseam::duplex`; it does not require a
WebSocket to make a call. C++ enters at language-support tier 4, covering the
shared core profile. Generated bindings, tunnel and live components are
separate language lanes.

## Build and test

Use CMake 3.24 or later, Ninja and a C++20 compiler with threading and stop-token
support. The source archives for Boost and jsoncons are pinned by digest in
[`cmake/dependencies.cmake`](../../cmake/dependencies.cmake).

From the repository root:

```sh
cmake -S . -B .build/cpp -G Ninja -DCMAKE_BUILD_TYPE=Debug
cmake --build .build/cpp --parallel 2
ctest --test-dir .build/cpp --output-on-failure
```

On Windows with MinGW, put the compiler's `bin` directory first in `PATH` for
both building and running, so the compiler and test programs load the matching
runtime DLLs. The same commands work with MinGW's Ninja generator.

The private testee under [`conformance/cpp`](../../conformance/cpp) is built
by the full Go conformance runner, which fails if a required toolchain is
missing. Run `go test ./conformance/go -run 'TestSelf|TestStar' -count=1` to
exercise the shared scenarios against the Go reference in both roles.

## Use the source targets

A CMake consumer can add a pinned checkout as a subdirectory and link the
public target. Dependencies are fetched from the same checked source archives:

```cmake
add_subdirectory(vendor/nightseam)
add_executable(consumer main.cpp)
target_link_libraries(consumer PRIVATE Nightseam::runtime)
```

Component tests and the private driver default to off when Nightseam is a
subdirectory. `NIGHTSEAM_BUILD_TESTS` and `NIGHTSEAM_BUILD_CONFORMANCE` select
them explicitly. No C++ registry package is published by the core lane.

[`cmake/consumer`](../../cmake/consumer) is a small standalone consumer that
checks this default and calls a peer through the public target. It can be
configured with `-DNIGHTSEAM_SOURCE=/path/to/nightseam`; the source package
needs the root `CMakeLists.txt`, `cmake`, `duplex/cpp` and `runtime/cpp`.

Public headers are under `nightseam/runtime`. `Value`, `parse_value` and
`stringify` preserve numeric lexemes until `Schema` checks their admitted
domain. `Peer` correlates calls in either direction and provides cancellation,
deadlines, ordered events, metadata and trace propagation over a framed
connection. Its observer contains names, sizes and lifecycle facts only.
Numeric validation is independent of the host's numeric locale.

The normative behavior remains the shared [wire profile](../../docs/wire/profile.md)
and [validator tables](../../conformance/tables/validator.json).
