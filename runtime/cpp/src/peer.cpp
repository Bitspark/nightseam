#include <nightseam/runtime/peer.hpp>

#include <algorithm>
#include <atomic>
#include <condition_variable>
#include <deque>
#include <mutex>
#include <limits>
#include <random>
#include <thread>

namespace nightseam::runtime {
using Clock = std::chrono::steady_clock;
using namespace std::chrono_literals;

bool RequestContext::cancelled() const { return wait.stop.stop_requested() || Clock::now() >= wait.deadline; }
void RequestContext::check() const {
    if (Clock::now() >= wait.deadline) throw duplex::DeadlineExceeded();
    wait.check();
}

namespace {
class W3CPropagator final : public Propagator {
    std::mutex mutex_;
    std::random_device random_;
    std::string random_id(std::size_t bytes) {
        std::lock_guard lock(mutex_);
        constexpr char hex[] = "0123456789abcdef";
        std::string value; value.reserve(bytes*2);
        for (std::size_t i = 0; i < bytes; ++i) { auto byte = random_(); value += hex[byte & 15]; value += hex[(byte >> 4) & 15]; }
        return value;
    }
public:
    void extract(RequestContext& context, const Trace& trace) override { if (!trace.parent.empty()) context.trace = trace; }
    Trace inject(const RequestContext& context) override {
        if (valid_traceparent(context.trace.parent)) return {context.trace.parent.substr(0,36) + random_id(8) + context.trace.parent.substr(52), context.trace.state};
        return {"00-" + random_id(16) + "-" + random_id(8) + "-01", {}};
    }
};
std::pair<std::string,std::string> outcome(std::exception_ptr error) {
    if (!error) return {"ok",{}};
    try { std::rethrow_exception(error); }
    catch (const PublicError& e) { return {"error", e.code}; }
    catch (const duplex::DeadlineExceeded&) { return {"timeout","request_timeout"}; }
    catch (const duplex::Cancelled&) { return {"cancelled","cancelled"}; }
    catch (...) { return {"error",{}}; }
}
PublicError public_error(std::exception_ptr error) {
    try { std::rethrow_exception(error); }
    catch (const PublicError& e) { if (!e.code.empty() && !e.message.empty()) return e; }
    catch (const duplex::DeadlineExceeded&) { return {"cancelled","Request cancelled"}; }
    catch (const duplex::Cancelled&) { return {"cancelled","Request cancelled"}; }
    catch (...) {}
    return {"internal","Internal error"};
}
void name_valid(const std::string& name) {
    if (name.empty()) throw std::invalid_argument("duplex operation name must not be empty");
    decode_utf8(name);
}
}

std::shared_ptr<Propagator> default_propagator() { return std::make_shared<W3CPropagator>(); }

struct Peer::Impl {
    struct Pending { bool ready = false; std::optional<Value> result; std::exception_ptr error; };
    struct Incoming { std::stop_source stop; Clock::time_point deadline; };
    struct Queued { Envelope frame; std::string text; bool control = false; };
    struct Worker { std::thread thread; std::shared_ptr<std::atomic_bool> done; };
    Peer& owner;
    std::shared_ptr<duplex::Conn> connection;
    Role role;
    PeerOptions options;
    mutable std::mutex mutex;
    mutable std::condition_variable_any cv;
    std::stop_source stop;
    bool closed = false, close_observed = false;
    std::exception_ptr failure;
    CloseInfo close_info;
    std::uint64_t next_id = 0;
    std::string received_serial;
    struct Publication;
    std::deque<Publication*> publications;
    std::map<std::string,std::shared_ptr<Pending>> pending;
    std::map<std::string,std::shared_ptr<Incoming>> incoming;
    std::map<std::string,RawHandler> raw_handlers;
    std::deque<Queued> outputs;
    std::deque<Envelope> events;
    std::size_t output_data = 0, output_control = 0;
    std::thread reader, writer, event_worker, timer;
    std::vector<Worker> workers; // reader owns and reaps completed handlers

