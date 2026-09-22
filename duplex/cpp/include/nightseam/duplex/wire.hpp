#pragma once

#include <bitwire/wire.hpp>
#include <nightseam/duplex/conn.hpp>
#include <string_view>

namespace nightseam::duplex {
struct InvalidPath : std::runtime_error { InvalidPath() : std::runtime_error("invalid wire path") {} };
struct NoRoute : std::runtime_error { NoRoute() : std::runtime_error("wire path has no destination") {} };
struct ReceiverExists : std::runtime_error { ReceiverExists() : std::runtime_error("endpoint already has a receiver") {} };

std::string encode_path(const bitwire::Path& path);
bitwire::Path decode_path(std::string_view encoded);
bitwire::WirePtr at(bitwire::WirePtr root, bitwire::Path path);
std::shared_ptr<bitwire::Endpoint> mount(std::map<std::string, std::shared_ptr<bitwire::Endpoint>> children);

// One explicit routing owner above a borrowed Bitwire endpoint.
class Dispatcher : public std::enable_shared_from_this<Dispatcher> {
    struct State;
    std::shared_ptr<State> state_;
    explicit Dispatcher(std::shared_ptr<bitwire::Endpoint> root);
    void attach();
public:
    static std::shared_ptr<Dispatcher> create(std::shared_ptr<bitwire::Endpoint> root);
    ~Dispatcher();
    std::shared_ptr<bitwire::Endpoint> select(bitwire::Path prefix);
    void close(int code = 1000, std::string reason = {});
};
} // namespace nightseam::duplex
