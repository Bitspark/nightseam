package io.nightseam.conformance;

import io.nightseam.runtime.Json;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Duration;
import java.util.List;
import java.util.Map;

public final class RecordedWireTest {
    public static void main(String[] args) throws Exception {
        Path checkout = args.length == 0 ? Path.of(".") : Path.of(args[0]);
        var scenario = (Map<?,?>)Json.parse(Files.readAllBytes(checkout.resolve("conformance/scenarios/peer/recorded-wire-head-and-order.json")));
        var steps = (List<?>)scenario.get("steps");
        Object expected = ((Map<?,?>)steps.getFirst()).get("expect");
        for (int repetition = 0; repetition < 10; repetition++) {
            Object observed = Json.parse(Json.stringify(RecordedWire.witness(Duration.ofSeconds(3))));
            if (!expected.equals(observed)) throw new AssertionError("recorded witness: " + Json.stringify(observed));
        }
    }
}
