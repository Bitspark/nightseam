#include <nightseam/duplex/conn.hpp>

#include <array>
#include <condition_variable>
#include <deque>
#include <mutex>

namespace nightseam::duplex {
namespace {

struct End {
    std::deque<Frame> sent;
    bool closed = false;
    int code = 1006;
    std::string reason;
};

struct Shared {
    std::mutex mutex;
    std::condition_variable_any changed;
    std::array<End, 2> ends;
    std::size_t byte_limit, capacity;
    Shared(std::size_t byte_limit, std::size_t capacity) : byte_limit(byte_limit), capacity(capacity) {}
};

template<class Predicate>
void await(Shared& state, std::unique_lock<std::mutex>& lock, Wait wait, Predicate ready) {
    wait.check();
    bool reached;
    if (wait.deadline == std::chrono::steady_clock::time_point::max()) {
        reached = state.changed.wait(lock, wait.stop, ready);
    } else {
        reached = state.changed.wait_until(lock, wait.stop, wait.deadline, ready);
    }
    if (!reached) { wait.check(); throw DeadlineExceeded(); }
}

class PipeEnd final : public Conn {
    std::shared_ptr<Shared> state_;
    std::size_t index_;

    void finish(int code, std::string reason) noexcept {
        {
            std::lock_guard lock(state_->mutex);
            auto& local = state_->ends[index_];
            if (local.closed) return;
            local.closed = true;
            local.code = code;
            local.reason = std::move(reason);
        }
        state_->changed.notify_all();
    }

public:
    PipeEnd(std::shared_ptr<Shared> state, std::size_t index) : state_(std::move(state)), index_(index) {}
    ~PipeEnd() override { abort(); }

    void send(const Frame& frame, Wait wait) override {
        if (frame.kind != Kind::text && frame.kind != Kind::binary) throw std::invalid_argument("duplex frame of no kind");
        std::unique_lock lock(state_->mutex);
        auto& local = state_->ends[index_];
        auto& remote = state_->ends[1-index_];
        if (local.closed) throw Closed();
        if (remote.closed) throw CloseError(remote.code, remote.reason);
        await(*state_, lock, wait, [&] { return local.closed || remote.closed || local.sent.size() < state_->capacity; });
        if (local.closed) throw Closed();
        if (remote.closed) throw CloseError(remote.code, remote.reason);
        local.sent.push_back(frame);
        lock.unlock();
        state_->changed.notify_all();
    }

    Frame receive(Wait wait) override {
        std::unique_lock lock(state_->mutex);
        auto& local = state_->ends[index_];
        auto& remote = state_->ends[1-index_];
        if (local.closed) throw Closed();
        await(*state_, lock, wait, [&] { return local.closed || remote.closed || !remote.sent.empty(); });
        if (local.closed) throw Closed();
        if (remote.sent.empty()) throw CloseError(remote.code, remote.reason);
        auto frame = std::move(remote.sent.front());
        remote.sent.pop_front();
        auto oversized = state_->byte_limit != 0 && frame.data.size() > state_->byte_limit;
        if (oversized) local.closed = true;
        lock.unlock();
        state_->changed.notify_all();
        if (oversized) throw FrameTooLarge("duplex frame exceeds the receive byte limit");
        return frame;
    }

    void close(int code, std::string reason, Wait) override { finish(code, std::move(reason)); }
    void abort() noexcept override { finish(1006, ""); }
};

} // namespace

std::pair<std::shared_ptr<Conn>, std::shared_ptr<Conn>> pipe(std::size_t byte_limit, std::size_t capacity) {
    if (capacity == 0) throw std::invalid_argument("a pipe's frame capacity must be positive");
    auto state = std::make_shared<Shared>(byte_limit, capacity);
    return {std::make_shared<PipeEnd>(state, 0), std::make_shared<PipeEnd>(state, 1)};
}

} // namespace nightseam::duplex
