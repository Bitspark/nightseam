cmake_minimum_required(VERSION 3.24)
project(nightseam LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

include(CTest)
include(cmake/dependencies.cmake)
find_package(Threads REQUIRED)
add_subdirectory(duplex/cpp)
add_subdirectory(runtime/cpp)
