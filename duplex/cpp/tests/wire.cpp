#include <nightseam/duplex/wire.hpp>
#include <condition_variable>
#include <deque>
#include <future>
#include <iostream>
#include <mutex>
#include <set>

using namespace nightseam::duplex;
using namespace bitwire;

namespace {
void check(bool condition, const char* message) { if (!condition) throw std::runtime_error(message); }
template<class Error, class F> void refuses(F action) {
    try { action(); } catch (const Error&) { return; }
    throw std::runtime_error("expected refusal");
}

// Only drain invokes callbacks. This proves views introduce no dispatch and
// preserve order without depending on thread scheduling.
class Root : public Endpoint, public std::enable_shared_from_this<Root> {
public:
    std::mutex mutex;
    std::map<std::pair<Path, bool>, Receiver> receivers;
    std::deque<std::pair<Path, Message>> queue;
    bool closed = false;
    int closes = 0;
    void send(const Path& path, const Message& message) override {
        encode_path(path);
        std::lock_guard lock(mutex);
        if (closed) throw Closed();
        queue.emplace_back(path, message);
    }
    Detach receive(Receiver receiver) override {
        std::lock_guard lock(mutex);
        if (closed) throw Closed();
        const auto key = std::make_pair(Path{}, false);
        if (!receivers.empty()) throw ReceiverExists();
        receivers[key] = std::move(receiver);
        return [weak = weak_from_this(), key, once = std::make_shared<std::once_flag>()] {
            std::call_once(*once, [&] {
                if (auto self = weak.lock()) { std::lock_guard lock(self->mutex); self->receivers.erase(key); }
            });
        };
    }
    void close(int code, std::string reason) override {
        std::map<std::pair<Path, bool>, Receiver> owned;
        {
            std::lock_guard lock(mutex);
            if (closed) return;
            closed = true;
            ++closes;
            owned.swap(receivers);
        }
        for (const auto& [key, receiver] : owned) if (receiver.closed) receiver.closed(code, reason);
    }
    void drain() {
        for (;;) {
            std::pair<Path, Message> delivery;
            Receiver receiver;
            {
                std::lock_guard lock(mutex);
                if (queue.empty()) return;
                delivery = std::move(queue.front()); queue.pop_front();
                if (!receivers.empty()) receiver = receivers.begin()->second;
            }
            if (receiver.message) receiver.message(delivery.first, delivery.second);
        }
    }
};

void codec() {
    const std::vector<Path> paths{{}, {""}, {"a", "b"}, {"a/b"}, {"a.b"}, {"a", "b:c"},
        {"\xc3\xa9", "e\xcc\x81", "\xf0\x9f\x98\x80", "\xef\xbb\xbf", std::string(1, '\0')}};
    std::set<std::string> encodings;
    for (const auto& path : paths) {
        const auto encoded = encode_path(path);
        check(encodings.insert(encoded).second, "path alias");
        check(decode_path(encoded) == path, "path round trip");
        for (const auto& suffix : paths) {
            auto joined = path; joined.insert(joined.end(), suffix.begin(), suffix.end());
            check(encode_path(joined) == encoded + encode_path(suffix), "path concatenation");
        }
    }
    check(encode_path({"a", "\xf0\x9f\x98\x80", ""}) == "1:a4:\xf0\x9f\x98\x80" "0:", "byte length");
    for (const std::string malformed : {"01:a", "00:", "1", ":", "-1:a", "2:a", "1:\xc3\xa9",
        "999999999999999999999999999999:x", "1:\xff", "3:\xed\xa0\x80", "4:\xf4\x90\x80\x80", "2:\xc0\x80"}) {
        refuses<InvalidPath>([&] { decode_path(malformed); });
    }
    refuses<InvalidPath>([] { encode_path({"\xff"}); });
}

void composition() {
    auto root = std::make_shared<Root>();
    auto reply = std::make_shared<Root>();
    auto address = std::make_shared<ReturnAddress>(ReturnAddress{reply});
    Path prefix{"a.b"};
    auto root_routes = Dispatcher::create(root);
    auto selected = root_routes->select(prefix); prefix[0] = "changed";
    std::map<std::string, std::shared_ptr<Endpoint>> children{{"x", selected}};
    auto mounted = mount(children); children["x"] = reply;
    auto views = Dispatcher::create(mounted);
    auto view = views->select({"x", "\xf0\x9f\x98\x80"});
    std::vector<Message> delivered;
    auto detach = view->receive({ [&](const Path& path, const Message& message) {
        check(path == Path{"call"}, "relative callback path"); delivered.push_back(message);
    }, {}});
    std::vector<Message> messages;
    for (auto kind : {ProfileKind::request, ProfileKind::response, ProfileKind::event, ProfileKind::cancel}) {
        ProfileFrame frame;
        frame.kind = kind; frame.id = "c:1"; frame.params = "{\"n\":9007199254740993}";
        frame.data = "null"; frame.error = ProfileError{"refused", "No", "{\"why\":\"test\"}"};
        frame.meta = {{"tag", "value"}};
        messages.push_back({frame, address});
        view->send({"call"}, messages.back());
    }
    check(delivered.empty() && root->queue.size() == 4 && reply->queue.empty(), "synchronous view dispatch");
    for (const auto& [path, message] : root->queue) check(path == Path{"a.b", "\xf0\x9f\x98\x80", "call"}, "root path");
    root->drain(); check(delivered == messages, "opaque message or return identity changed");
    refuses<ReceiverExists>([&] { view->receive({}); });
    const auto captured = root->receivers.begin()->second;
    detach(); detach();
    captured.message({"a.b", "\xf0\x9f\x98\x80", "call"}, messages.back());
    check(delivered.size() == 5, "captured cancellation lost on detach");
    auto replacement = view->receive({});
    detach(); check(root->receivers.size() == 1, "old detach removed new receiver"); replacement();
    views->close(); root_routes->close();

    int closed = 0;
    auto empty = mount({{"", root}});
    empty->receive({ {}, [&](int code, const std::string& reason) {
        ++closed; check(code == 1000 && reason == "ended", "close details"); empty->close(code, reason);
    }});
    refuses<ReceiverExists>([&] { empty->receive({}); });
    empty->close(1000, "ended"); empty->close();
    check(closed == 1 && root->closes == 0 && root->receivers.empty(), "mount ownership");
    refuses<Closed>([&] { empty->send({"", "call"}, {}); });
    root->send({"call"}, {});
    check(!std::dynamic_pointer_cast<Endpoint>(at(root, {"call"})), "selection granted endpoint authority");
    root->close(1000, "done");
}

void namespaces() {
    auto left = std::make_shared<Root>(), right = std::make_shared<Root>();
    auto left_routes = Dispatcher::create(left);
    auto mounted = mount({{"left", left_routes->select({"private"})}, {"", right}});
    std::vector<Path> paths;
    int closed = 0;
    auto detach = mounted->receive({ [&](const Path& path, const Message&) { paths.push_back(path); },
        [&](int, const std::string&) { ++closed; }});
    mounted->send({"left", "nested", "call"}, {}); left->drain(); left->close(1000, "left ended");
    check(closed == 0, "one child ended namespace");
    mounted->send({"", "still", "usable"}, {}); right->drain();
    check(paths == std::vector<Path>{{"left", "nested", "call"}, {"", "still", "usable"}}, "namespace path");
    detach(); detach(); mounted->close();
    check(closed == 0 && right->receivers.empty() && !right->closed, "namespace detach ownership");
    auto root = std::make_shared<Root>();
    auto overlapping = mount({{"one", root}, {"two", root}});
    refuses<ReceiverExists>([&] { overlapping->receive({ {}, {}}); });
    check(root->receivers.empty() && !root->closed, "partial registration rollback");
    auto all = mount({{"one", left}, {"two", right}});
    refuses<Closed>([&] { all->receive({ {}, {}}); });
    check(right->receivers.empty(), "closed child registration rollback");

    auto a = std::make_shared<Root>(), b = std::make_shared<Root>();
    auto endings = mount({{"a", a}, {"b", b}});
    int ended = 0;
    endings->receive({ {}, [&](int, const std::string&) { ++ended; }});
    a->close(1000, "a ended"); check(ended == 0, "early namespace ending");
    b->close(1000, "b ended"); endings->close();
    check(ended == 1, "namespace must end exactly once");

    auto borrowed = std::make_shared<Root>();
    {
        auto temporary = mount({{"child", borrowed}});
        temporary->receive({ {}, {}});
    }
    check(borrowed->receivers.empty() && !borrowed->closed, "dropped mount retained a receiver");

    auto nested = mount({{"outer", mount({{"inner", borrowed}})}});
    auto marker = std::make_shared<int>(1);
    std::weak_ptr<int> released = marker;
    auto retained_detach = nested->receive({
        [marker](const Path&, const Message&) {}, {}});
    marker.reset();
    nested->close();
    check(released.expired(), "nested registrations retained a callback after close");
    retained_detach();
}

void registration_race() {
    class PausingRoot final : public Root {
    public:
        std::promise<void> registered, resume;
        Detach receive(Receiver receiver) override {
            auto detach = Root::receive(std::move(receiver));
            registered.set_value(); resume.get_future().wait(); return detach;
        }
    };
    auto root = std::make_shared<PausingRoot>();
    auto mounted = mount({{"x", root}});
    int closed = 0;
    auto task = std::async(std::launch::async, [&] {
        refuses<Closed>([&] { mounted->receive({ {}, [&](int, const std::string&) { ++closed; }}); });
    });
    root->registered.get_future().wait();
    mounted->close(); root->resume.set_value(); task.get();
    check(root->receivers.empty() && root->closes == 0 && closed == 1, "registration close race");
}
} // namespace

int main() {
    try { codec(); composition(); namespaces(); registration_race(); }
    catch (const std::exception& error) { std::cerr << error.what() << '\n'; return 1; }
}
