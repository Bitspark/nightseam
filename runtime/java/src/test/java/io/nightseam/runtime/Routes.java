package io.nightseam.runtime;
import dev.bitspark.bitwire.Endpoint;
import io.nightseam.duplex.Dispatcher;
import java.util.IdentityHashMap;
final class Routes {
    private static final IdentityHashMap<Endpoint,Dispatcher> routers = new IdentityHashMap<>();
    static synchronized Dispatcher of(Endpoint endpoint) { return routers.computeIfAbsent(endpoint, Dispatcher::new); }
}
