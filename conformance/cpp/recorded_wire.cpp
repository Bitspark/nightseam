#include "recorded_wire.hpp"
#include <nightseam/duplex/wire.hpp>
#include <atomic>
#include <condition_variable>
#include <deque>
#include <mutex>
#include <thread>

namespace nightseam::conformance {
namespace {
using namespace duplex;
using runtime::Value;

template<class T> class Channel {
    std::mutex mutex_;
    std::condition_variable_any changed_;
    std::deque<T> queue_;
    std::exception_ptr failure_;
public:
    void push(T value) {
        { std::lock_guard lock(mutex_); queue_.push_back(std::move(value)); }
        changed_.notify_all();
    }
    void fail(std::exception_ptr failure) {
        { std::lock_guard lock(mutex_); failure_ = failure; }
        changed_.notify_all();
    }
    T pop(Wait wait) {
        std::unique_lock lock(mutex_);
        if (!changed_.wait_until(lock, wait.stop, wait.deadline, [&] { return failure_ || !queue_.empty(); })) {
            wait.check(); throw DeadlineExceeded();
        }
        if (failure_) std::rethrow_exception(failure_);
        T value = std::move(queue_.front()); queue_.pop_front(); return value;
    }
    std::size_t size() { std::lock_guard lock(mutex_); return queue_.size(); }
};

struct Entry { Path path; Message message; int sequence = 0; };

// This is the witness's asynchronous fixture root, not a transport or public
// record/follow primitive. Production selection and mounting do all routing.
class Root final : public Wire, public std::enable_shared_from_this<Root> {
    std::mutex mutex_;
    std::condition_variable changed_;
    std::map<std::string, Receiver> receivers_;
    std::deque<Entry> queue_;
    bool stopped_ = false;
    std::thread worker_;
    void run() {
        for (;;) {
            Entry entry;
            Receiver receiver;
            {
                std::unique_lock lock(mutex_);
                changed_.wait(lock, [&] { return stopped_ || !queue_.empty(); });
                if (stopped_) return;
                entry = std::move(queue_.front()); queue_.pop_front();
                auto found = receivers_.find(encode_path(entry.path));
                if (found != receivers_.end()) receiver = found->second;
            }
            if (receiver.message) receiver.message(entry.path, entry.message);
        }
    }
public:
    Root() : worker_([this] { run(); }) {}
    ~Root() override { close(1000, "done"); }
    void send(const Path& path, const Message& message) override {
        encode_path(path);
        {
            std::lock_guard lock(mutex_);
            if (stopped_) throw Closed();
            if (queue_.size() >= 16) throw std::runtime_error("witness output queue full");
            queue_.push_back({path, message});
        }
        changed_.notify_one();
    }
    Detach receive(const Path& path, Receiver receiver) override {
        const auto key = encode_path(path);
        std::lock_guard lock(mutex_);
        if (stopped_) throw Closed();
        if (receivers_.contains(key)) throw ReceiverExists();
        receivers_[key] = std::move(receiver);
        return [weak = weak_from_this(), key, once = std::make_shared<std::once_flag>()] {
            std::call_once(*once, [&] {
                if (auto self = weak.lock()) { std::lock_guard lock(self->mutex_); self->receivers_.erase(key); }
            });
        };
    }
    void close(int, std::string) override {
        { std::lock_guard lock(mutex_); stopped_ = true; }
        changed_.notify_all();
        if (worker_.joinable()) worker_.join();
    }
};

class Follower {
    WirePtr target_;
    std::size_t bound_;
    std::mutex mutex_;
    std::condition_variable changed_;
    std::deque<Entry> live_;
    bool stopped_ = false;
    bool resumed_ = false;
    std::thread worker_;
    bool send(const Entry& entry) {
        { std::lock_guard lock(mutex_); if (stopped_) return false; }
        target_->send(entry.path, entry.message);
        sent.push(entry.sequence);
        return true;
    }
    void run(const std::vector<Entry>& history, bool pause) {
        try {
            bool running = true;
            for (std::size_t i = 0; i < history.size(); ++i) {
                if (!send(history[i])) { running = false; break; }
                if (i == 0 && pause) {
                    paused.push(true);
                    std::unique_lock lock(mutex_);
                    changed_.wait(lock, [&] { return stopped_ || resumed_; });
                    if (stopped_) { running = false; break; }
                }
            }
            while (running) {
                Entry entry;
                {
                    std::unique_lock lock(mutex_);
                    changed_.wait(lock, [&] { return stopped_ || !live_.empty(); });
                    if (stopped_) break;
                    entry = std::move(live_.front()); live_.pop_front();
                }
                if (!send(entry)) break;
            }
        } catch (...) { sent.fail(std::current_exception()); }
        try { target_->close(1008, "recorded handoff ended"); }
        catch (...) { done.fail(std::current_exception()); }
        done.push(true);
    }
public:
    Channel<int> sent;
    Channel<bool> paused, done;
    Follower(WirePtr target, std::size_t bound) : target_(std::move(target)), bound_(bound) {}
    ~Follower() { stop(); if (worker_.joinable()) worker_.join(); }
    void start(std::vector<Entry> history, bool pause) {
        worker_ = std::thread([this, history = std::move(history), pause] { run(history, pause); });
    }
    bool enqueue(const Entry& entry) {
        bool accepted;
        {
            std::lock_guard lock(mutex_);
            accepted = !stopped_ && live_.size() < bound_;
            if (accepted) live_.push_back(entry);
            else stopped_ = true;
        }
        // Signal only. Closing the subscriber is its own writer's job, outside
        // append exclusion and off the producer's stack.
        changed_.notify_all();
        return accepted;
    }
    void stop() { { std::lock_guard lock(mutex_); stopped_ = true; } changed_.notify_all(); }
    void resume() { { std::lock_guard lock(mutex_); resumed_ = true; } changed_.notify_all(); }
    std::size_t queued() { std::lock_guard lock(mutex_); return live_.size(); }
    void await_sent(int last, Wait wait) { while (sent.pop(wait) != last) {} }
};

class Store final : public Wire {
    std::mutex mutex_;
    std::vector<Entry> entries_;
    std::vector<std::weak_ptr<Follower>> followers_;
public:
    int head() { std::lock_guard lock(mutex_); return static_cast<int>(entries_.size()); }
    void send(const Path& path, const Message& message) override {
        std::lock_guard lock(mutex_);
        Entry entry{path, message, static_cast<int>(entries_.size()) + 1};
        entries_.push_back(entry);
        for (auto i = followers_.begin(); i != followers_.end();) {
            auto follower = i->lock();
            if (!follower || !follower->enqueue(entry)) i = followers_.erase(i);
            else ++i;
        }
    }
    Detach receive(const Path&, Receiver) override { throw NoRoute(); }
    void close(int, std::string) override {
        std::vector<std::weak_ptr<Follower>> followers;
        { std::lock_guard lock(mutex_); followers.swap(followers_); }
        for (const auto& weak : followers) if (auto follower = weak.lock()) follower->stop();
    }
    std::pair<int, std::shared_ptr<Follower>> attach(int after, WirePtr target, std::size_t bound, bool pause) {
        auto follower = std::make_shared<Follower>(std::move(target), bound);
        std::vector<Entry> history;
        int head;
        {
            std::lock_guard lock(mutex_);
            head = static_cast<int>(entries_.size());
            history.assign(entries_.begin() + after, entries_.end());
            // The head and registration share append's lock, preventing a gap
            // or duplicate at the replay-to-follow cut.
            followers_.push_back(follower);
        }
        follower->start(std::move(history), pause);
        return {head, follower};
    }
};

Message tick(int value) {
    Message message;
    message.frame.kind = ProfileKind::event;
    message.frame.data = std::to_string(value);
    return message;
}

struct Presentation {
    struct Observed {
        Channel<int> values, closed;
        std::atomic<int> callbacks{0};
    };
    std::shared_ptr<Root> root = std::make_shared<Root>();
    std::shared_ptr<Root> end = std::make_shared<Root>();
    std::shared_ptr<Observed> observed = std::make_shared<Observed>();
    WirePtr wire, destination;
    explicit Presentation(const std::shared_ptr<Store>& store) {
        destination = at(mount({{"out", at(end, {"destination"})}}), {"out"});
        wire = at(mount({{"outer", mount({{"in", at(root, {"source"})}})}}), {"outer", "in"});
        destination->receive({"tick"}, {false, [store, state = observed](const Path& path, const Message& message) {
            try {
                if (path != Path{"tick"}) throw std::runtime_error("recorded destination received wrong relative path");
                // This real callback reenters append. Calling it under append
                // exclusion deadlocks and fails the witness's deadline.
                (void)store->head();
                ++state->callbacks;
                state->values.push(runtime::parse_value(message.frame.data.value()).as<int>());
            } catch (...) { state->values.fail(std::current_exception()); }
        }, {}});
        wire->receive({"tick"}, {false, [target = destination, state = observed](const Path& path, const Message& message) {
            // An opaque forwarding hop, not payload interpretation.
            try { target->send(path, message); }
            catch (...) { state->values.fail(std::current_exception()); }
        }, [store, state = observed](int code, const std::string&) {
            (void)store->head(); state->closed.push(code);
        }});
    }
    ~Presentation() {
        wire->close(1000, "done");
        root->close(1000, "done");
        destination->close(1000, "done");
        end->close(1000, "done");
    }
    Value collect(Wait wait) {
        // A fence follows the same nested mount and forward route, so delayed
        // duplicates cannot hide behind a fixed expected delivery count.
        wire->send({"tick"}, tick(0));
        Value values = Value::array();
        for (;;) {
            const auto value = observed->values.pop(wait);
            if (value == 0) return values;
            values.push_back(value);
        }
    }
};

Value head_case(Wait wait, bool before) {
    auto store = std::make_shared<Store>();
    Presentation first(store);
    auto source = at(mount({{"record", store}}), {"record"});
    for (int value = 1; value <= 3; ++value) source->send({"tick"}, tick(value));
    const auto cut = before ? "append_before_head" : "head_before_append";
    int next = 4;
    if (before) { source->send({"tick"}, tick(4)); next = 5; }
    auto [head, follower] = store->attach(0, first.wire, 2, true);
    follower->paused.pop(wait);
    Channel<bool> produced;
    std::jthread producer([&] {
        try {
            for (int value = next; value <= 5; ++value) source->send({"tick"}, tick(value));
            produced.push(true);
        } catch (...) { produced.fail(std::current_exception()); }
    });
    const bool progress = produced.pop(wait); // Writer is still held here.
    follower->resume(); follower->await_sent(5, wait);
    source->send({"tick"}, tick(6)); follower->await_sent(6, wait);
    auto values = first.collect(wait);
    Presentation second(store);
    auto [late_head, late] = store->attach(3, second.wire, 2, false);
    (void)late_head;
    late->await_sent(6, wait);
    auto after = second.collect(wait);
    Value result;
    result["cut"] = cut; result["head"] = head;
    result["first"] = std::move(values); result["after_three"] = std::move(after);
    result["producer_progress"] = progress;
    result["callbacks_outside_append"] = first.observed->callbacks.load() > 0 && second.observed->callbacks.load() > 0;
    return result;
}

Value stall_case(Wait wait) {
    auto store = std::make_shared<Store>();
    for (int value = 1; value <= 3; ++value) store->send({"tick"}, tick(value));
    Presentation stalled(store), healthy(store);
    auto [head, slow] = store->attach(0, stalled.wire, 2, true);
    (void)head;
    slow->paused.pop(wait);
    auto [healthy_head, fast] = store->attach(3, healthy.wire, 2, false);
    (void)healthy_head;
    for (int value = 4; value <= 5; ++value) {
        store->send({"tick"}, tick(value)); fast->await_sent(value, wait);
    }
    const auto queued = slow->queued();
    store->send({"tick"}, tick(6)); fast->await_sent(6, wait);
    slow->done.pop(wait);
    const auto code = stalled.observed->closed.pop(wait);
    bool refused = false;
    try { stalled.wire->send({"tick"}, tick(99)); } catch (const Closed&) { refused = true; }
    if (!refused) throw std::runtime_error("stalled carrier accepted after close");
    Channel<int> underneath;
    stalled.root->receive({"probe"}, {false, [&](const Path&, const Message& message) {
        try { underneath.push(runtime::parse_value(message.frame.data.value()).as<int>()); }
        catch (...) { underneath.fail(std::current_exception()); }
    }, {}});
    stalled.root->send({"probe"}, tick(99));
    const auto probe = underneath.pop(wait);
    store->send({"tick"}, tick(7)); fast->await_sent(7, wait);
    auto values = healthy.collect(wait);
    Value result;
    result["bound"] = 2; result["queued_at_bound"] = queued;
    result["closed"] = 1 + stalled.observed->closed.size(); result["close_code"] = code;
    result["healthy"] = std::move(values);
    Value probes = Value::array(); probes.push_back(probe); result["underneath"] = std::move(probes);
    result["head"] = store->head(); result["producer_progress"] = true;
    return result;
}
} // namespace

Value recorded_wire_witness(duplex::Wait wait) {
    wait.check();
    Value cases = Value::array();
    cases.push_back(head_case(wait, false)); cases.push_back(head_case(wait, true));
    Value result;
    result["cases"] = std::move(cases); result["stalled"] = stall_case(wait);
    return result;
}

} // namespace nightseam::conformance
