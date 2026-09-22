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

bitwire::Path joined(const bitwire::Path& prefix, const bitwire::Path& path) {
    bitwire::Path result(prefix);
    result.insert(result.end(), path.begin(), path.end());
    return result;
}

class SelectedWire final : public bitwire::Wire {
    bitwire::WirePtr root_;
    bitwire::Path prefix_;
public:
    SelectedWire(bitwire::WirePtr root, bitwire::Path prefix) : root_(std::move(root)), prefix_(std::move(prefix)) {}
    void send(const bitwire::Path& path, const bitwire::Message& message) override { root_->send(joined(prefix_, path), message); }
};

class MountedWire final : public bitwire::Endpoint, public std::enable_shared_from_this<MountedWire> {
    struct Attachment {
        bitwire::Receiver receiver;
        std::vector<bitwire::Detach> detaches;
        std::size_t remaining;
    };
    std::map<std::string, std::shared_ptr<bitwire::Endpoint>> children_;
    std::mutex mutex_;
    bool closed_ = false;
    std::shared_ptr<Attachment> attachment_;
    void detach(const std::shared_ptr<Attachment>& held) {
        std::vector<bitwire::Detach> stops;
        { std::lock_guard lock(mutex_); if (attachment_ != held) return;
          attachment_.reset(); stops.swap(held->detaches); }
        for (auto& stop : stops) stop();
    }
public:
    explicit MountedWire(std::map<std::string, std::shared_ptr<bitwire::Endpoint>> children) : children_(std::move(children)) {}
    ~MountedWire() override { if (attachment_) for (auto& stop : attachment_->detaches) stop(); }
    void send(const bitwire::Path& path, const bitwire::Message& message) override {
        std::shared_ptr<bitwire::Endpoint> child;
        { std::lock_guard lock(mutex_); if (closed_) throw Closed();
          encode_path(path); if (path.empty() || !children_.contains(path[0]) || !children_.at(path[0])) throw NoRoute();
          child = children_.at(path[0]); }
        child->send(bitwire::Path(path.begin()+1,path.end()), message);
    }
    bitwire::Detach receive(bitwire::Receiver receiver) override {
        auto held = std::make_shared<Attachment>(Attachment{receiver, {}, children_.size()});
        { std::lock_guard lock(mutex_); if (closed_) throw Closed(); if (attachment_) throw ReceiverExists(); attachment_=held; }
        auto weak = weak_from_this();
        try {
            for (const auto& [key, child] : children_) {
                if (!child) throw NoRoute();
                auto stop = child->receive({
                    [callback=receiver.message,key](const bitwire::Path& path,const bitwire::Message& message) { if(callback) callback(joined({key},path),message); },
                    [weak,held](int code,const std::string& reason) {
                        if (auto self=weak.lock()) {
                            bool last;
                            {std::lock_guard lock(self->mutex_); last=self->attachment_==held && --held->remaining==0;}
                            if(last) { self->detach(held); if(held->receiver.closed) held->receiver.closed(code,reason); }
                        }
                    }});
                bool active;
                {std::lock_guard lock(mutex_);active=attachment_==held;if(active)held->detaches.push_back(stop);}
                if(!active){stop();throw Closed();}
            }
        } catch (...) { detach(held); throw; }
        return [weak,held=std::weak_ptr<Attachment>(held)] {if(auto self=weak.lock())if(auto value=held.lock())self->detach(value);};
    }
    void close(int code,std::string reason) override {
        std::shared_ptr<Attachment> held;
        {std::lock_guard lock(mutex_);if(closed_)return;closed_=true;held=std::move(attachment_);}
        if(held){for(auto& stop:held->detaches)stop();held->detaches.clear();if(held->receiver.closed)held->receiver.closed(code,reason);}
    }
};

} // namespace

std::string encode_path(const bitwire::Path& path) {
    std::string encoded;
    for (const auto& segment : path) {
        if (!scalar_utf8(segment)) throw InvalidPath();
        encoded += std::to_string(segment.size()) + ':' + segment;
    }
    return encoded;
}