    // A cancellable FIFO gate spans reservation, encoding, and queue admission.
    // Entries remain queued while capacity is awaited; new calls cannot pass them.
    struct Publication {
        Impl& peer;
        std::thread::id thread = std::this_thread::get_id();
        Publication(Impl& peer, duplex::Wait wait) : peer(peer) {
            std::unique_lock lock(peer.mutex);
            peer.check_open(); wait.check();
            if (!peer.publications.empty() && peer.publications.front()->thread == thread)
                throw PublicError("busy","Request publication is reentrant");
            peer.publications.push_back(this);
            try {
                while (peer.publications.front() != this) {
                    peer.check_open(); wait.check();
                    peer.cv.wait_until(lock,wait.stop,wait.deadline,[&]{return peer.closed || peer.publications.front()==this;});
                }
                peer.check_open(); wait.check();
            } catch (...) {
                std::erase(peer.publications,this); peer.cv.notify_all(); throw;
            }
        }
        ~Publication() {
            std::lock_guard lock(peer.mutex);
            std::erase(peer.publications,this); peer.cv.notify_all();
        }
        Publication(const Publication&) = delete;
        Publication& operator=(const Publication&) = delete;
    };

    Impl(Peer& owner, std::shared_ptr<duplex::Conn> connection, Role role, PeerOptions options)
        : owner(owner), connection(std::move(connection)), role(role), options(std::move(options)) {
        if (!this->connection) throw std::invalid_argument("duplex requires a connection");
        if (role != Role::client && role != Role::server) throw std::invalid_argument("invalid duplex role");
        auto& o = this->options;
        if (o.request_timeout < 0ms || o.write_timeout < 0ms) throw std::invalid_argument("duplex limits must not be negative");
        if (!o.max_concurrent_handlers) o.max_concurrent_handlers = 64;
        if (!o.max_pending_requests) o.max_pending_requests = 128;
        if (!o.queue_capacity) o.queue_capacity = 128;
        if (!o.max_frame_bytes) o.max_frame_bytes = 1 << 20;
        if (o.request_timeout == 0ms) o.request_timeout = 30s;
        if (o.write_timeout == 0ms) o.write_timeout = 10s;
        if (!o.propagator) o.propagator = default_propagator();
        for (const auto& [name,h] : o.handlers) { name_valid(name); if (!h) throw std::invalid_argument("empty handler"); }
        for (const auto& [name,h] : o.events) { name_valid(name); if (!h) throw std::invalid_argument("empty event handler"); }
    }
    ~Impl() {
        end(std::make_exception_ptr(duplex::Closed()),1000,{});
        for (auto* thread : {&reader,&writer,&event_worker,&timer}) if (thread->joinable()) thread->join();
        for (auto& worker : workers) if (worker.thread.joinable()) worker.thread.join();
    }
    void observe(Observation event) const noexcept {
        if (!options.observer) return;
        auto name = event.method.empty() ? event.name : event.method;
        auto found = options.families.find(name);
        if (found != options.families.end()) event.family = found->second;
        try { options.observer(event); } catch (...) {}
    }
    void frame_observation(const char* type, const Envelope& frame, std::size_t bytes) {
        Observation event; event.type=type; event.kind=frame.kind; event.name=frame.name(); event.id=frame.id; event.trace=frame.trace; event.bytes=bytes; observe(std::move(event));
    }
    Clock::time_point request_started(const Envelope& frame, bool incoming) {
        auto at = Clock::now(); Observation event; event.type="request.started"; event.id=frame.id; event.method=frame.method; event.incoming=incoming; event.trace=frame.trace; observe(std::move(event)); return at;
    }
    void request_ended(const Envelope& frame, bool incoming, Clock::time_point at, std::exception_ptr error) {
        Observation event; event.type="request.ended"; event.id=frame.id; event.method=frame.method; event.incoming=incoming; event.trace=frame.trace; event.duration=Clock::now()-at;
        auto [result,code] = outcome(error); event.outcome=result; event.error_code=code; observe(std::move(event));
    }
    void backpressure(std::size_t queued, bool stalled) {
        Observation event; event.type="backpressure"; event.queued=queued; event.stalled=stalled; event.deadline=options.write_timeout; observe(std::move(event));
    }
    void end(std::exception_ptr error, int code=1006, std::string reason={}, bool local=true) noexcept {
        std::vector<std::shared_ptr<Incoming>> active;
        {
            std::lock_guard lock(mutex);
            if (closed) return;
            closed=true; failure=error; close_info={code==1000,code,local,reason};
            for (auto& [id,call] : incoming) active.push_back(call);
            outputs.clear(); output_data=output_control=0; events.clear();
        }
        stop.request_stop();
        for (auto& call : active) call->stop.request_stop();
        Observation event; event.type="connection.closed"; event.code=code; event.reason=reason; event.local=local; observe(std::move(event));
        { std::lock_guard lock(mutex); close_observed = true; }
        cv.notify_all();
        try {
            if (local && code!=1006) connection->close(code,std::move(reason),duplex::Wait::after(options.write_timeout));
            else connection->abort();
        } catch (...) { connection->abort(); }
    }
    void failed(std::exception_ptr error) noexcept {
        try { std::rethrow_exception(error); }
        catch (const duplex::CloseError& e) { end(error,e.code,e.reason,false); }
        catch (...) { end(error); }
    }
    void start() {
        Observation opened; opened.type="connection.opened"; opened.role=role; observe(std::move(opened));
        try {
            reader=std::thread([this] { read_loop(); });
            writer=std::thread([this] { write_loop(); });
            event_worker=std::thread([this] { event_loop(); });
            timer=std::thread([this] { timer_loop(); });
        } catch (...) { failed(std::current_exception()); throw; }
    }
    void check_open() const { if (closed) std::rethrow_exception(failure); }
    Queued encode(Envelope frame, bool control=false) {
        auto text=encode_envelope(frame);
        if (text.size()>options.max_frame_bytes) throw duplex::FrameTooLarge("duplex frame exceeds size limit");
        return {std::move(frame),std::move(text),control};
    }
    void enqueue(Queued queued, duplex::Wait wait={}, bool immediate=false) {
        std::unique_lock lock(mutex); check_open(); wait.check();
        auto full=[&] { return queued.control ? output_control>=options.max_pending_requests : output_data>=options.queue_capacity; };
        if (full()) {
            auto depth=outputs.size(); lock.unlock(); backpressure(depth,false); lock.lock();
            if (immediate && full()) {
                lock.unlock(); std::this_thread::yield(); lock.lock();
                if (full()) { lock.unlock(); auto e=std::make_exception_ptr(std::runtime_error("duplex consumer is stalled")); end(e); std::rethrow_exception(e); }
            }
            auto deadline=std::min(wait.deadline,Clock::now()+options.write_timeout);
            std::stop_callback cancelled(wait.stop,[&]{cv.notify_all();});
            while (full() && !closed) {
                wait.check();
                if (!cv.wait_until(lock,wait.stop,deadline,[&]{return closed || !full();}) && full()) {
                    wait.check(); depth=outputs.size(); lock.unlock(); backpressure(depth,true);
                    auto e=std::make_exception_ptr(std::runtime_error("duplex consumer is stalled")); end(e); std::rethrow_exception(e);
                }
            }
        }
        check_open(); wait.check();
        if (queued.control) ++output_control; else ++output_data;
        outputs.push_back(std::move(queued)); lock.unlock(); cv.notify_all();
    }
    void cancel(const Envelope& request) noexcept {
        try { Envelope frame; frame.kind="cancel"; frame.id=request.id; frame.trace=request.trace; enqueue(encode(std::move(frame),true),{},true); } catch (...) {}
    }
    void write_loop() noexcept {
        try {
            while (true) {
                std::unique_lock lock(mutex); cv.wait(lock,[&]{return closed || !outputs.empty();});
                if (closed) return;
                auto queued=std::move(outputs.front()); outputs.pop_front();
                if (queued.control) --output_control; else --output_data;
                lock.unlock(); cv.notify_all();
                frame_observation("frame.sent",queued.frame,queued.text.size());
                // Closing the carrier releases its I/O. Cancelling this wait
                // would abort a WebSocket before its chosen close is sent.
                connection->send({duplex::Kind::text,std::move(queued.text)},duplex::Wait::after(options.write_timeout));
            }
        } catch (...) { failed(std::current_exception()); }
    }
    void read_loop() noexcept {
        try {
            while (!stop.stop_requested()) {
                auto received=connection->receive();
                Envelope frame;
                try {
                    if (received.kind!=duplex::Kind::text) throw std::invalid_argument("duplex requires JSON text frames");
                    if (received.data.size()>options.max_frame_bytes) throw std::invalid_argument("duplex frame exceeds size limit");
                    frame=decode_envelope(received.data,role);
                } catch (...) { end(std::current_exception(),4011,"invalid duplex frame"); return; }
                frame_observation("frame.received",frame,received.data.size());
                if (frame.kind=="response") {
                    std::lock_guard lock(mutex);
                    auto found=pending.find(frame.id);
                    if (found!=pending.end()) {
                        auto call=found->second; pending.erase(found);
                        call->ready=true; call->result=std::move(frame.result);
                        if (frame.error) call->error=std::make_exception_ptr(*frame.error);
                        cv.notify_all();
                    }
                } else if (frame.kind=="cancel") {
                    std::shared_ptr<Incoming> call;
                    { std::lock_guard lock(mutex); auto found=incoming.find(frame.id); if (found!=incoming.end()) call=found->second; }
                    if (call) call->stop.request_stop();
                } else if (frame.kind=="request") {
                    auto serial=frame.id.substr(2);
                    if (serial.size()<received_serial.size() || (serial.size()==received_serial.size() && serial<=received_serial)) {
                        end(std::make_exception_ptr(std::runtime_error("request serial did not increase")),4011,"request serial did not increase"); return;
                    }
                    received_serial=std::move(serial);
                    start_request(std::move(frame));
                }
                else {
                    std::unique_lock lock(mutex);
                    if (events.size()>=options.queue_capacity) {
                        auto depth=events.size(); lock.unlock(); backpressure(depth,false); lock.lock();
                        auto deadline=Clock::now()+options.write_timeout;
                        while (!closed && events.size()>=options.queue_capacity) {
                            if (cv.wait_until(lock,deadline)==std::cv_status::timeout && events.size()>=options.queue_capacity) {
                                depth=events.size(); lock.unlock(); backpressure(depth,true); end(std::make_exception_ptr(std::runtime_error("duplex consumer is stalled"))); return;
                            }
                        }
                    }
                    if (closed) return;
                    events.push_back(std::move(frame)); lock.unlock(); cv.notify_all();
                }
            }
        } catch (...) { failed(std::current_exception()); }
    }
    void respond(const Envelope& request, std::optional<Value> result, std::optional<std::string> raw_result, std::exception_ptr error, bool immediate=false) {
        Envelope frame; frame.kind="response"; frame.id=request.id; frame.trace=request.trace;
        if (error) frame.error=public_error(error);
        else { frame.result=std::move(result); frame.raw_result=std::move(raw_result); }
        try { enqueue(encode(std::move(frame)),{stop.get_token()},immediate); }
        catch (...) {
            if (stop.stop_requested()) return;
            Envelope fallback; fallback.kind="response"; fallback.id=request.id; fallback.trace=request.trace;
            fallback.error.emplace("internal","Response could not be encoded");
            try { enqueue(encode(std::move(fallback)),{stop.get_token()},immediate); } catch (...) { failed(std::current_exception()); }
        }
    }
    void start_request(Envelope frame) {
        Handler handler; RawHandler raw_handler;
        auto active=std::make_shared<Incoming>(); active->deadline=Clock::now()+options.request_timeout;
        std::exception_ptr rejection;
        {
            std::unique_lock lock(mutex);
            if (auto found=options.handlers.find(frame.method); found!=options.handlers.end()) handler=found->second;
            if (auto found=raw_handlers.find(frame.method); found!=raw_handlers.end()) raw_handler=found->second;
            if (!handler && !raw_handler) rejection=std::make_exception_ptr(PublicError("method_not_found","Unknown method"));
            else if (incoming.size()>=options.max_concurrent_handlers) rejection=std::make_exception_ptr(PublicError("busy","Too many concurrent requests"));
            else incoming.emplace(frame.id,active);
        }
        auto started=request_started(frame,true);
        if (rejection) { request_ended(frame,true,started,rejection); respond(frame,{},{},rejection,true); return; }
        cv.notify_all();
        // Reap before admitting the next worker; retained thread handles are
        // bounded by the active-handler limit plus one just-finished batch.
        for (auto it=workers.begin(); it!=workers.end();) {
            if (it->done->load()) { it->thread.join(); it=workers.erase(it); } else ++it;
        }
        auto done=std::make_shared<std::atomic_bool>(false);
        workers.push_back({std::thread([this,frame=std::move(frame),active,handler=std::move(handler),raw_handler=std::move(raw_handler),started,done] {
            std::exception_ptr error; std::optional<Value> result; std::optional<std::string> raw_result;
            RequestContext context; context.wait={active->stop.get_token(),active->deadline}; context.meta=frame.meta;
            context.id=frame.id; context.method=frame.method; context.raw_payload=frame.raw_params.value_or("null");
            try {
                options.propagator->extract(context,frame.trace);
                if (raw_handler) { raw_result=raw_handler(context,owner,*frame.params); parse_value(*raw_result); }
                else result=handler(context,owner,*frame.params);
                context.check();
            } catch (const PublicError&) { error=std::current_exception(); }
            catch (const duplex::Cancelled&) { error=std::current_exception(); }
            catch (const duplex::DeadlineExceeded&) { error=std::current_exception(); }
            catch (const std::exception& e) {
                error=std::current_exception(); Observation event; event.type="handler.panic"; event.method=frame.method; event.value=e.what(); event.trace=frame.trace; observe(std::move(event));
            } catch (...) {
                error=std::current_exception(); Observation event; event.type="handler.panic"; event.method=frame.method; event.value="unknown exception"; event.trace=frame.trace; observe(std::move(event));
            }
            request_ended(frame,true,started,error); respond(frame,std::move(result),std::move(raw_result),error);
            { std::lock_guard lock(mutex); incoming.erase(frame.id); }
            done->store(true); cv.notify_all();
        }),done});
    }
    void event_loop() noexcept {
        try {
            while (true) {
                std::unique_lock lock(mutex); cv.wait(lock,[&]{return closed || !events.empty();}); if (closed) return;
                auto frame=std::move(events.front()); events.pop_front();
                EventHandler handler;
                if (auto found=options.events.find(frame.event); found!=options.events.end()) handler=found->second;
                lock.unlock(); cv.notify_all();
                RequestContext context; context.wait.stop=stop.get_token(); context.meta=frame.meta; context.method=frame.event; context.raw_payload=frame.raw_data.value_or("null");
                options.propagator->extract(context,frame.trace);
                Observation event; event.type="event.delivered"; event.name=frame.event; event.trace=frame.trace; event.bytes=context.raw_payload.size(); observe(std::move(event));
                if (handler) handler(context,owner,*frame.data);
                if (options.event_listener) options.event_listener(context,owner,*frame.data);
            }
        } catch (...) { failed(std::current_exception()); }
    }
    void timer_loop() noexcept {
        std::unique_lock lock(mutex);
        while (!closed) {
            std::vector<std::shared_ptr<Incoming>> expired;
            auto next=Clock::time_point::max(); auto now=Clock::now();
            for (auto& [id,call] : incoming) {
                if (call->stop.stop_requested()) continue;
                if (call->deadline<=now) expired.push_back(call); else next=std::min(next,call->deadline);
            }
            if (!expired.empty()) { lock.unlock(); for (auto& call:expired) call->stop.request_stop(); lock.lock(); continue; }
            if (next==Clock::time_point::max()) cv.wait(lock); else cv.wait_until(lock,next);
        }
    }
};

Peer::Peer(std::shared_ptr<duplex::Conn> connection, Role role, PeerOptions options)
    : impl_(std::make_unique<Impl>(*this,std::move(connection),role,std::move(options))) {
    if (impl_->options.prepare) impl_->options.prepare(*this);
    impl_->start();
}
Peer::~Peer() = default;
void Peer::handle(std::string method, Handler handler) {
    name_valid(method); if (!handler) throw std::invalid_argument("empty handler");
    std::lock_guard lock(impl_->mutex); impl_->check_open();
    if (impl_->raw_handlers.contains(method) || impl_->options.handlers.contains(method)) throw std::invalid_argument("method already registered");
    impl_->options.handlers.emplace(std::move(method),std::move(handler));
}
void Peer::handle_raw(std::string method, RawHandler handler) {
    name_valid(method); if (!handler) throw std::invalid_argument("empty handler");
    std::lock_guard lock(impl_->mutex); impl_->check_open();
    if (impl_->raw_handlers.contains(method) || impl_->options.handlers.contains(method)) throw std::invalid_argument("method already registered");
    impl_->raw_handlers.emplace(std::move(method),std::move(handler));
}
void Peer::handle_event(std::string name, EventHandler handler) {
    name_valid(name); if (!handler) throw std::invalid_argument("empty event handler");
    std::lock_guard lock(impl_->mutex); impl_->check_open();
    if (impl_->options.events.contains(name)) throw std::invalid_argument("event already registered");
    impl_->options.events.emplace(std::move(name),std::move(handler));
}

Value Peer::call(std::string method, Value params, CallOptions options) { return call_raw(std::move(method),stringify(params),std::move(options)); }
Value Peer::call_raw(std::string method, std::optional<std::string> params, CallOptions options) {
    name_valid(method); auto& p=*impl_;
    std::stop_source stop;
    auto parent=options.context.value_or(RequestContext{});
    auto signal=[&]{stop.request_stop(); p.cv.notify_all();};
    std::stop_callback direct(options.wait.stop,signal), inherited(parent.wait.stop,signal), peer(p.stop.get_token(),signal);
    auto timeout=options.timeout==0ms?p.options.request_timeout:options.timeout;
    duplex::Wait wait{stop.get_token(),std::min({options.wait.deadline,parent.wait.deadline,Clock::now()+timeout})};
    { std::lock_guard lock(p.mutex); p.check_open(); } wait.check();
    Envelope frame; frame.kind="request"; frame.method=std::move(method); frame.raw_params=params.value_or("null");
    frame.params=parse_value(*frame.raw_params); frame.meta=options.meta; frame.trace=p.options.propagator->inject(parent);
    auto pending=std::make_shared<Impl::Pending>();
    Impl::Queued queued;
    std::optional<Impl::Publication> publication(std::in_place,p,wait);
    {
        std::unique_lock lock(p.mutex); p.check_open();
        if (p.pending.size()>=p.options.max_pending_requests) throw PublicError("busy","Too many outstanding requests");
        if (p.next_id==static_cast<std::uint64_t>(std::numeric_limits<std::int64_t>::max())) {
            lock.unlock(); publication.reset();
            p.end(std::make_exception_ptr(std::runtime_error("request serials exhausted")),4011,"request serials exhausted");
            throw PublicError("identifier_exhausted","Create a new peer before further calls");
        }
        frame.id=(p.role==Role::client?"c:":"s:")+std::to_string(++p.next_id);
        queued=p.encode(frame); p.pending.emplace(frame.id,pending);
    }
    auto started=p.request_started(frame,false);
    bool sent=false;
    try {
        p.enqueue(std::move(queued),wait); sent=true;
        publication.reset();
        std::unique_lock lock(p.mutex);
        while (!pending->ready) {
            p.check_open(); wait.check();
            p.cv.wait_until(lock,wait.stop,wait.deadline,[&]{return p.closed || pending->ready;});
        }
        auto error=pending->error; auto result=std::move(pending->result);
        lock.unlock();
        if (error) std::rethrow_exception(error);
        p.request_ended(frame,false,started,{}); return result.value_or(Value::null());
    } catch (...) {
        auto error=std::current_exception();
        publication.reset();
        { std::lock_guard lock(p.mutex); p.pending.erase(frame.id); }
        p.request_ended(frame,false,started,error);
        if (sent && !p.stop.stop_requested() && (stop.stop_requested() || Clock::now()>=wait.deadline)) p.cancel(frame);
        std::rethrow_exception(error);
    }
}
void Peer::emit(std::string event, Value data, CallOptions options) { emit_raw(std::move(event),stringify(data),std::move(options)); }
void Peer::emit_raw(std::string event, std::optional<std::string> data, CallOptions options) {
    name_valid(event); auto& p=*impl_;
    auto parent=options.context.value_or(RequestContext{});
    std::stop_source stop; auto signal=[&]{stop.request_stop();p.cv.notify_all();};
    std::stop_callback direct(options.wait.stop,signal), inherited(parent.wait.stop,signal), peer(p.stop.get_token(),signal);
    duplex::Wait wait{stop.get_token(),std::min(options.wait.deadline,parent.wait.deadline)};
    wait.check(); Envelope frame; frame.kind="event"; frame.event=std::move(event); frame.raw_data=data.value_or("null"); frame.data=parse_value(*frame.raw_data);
    frame.meta=options.meta; frame.trace=p.options.propagator->inject(parent);
    auto queued=p.encode(frame);
    Observation observed; observed.type="event.emitted"; observed.name=frame.event; observed.trace=frame.trace; observed.bytes=frame.raw_data->size(); p.observe(std::move(observed));
    p.enqueue(std::move(queued),wait);
}
Value Peer::call_path(const Path& path, Value params, CallOptions options) { return call(encode_path(path),std::move(params),std::move(options)); }
void Peer::emit_path(const Path& path, Value data, CallOptions options) { emit(encode_path(path),std::move(data),std::move(options)); }
void Peer::handle_path(const Path& path, Handler handler) { handle(encode_path(path),std::move(handler)); }
void Peer::handle_event_path(const Path& path, EventHandler handler) { handle_event(encode_path(path),std::move(handler)); }
void Peer::close() { impl_->end(std::make_exception_ptr(duplex::Closed()),1000,{}); }
bool Peer::ended() const { std::lock_guard lock(impl_->mutex); return impl_->closed; }
std::exception_ptr Peer::error() const { std::lock_guard lock(impl_->mutex); return impl_->failure; }
CloseInfo Peer::await_close(duplex::Wait wait) const {
    auto& p=*impl_; std::unique_lock lock(p.mutex); std::stop_callback stop(wait.stop,[&]{p.cv.notify_all();});
    while (!p.close_observed) { wait.check(); p.cv.wait_until(lock,wait.stop,wait.deadline,[&]{return p.close_observed;}); }
    return p.close_info;
}
Role Peer::role() const { return impl_->role; }
const std::string& Peer::subprotocol() const { return impl_->options.subprotocol; }
duplex::Wait Peer::context() const { return {impl_->stop.get_token()}; }
void Peer::observe(const Observation& event) const { impl_->observe(event); }

namespace {
void valid_identity(const Value& value) {
    auto invalid=[] { throw PublicError("contract_invalid","a declaration identity is an object with path and optional lowercase SHA-256 digest"); };
    if (!value.is_object() || !value.contains("path") || !is_text(value.at("path")) || value.at("path").as<std::string>().empty()) invalid();
    for (const auto& member : value.object_range()) if (member.key()!="path" && member.key()!="digest") invalid();
    try { validate_unicode(value); } catch (...) { invalid(); }
    if (value.contains("digest")) {
        if (!is_text(value.at("digest"))) invalid();
        auto digest=value.at("digest").as<std::string>();
        if (digest.size()!=64) invalid();
        for (auto c:digest) if (!((c>='0' && c<='9') || (c>='a' && c<='f'))) invalid();
    }
}
Value identity_value(std::string path,std::string digest) {
    Value value(jsoncons::json_object_arg); value["path"]=std::move(path); if (!digest.empty()) value["digest"]=std::move(digest); valid_identity(value); return value;
}
void compare_identity(const Value& expected,const Value& remote) {
    valid_identity(remote);
    if (expected.at("path")!=remote.at("path") || (expected.contains("digest") && remote.contains("digest") && expected.at("digest")!=remote.at("digest"))) {
        throw PublicError("contract_mismatch","the declaration identity for "+expected.at("path").as<std::string>()+" differs");
    }
}
}
void Peer::identity(std::string path,std::string digest) {
    auto expected=identity_value(std::move(path),std::move(digest));
    handle("identity.check",[expected=std::move(expected)](const RequestContext&,Peer&,const Value& value) { compare_identity(expected,value); return expected; });
}
void Peer::check_identity(std::string path,std::string digest,CallOptions options) {
    auto expected=identity_value(std::move(path),std::move(digest));
    Value remote;
    try { remote=call("identity.check",expected,std::move(options)); }
    catch (const PublicError& error) { if (error.code=="method_not_found") return; throw; }
    compare_identity(expected,remote);
}
} // namespace nightseam::runtime
