#include <nightseam/duplex/wire.hpp>
#include <charconv>
#include <mutex>
#include <set>

namespace nightseam::duplex {
namespace {

bool scalar_utf8(std::string_view text) {
    for (std::size_t i = 0; i < text.size();) {
        const auto first = static_cast<unsigned char>(text[i++]);
        if (first < 0x80) continue;
        unsigned int value;
        std::size_t more;
        unsigned int minimum;
        if (first >= 0xc2 && first <= 0xdf) { value = first & 0x1f; more = 1; minimum = 0x80; }
        else if (first >= 0xe0 && first <= 0xef) { value = first & 0x0f; more = 2; minimum = 0x800; }
        else if (first >= 0xf0 && first <= 0xf4) { value = first & 0x07; more = 3; minimum = 0x10000; }
        else return false;
        if (more > text.size() - i) return false;
        while (more--) {
            const auto next = static_cast<unsigned char>(text[i++]);
            if ((next & 0xc0) != 0x80) return false;
            value = (value << 6) | (next & 0x3f);
        }
        if (value < minimum || value > 0x10ffff || (value >= 0xd800 && value <= 0xdfff)) return false;
    }
    return true;
}

Path joined(const Path& prefix, const Path& path) {
    Path result(prefix);
    result.insert(result.end(), path.begin(), path.end());
    return result;
}

class SelectedWire final : public Wire {
    WirePtr root_;
    Path prefix_;
public:
    SelectedWire(WirePtr root, Path prefix) : root_(std::move(root)), prefix_(std::move(prefix)) {}
    void send(const Path& path, const Message& message) override { root_->send(joined(prefix_, path), message); }
    Detach receive(const Path& path, Receiver receiver) override {
        auto callback = std::move(receiver.message);
        receiver.message = [count = prefix_.size(), callback = std::move(callback)](const Path& delivered, const Message& message) {
            if (callback) callback(Path(delivered.begin() + count, delivered.end()), message);
        };
        return root_->receive(joined(prefix_, path), std::move(receiver));
    }
    void close(int code, std::string reason) override { root_->close(code, std::move(reason)); }
};

class MountedWire final : public Wire, public std::enable_shared_from_this<MountedWire> {
    struct Registration {
        Receiver receiver;
        Detach detach;
        bool active = true;
    };
    std::map<std::string, WirePtr> children_;
    std::mutex mutex_;
    bool closed_ = false;
    std::set<std::shared_ptr<Registration>> registrations_;

    WirePtr destination(const Path& path) {
        if (closed_) throw Closed();
        if (path.empty()) throw NoRoute();
        encode_path(path);
        auto child = children_.find(path.front());
        if (child == children_.end() || !child->second) throw NoRoute();
        return child->second;
    }

    void remove(const std::shared_ptr<Registration>& registration, bool tell, int code = 0, const std::string& reason = {}) {
        Detach detach;
        {
            std::lock_guard lock(mutex_);
            if (!registration->active) return;
            registration->active = false;
            registrations_.erase(registration);
            detach = std::move(registration->detach);
        }
        if (detach) detach();
        if (tell && registration->receiver.closed) registration->receiver.closed(code, reason);
    }

