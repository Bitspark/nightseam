#include <nightseam/runtime/peer.hpp>
#include <fstream>
#include <future>
#include <iostream>
#include <thread>
using namespace nightseam::runtime;
using namespace nightseam::duplex;
using namespace std::chrono_literals;
static void require(bool value,const char* message) { if(!value) throw std::runtime_error(message); }
int main(int argc,char** argv) {
    try {
        require(argc==2,"table directory required");
        std::ifstream input(std::string(argv[1])+"/serials.json");
        std::string json((std::istreambuf_iterator<char>(input)),{});
        auto table=parse_value(json);
        for(const auto& row:table.at("rows").array_range()) {
            auto [raw,carrier]=pipe();
            PeerOptions options; options.handlers["echo"]=[](const RequestContext&,Peer&,const Value& value){return value;};
            Peer peer(carrier,Role::server,options);
            auto before=row.at("before").as<std::string>();
            raw->send({Kind::text,before});
            if(parse_value(before).at("kind").as<std::string>()=="request") raw->receive(Wait::after(1s));
            raw->send({Kind::text,row.at("frame").as<std::string>()});
            if(row.at("valid").as<bool>()) {
                raw->receive(Wait::after(1s)); require(!peer.ended(),"valid serial closed peer");
            } else require(peer.await_close(Wait::after(1s)).code==4011,"non-increasing serial admitted");
            peer.close();
        }
        {
            auto [raw,carrier]=pipe();
            std::promise<void> reserved,release;
            auto released=release.get_future().share();
            PeerOptions options; options.max_pending_requests=1;
            options.observer=[&](const Observation& event) {
                if(event.type=="request.started" && !event.incoming) { reserved.set_value(); released.wait(); }
            };
            Peer peer(carrier,Role::client,options);
            auto first=std::async(std::launch::async,[&]{try {peer.call("first");} catch(const std::exception&) {}});
            reserved.get_future().wait();
            bool busy=false;
            try {peer.call("over-budget");} catch(const PublicError& error) {busy=error.code=="busy";}
            release.set_value(); peer.close(); first.get();
            require(busy,"publication waiters escaped the pending budget");
        }
        auto [raw,carrier]=pipe();
        std::promise<void> reserved,release;
        auto released=release.get_future().share();
        PeerOptions options; options.queue_capacity=1;
        options.observer=[&](const Observation& event) {
            if(event.type=="request.started" && !event.incoming && event.id=="c:1") { reserved.set_value(); released.wait(); }
        };
        Peer peer(carrier,Role::client,options);
        auto call=[&]{ try {peer.call("probe");} catch(const std::exception&) {} };
        auto first=std::async(std::launch::async,call);
        reserved.get_future().wait();
        auto second=std::async(std::launch::async,call);
        std::this_thread::sleep_for(50ms); release.set_value();
        auto one=decode_envelope(raw->receive(Wait::after(1s)).data,Role::server);
        auto two=decode_envelope(raw->receive(Wait::after(1s)).data,Role::server);
        peer.close(); first.get(); second.get();
        require(one.id=="c:1" && two.id=="c:2","request publication inverted serials");
        std::cout << "request serial invariants passed\n";
    } catch(const std::exception& e) { std::cerr << e.what() << '\n'; return 1; }
}