bitwire::Path decode_path(std::string_view encoded) {
    bitwire::Path path;
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

bitwire::WirePtr at(bitwire::WirePtr root, bitwire::Path path) {
    if (!root) throw std::invalid_argument("wire selection requires an origin");
    return std::make_shared<SelectedWire>(std::move(root), std::move(path));
}

std::shared_ptr<bitwire::Endpoint> mount(std::map<std::string, std::shared_ptr<bitwire::Endpoint>> children) {
    return std::make_shared<MountedWire>(std::move(children));
}

struct Dispatcher::State {
    struct Slot { bitwire::Receiver receiver; bitwire::Path prefix; };
    std::shared_ptr<bitwire::Endpoint> root;
    std::mutex mutex;
    bool closed = false;
    bitwire::Detach detach;
    std::map<bitwire::Path, std::shared_ptr<Slot>> routes;
    std::map<std::pair<const bitwire::ReturnAddress*, std::string>,
        std::pair<std::weak_ptr<bitwire::ReturnAddress>, std::shared_ptr<Slot>>> captured;
};

Dispatcher::Dispatcher(std::shared_ptr<bitwire::Endpoint> root) : state_(std::make_shared<State>()) {
    if (!root) throw std::invalid_argument("dispatcher requires an endpoint");
    state_->root = std::move(root);
}
std::shared_ptr<Dispatcher> Dispatcher::create(std::shared_ptr<bitwire::Endpoint> root) {
    auto owner = std::shared_ptr<Dispatcher>(new Dispatcher(std::move(root)));
    owner->attach();
    return owner;
}
Dispatcher::~Dispatcher() { close(); }
void Dispatcher::attach() {
    const auto weak = weak_from_this();
    auto stop = state_->root->receive({
        [weak](const bitwire::Path& path, const bitwire::Message& message) {
            auto owner = weak.lock(); if (!owner) return;
            auto state = owner->state_;
            std::shared_ptr<State::Slot> slot;
            {
                std::lock_guard lock(state->mutex);
                std::erase_if(state->captured, [](const auto& entry) { return entry.second.first.expired(); });
                const auto key = std::make_pair(message.return_address.get(), message.frame.id);
                if (message.frame.kind == bitwire::ProfileKind::cancel) {
                    const auto found = state->captured.find(key);
                    if (found != state->captured.end()) slot = found->second.second;
                } else {
                    for (const auto& [prefix, candidate] : state->routes) {
                        if (prefix.size() <= path.size() && std::equal(prefix.begin(), prefix.end(), path.begin()) &&
                            (!slot || prefix.size() > slot->prefix.size())) slot = candidate;
                    }
                    if (slot && message.frame.kind == bitwire::ProfileKind::request && message.return_address)
                        state->captured[key] = {message.return_address, slot};
                }
            }
            if (slot && slot->receiver.message)
                slot->receiver.message(bitwire::Path(path.begin() + slot->prefix.size(), path.end()), message);
        },
        [weak](int code, const std::string& reason) { if (auto owner = weak.lock()) owner->close(code, reason); }
    });
    bool ended;
    { std::lock_guard lock(state_->mutex); ended = state_->closed; if (!ended) state_->detach = stop; }
    if (ended) { stop(); throw Closed(); }
}
void Dispatcher::close(int code, std::string reason) {
    bitwire::Detach stop;
    std::map<bitwire::Path, std::shared_ptr<State::Slot>> held;
    {
        std::lock_guard lock(state_->mutex);
        if (state_->closed) return;
        state_->closed = true; stop = std::move(state_->detach); held.swap(state_->routes);
    }
    if (stop) stop();
    for (const auto& [_, slot] : held) if (slot->receiver.closed) slot->receiver.closed(code, reason);
}
std::shared_ptr<bitwire::Endpoint> Dispatcher::select(bitwire::Path prefix) {
    encode_path(prefix);
    class Selected final : public bitwire::Endpoint {
        std::shared_ptr<Dispatcher> owner_;
        std::shared_ptr<State> state_;
        bitwire::Path prefix_;
        bool closed_ = false;
        std::weak_ptr<State::Slot> attachment_;
    public:
        Selected(std::shared_ptr<Dispatcher> owner, std::shared_ptr<State> state, bitwire::Path prefix)
            : owner_(std::move(owner)), state_(std::move(state)), prefix_(std::move(prefix)) {}
        void send(const bitwire::Path& path, const bitwire::Message& message) override {
            { std::lock_guard lock(state_->mutex); if (closed_ || state_->closed) throw Closed(); }
            state_->root->send(joined(prefix_, path), message);
        }
        bitwire::Detach receive(bitwire::Receiver receiver) override {
            auto slot = std::make_shared<State::Slot>(State::Slot{std::move(receiver), prefix_});
            {
                std::lock_guard lock(state_->mutex);
                if (closed_ || state_->closed) throw Closed();
                if (state_->routes.contains(prefix_)) throw ReceiverExists();
                state_->routes[prefix_] = slot; attachment_ = slot;
            }
            return [weak = std::weak_ptr<State>(state_), prefix = prefix_, slot = std::weak_ptr<State::Slot>(slot)] {
                if (auto state = weak.lock()) {
                    std::lock_guard lock(state->mutex);
                    const auto found = state->routes.find(prefix);
                    if (found != state->routes.end() && found->second == slot.lock()) state->routes.erase(found);
                }
            };
        }
        void close(int code, std::string reason) override {
            std::shared_ptr<State::Slot> slot;
            {
                std::lock_guard lock(state_->mutex);
                if (closed_) return; closed_ = true;
                const auto found = state_->routes.find(prefix_);
                if (found != state_->routes.end() && found->second == attachment_.lock()) {
                    slot = found->second; state_->routes.erase(found);
                }
            }
            if (slot && slot->receiver.closed) slot->receiver.closed(code, reason);
        }
    };
    return std::make_shared<Selected>(shared_from_this(), state_, std::move(prefix));
}

} // namespace nightseam::duplex
