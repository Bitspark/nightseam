include(FetchContent)

# Both source archives are pinned and checked; no system-wide installation
# is needed to build a consumer or run the conformance testee.
set(BOOST_INCLUDE_LIBRARIES asio beast)
set(BOOST_BUILD_TESTS OFF CACHE BOOL "" FORCE)
FetchContent_Declare(Boost
  URL https://github.com/boostorg/boost/releases/download/boost-1.92.0/boost-1.92.0-cmake.tar.xz
  URL_HASH SHA256=9bed76128d4e46755dbe818487788c6fceb6f72b378f4daa49b7e1e600d9088d
  DOWNLOAD_EXTRACT_TIMESTAMP TRUE)

set(JSONCONS_BUILD_TESTS OFF CACHE BOOL "" FORCE)
FetchContent_Declare(jsoncons
  URL https://github.com/danielaparker/jsoncons/archive/bcb44594c50c495ee1e690602cdd71455942ad0e.tar.gz
  URL_HASH SHA256=44742915ad9fa93fa33680be56cadd77ee2c81c2740d790f538a93c084fe6f6a
  DOWNLOAD_EXTRACT_TIMESTAMP TRUE)
FetchContent_MakeAvailable(Boost jsoncons)

# The shared contract is a public immutable source dependency.
set(BITWIRE_BUILD_TESTS OFF CACHE BOOL "" FORCE)
FetchContent_Declare(Bitwire
  GIT_REPOSITORY https://github.com/Bitspark/bitwire.git
  GIT_TAG 616a2fc5e3a0972f67f40331a9d9ca102bc9698d)
FetchContent_MakeAvailable(Bitwire)
