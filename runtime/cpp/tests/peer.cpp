#include <nightseam/runtime/peer.hpp>
#include <nightseam/duplex/ws.hpp>
#include <future>
#include <iostream>
#include <mutex>
#include <thread>
using namespace nightseam::runtime;
using namespace nightseam::duplex;
using namespace std::chrono_literals;
static void require(bool value, const char* message) { if (!value) throw std::runtime_error(message); }
static void websocket_close() {
    ws::Listener listener;
    auto dialled=ws::dial(listener.url());
    auto accepted=listener.accept(Wait::after(2s));
    PeerOptions options; options.write_timeout=1s;
    Peer server(accepted.conn,Role::server,options), client(dialled.conn,Role::client,options);
    server.handle("echo",[](const RequestContext&,Peer&,const Value& value){return value;});
    require(client.call("echo",Value(1))==Value(1),"WebSocket peer exchange");
    client.close();
    auto local=client.await_close(Wait::after(2s)), remote=server.await_close(Wait::after(2s));
    require(local.clean && remote.clean && local.code==1000 && remote.code==1000,"WebSocket chosen close reaches both peers");
}

static void raw_payload_and_metadata() {
    auto [raw,transport]=pipe();
    PeerOptions options;
    options.prepare=[](Peer& peer) {
        peer.handle_raw("echo",[](const RequestContext& ctx,Peer&,const Value&) {
            require(ctx.meta && ctx.meta->at("tenant")=="secret","metadata reaches handler");
            return ctx.raw_payload;
        });
    };
    Peer peer(transport,Role::server,options);
    std::string payload=R"({ "n":1e3, "duplicate":1, "duplicate":2, "nil":null })";
    raw->send({Kind::text,"{\"version\":1,\"kind\":\"request\",\"id\":\"c:9\",\"method\":\"echo\",\"params\":"+payload+",\"meta\":{\"tenant\":\"secret\"}}"});
    auto reply=decode_envelope(raw->receive(Wait::after(2s)).data,Role::client);
    require(reply.raw_result==payload,"raw handler preserves JSON value bytes");
    require(!reply.meta,"response does not carry metadata");
    peer.close();
}

static void pending_limit_and_observer() {
    auto [raw,transport]=pipe();
    std::mutex mutex; std::vector<Observation> seen;
    PeerOptions options; options.max_pending_requests=1;
    options.observer=[&](const Observation& event){std::lock_guard lock(mutex);seen.push_back(event);};
    Peer peer(transport,Role::client,options);
    std::stop_source stop;
    auto call=std::async(std::launch::async,[&]{CallOptions o; o.wait.stop=stop.get_token(); try { peer.call("wait",Value(1),o); } catch (const Cancelled&) {} });
    auto request=decode_envelope(raw->receive(Wait::after(1s)).data,Role::server);
    bool busy=false;
    try { peer.call("unsent"); } catch (const PublicError& e) {busy=e.code=="busy";}
    require(busy,"pending requests are bounded locally");
    stop.request_stop(); call.get();
    auto cancel=decode_envelope(raw->receive(Wait::after(1s)).data,Role::server);
    require(cancel.kind=="cancel" && cancel.id==request.id && cancel.trace.parent==request.trace.parent,"cancel correlation and trace");
    {
        std::lock_guard lock(mutex);
        std::size_t ended=seen.size(),sent=seen.size();
        for (std::size_t i=0;i<seen.size();++i) {
            require(seen[i].method!="unsent","refused call has no observer lifecycle");
            if (seen[i].type=="request.ended") { ended=i;require(seen[i].outcome=="cancelled","local cancellation outcome"); }
            if (seen[i].type=="frame.sent" && seen[i].kind=="cancel") sent=i;
        }
        require(ended<sent,"request ends before cancellation is sent");
    }
    peer.close();
}

static void cancellation_has_reserved_queue_capacity() {
    auto [raw,transport]=pipe(1<<20,1);
    PeerOptions options; options.queue_capacity=1; options.write_timeout=2s;
    Peer peer(transport,Role::client,options);
    std::stop_source stop;
    auto call=std::async(std::launch::async,[&]{CallOptions o; o.wait.stop=stop.get_token(); try { peer.call("wait",Value::null(),o); } catch (const Cancelled&) {} });
    auto request=decode_envelope(raw->receive(Wait::after(1s)).data,Role::server);
    for (int i=1;i<=3;++i) peer.emit("queued",Value(i));
    stop.request_stop();
    require(call.wait_for(500ms)==std::future_status::ready,"saturated data queue does not strand cancellation"); call.get();
    for (int i=1;i<=3;++i) {
        auto frame=decode_envelope(raw->receive(Wait::after(1s)).data,Role::server);
        require(frame.kind=="event" && frame.data==Value(i),"reserved control retains accepted frame order");
    }
    auto cancel=decode_envelope(raw->receive(Wait::after(1s)).data,Role::server);
    require(cancel.kind=="cancel" && cancel.id==request.id,"reserved cancellation delivered"); peer.close();
}