    Detach receive_namespace(Receiver receiver) {
        struct Aggregate {
            std::mutex mutex;
            std::vector<Detach> detaches;
            bool ended = false;
            std::size_t remaining = 0;
            void stop() {
                std::vector<Detach> owned;
                {
                    std::lock_guard lock(mutex);
                    ended = true;
                    owned.swap(detaches);
                }
                for (const auto& detach : owned) detach();
            }
        };
        auto group = std::make_shared<Aggregate>();
        auto registration = std::make_shared<Registration>();
        registration->receiver = receiver;
        registration->detach = [group] { group->stop(); };
        std::vector<std::string> keys;
        {
            std::lock_guard lock(mutex_);
            if (closed_) throw Closed();
            for (const auto& [key, child] : children_) if (child) keys.push_back(key);
            group->remaining = keys.size();
            registrations_.insert(registration);
        }
        const auto weak = weak_from_this();
        try {
            for (const auto& key : keys) {
                Receiver child;
                child.namespace_ = true;
                child.message = receiver.message;
                child.closed = [group, weak, registration](int code, const std::string& reason) {
                    bool last;
                    {
                        std::lock_guard lock(group->mutex);
                        last = --group->remaining == 0;
                    }
                    if (last) if (auto self = weak.lock()) self->remove(registration, true, code, reason);
                };
                auto detach = receive({key}, std::move(child));
                bool ended;
                {
                    std::lock_guard lock(group->mutex);
                    ended = group->ended;
                    if (!ended) group->detaches.push_back(detach);
                }
                if (ended) { detach(); throw Closed(); }
            }
        } catch (...) {
            remove(registration, false);
            throw;
        }
        return [weak = weak_from_this(), held = std::weak_ptr<Registration>(registration)] {
            if (auto self = weak.lock()) if (auto registration = held.lock()) self->remove(registration, false);
        };
    }

public:
    explicit MountedWire(std::map<std::string, WirePtr> children) : children_(std::move(children)) {}
    ~MountedWire() override {
        // Dropped views cannot strand registrations in their borrowed roots.
        for (const auto& registration : registrations_) if (registration->detach) registration->detach();
    }
    void send(const Path& path, const Message& message) override {
        WirePtr child;
        {
            std::lock_guard lock(mutex_);
            child = destination(path);
        }
        child->send(Path(path.begin() + 1, path.end()), message);
    }
    Detach receive(const Path& path, Receiver receiver) override {
        if (path.empty() && receiver.namespace_) return receive_namespace(std::move(receiver));
        WirePtr child;
        auto registration = std::make_shared<Registration>();
        registration->receiver = receiver;
        {
            std::lock_guard lock(mutex_);
            child = destination(path);
            registrations_.insert(registration);
        }
        Receiver child_receiver;
        child_receiver.namespace_ = receiver.namespace_;
        child_receiver.message = [key = path.front(), callback = std::move(receiver.message)](const Path& delivered, const Message& message) {
            // The root may have captured this receiver for an admitted request;
            // detachment must not discard its later cancellation.
            if (callback) callback(joined({key}, delivered), message);
        };
        child_receiver.closed = [weak = weak_from_this(), registration](int code, const std::string& reason) {
            if (auto self = weak.lock()) self->remove(registration, true, code, reason);
        };
        Detach detach;
        try {
            detach = child->receive(Path(path.begin() + 1, path.end()), std::move(child_receiver));
        } catch (...) {
            remove(registration, false);
            throw;
        }
        bool active;
        {
            std::lock_guard lock(mutex_);
            active = registration->active;
            if (active) registration->detach = detach;
        }
        if (!active) { if (detach) detach(); throw Closed(); }
        return [weak = weak_from_this(), held = std::weak_ptr<Registration>(registration)] {
            if (auto self = weak.lock()) if (auto registration = held.lock()) self->remove(registration, false);
        };
    }
    void close(int code, std::string reason) override {
        std::set<std::shared_ptr<Registration>> owned;
        {
            std::lock_guard lock(mutex_);
            if (closed_) return;
            closed_ = true;
            owned.swap(registrations_);
            for (const auto& registration : owned) registration->active = false;
        }
        for (const auto& registration : owned) if (registration->detach) registration->detach();
        for (const auto& registration : owned) if (registration->receiver.closed) registration->receiver.closed(code, reason);
    }
};

} // namespace

std::string encode_path(const Path& path) {
    std::string encoded;
    for (const auto& segment : path) {
        if (!scalar_utf8(segment)) throw InvalidPath();
        encoded += std::to_string(segment.size()) + ':' + segment;
    }
    return encoded;
}

Path decode_path(std::string_view encoded) {
    Path path;
    while (!encoded.empty()) {
        const auto colon = encoded.find(':');
        if (colon == std::string_view::npos || colon == 0 || (colon > 1 && encoded.front() == '0')) throw InvalidPath();
        auto digits = encoded.substr(0, colon);
        for (const auto digit : digits) if (digit < '0' || digit > '9') throw InvalidPath();
        std::size_t length = 0;
        const auto parsed = std::from_chars(digits.data(), digits.data() + digits.size(), length);
        encoded.remove_prefix(colon + 1);
        if (parsed.ec != std::errc{} || length > encoded.size()) throw InvalidPath();
        auto segment = encoded.substr(0, length);
        if (!scalar_utf8(segment)) throw InvalidPath();
        path.emplace_back(segment);
        encoded.remove_prefix(length);
    }
    return path;
}

WirePtr at(WirePtr root, Path path) {
    if (!root) throw std::invalid_argument("wire selection requires an origin");
    return std::make_shared<SelectedWire>(std::move(root), std::move(path));
}

WirePtr mount(std::map<std::string, WirePtr> children) {
    return std::make_shared<MountedWire>(std::move(children));
}

} // namespace nightseam::duplex