static void inbound_stall_ends_peer() {
    auto [raw,transport]=pipe();
    std::promise<void> started; std::atomic_bool stalled=false;
    PeerOptions options; options.queue_capacity=1; options.write_timeout=30ms;
    options.observer=[&](const Observation& event){if(event.type=="backpressure" && event.stalled) stalled=true;};
    options.events["block"]=[&](const RequestContext& ctx,Peer&,const Value&){ started.set_value(); while(!ctx.cancelled()) std::this_thread::sleep_for(1ms); };
    Peer peer(transport,Role::server,options);
    auto event=R"({"version":1,"kind":"event","event":"block","data":null})";
    raw->send({Kind::text,event}); started.get_future().wait();
    raw->send({Kind::text,event}); raw->send({Kind::text,event});
    auto close=peer.await_close(Wait::after(1s)); require(!close.clean && stalled,"inbound stall is observed and ends peer");
}
int main() {
    try {
        websocket_close(); raw_payload_and_metadata(); pending_limit_and_observer();
        cancellation_has_reserved_queue_capacity(); inbound_stall_ends_peer();
        auto [a,b] = pipe();
        PeerOptions options; options.request_timeout = 2s;
        options.handlers["echo"] = [](const RequestContext&, Peer&, const Value& v) { return v; };
        options.handlers["reverse"] = [](const RequestContext& ctx, Peer& p, const Value& v) {
            CallOptions o; o.context = ctx; return p.call("echo",v,o);
        };
        options.handlers["wait"] = [](const RequestContext& ctx, Peer&, const Value&) {
            while (!ctx.cancelled()) std::this_thread::sleep_for(1ms);
            ctx.check(); return Value::null();
        };
        Peer server(b,Role::server,options), client(a,Role::client,options);
        require(client.call("reverse",parse_value("{\"optional\":null,\"empty\":[],\"number\":1e3}")) == parse_value("{\"optional\":null,\"empty\":[],\"number\":1e3}"), "reverse/presence");
        std::stop_source stop;
        CallOptions cancellation; cancellation.wait.stop = stop.get_token();
        auto call = std::async(std::launch::async,[&] {
            try { client.call("wait",Value::null(),cancellation); return false; }
            catch (const Cancelled&) { return true; }
        });
        stop.request_stop(); require(call.get(),"call cancellation");
        CallOptions timeout; timeout.timeout = 20ms;
        bool deadline = false;
        try { client.call("wait",Value::null(),timeout); } catch (const DeadlineExceeded&) { deadline = true; }
        require(deadline,"deadline is distinct from cancellation");
        require(client.call("echo",Value(42)) == Value(42),"late reply does not poison next call");
        server.identity("test",std::string(64,'a')); client.check_identity("test");
        bool mismatch = false;
        try { client.check_identity("wrong"); } catch (const PublicError& e) { mismatch = e.code == "contract_mismatch"; }
        require(mismatch,"identity mismatch");
        client.close(); require(client.await_close(Wait::after(1s)).code == 1000,"chosen close");

        // A raw transport proves a cancelled incoming request is retained until
        // its handler returns, and the response carries the original trace.
        auto [raw,transport] = pipe();
        std::promise<void> started, release;
        auto released = release.get_future().share();
        PeerOptions held;
        held.handlers["held"] = [&](const RequestContext&, Peer&, const Value&) { started.set_value(); released.wait(); return Value(7); };
        Peer peer(transport,Role::server,held);
        raw->send({Kind::text,R"({"version":1,"kind":"request","id":"c:1","method":"held","params":null,"tracestate":"verbatim"})"});
        started.get_future().wait();
        raw->send({Kind::text,R"({"version":1,"kind":"cancel","id":"c:1"})"});
        bool waiting = false;
        try { raw->receive(Wait::after(30ms)); } catch (const DeadlineExceeded&) { waiting = true; }
        require(waiting,"cancel waits for handler return"); release.set_value();
        auto response = decode_envelope(raw->receive(Wait::after(1s)).data,Role::client);
        require(response.error && response.error->code == "cancelled", "cancelled handler result");
        require(response.trace.state == "verbatim","trace echo"); peer.close();
        std::cout << "peer lifecycle invariants passed\n";
    } catch (const std::exception& error) { std::cerr << error.what() << '\n'; return 1; }
}
