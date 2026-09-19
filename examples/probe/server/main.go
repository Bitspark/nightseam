// Command server serves the probe family over a WebSocket. The behavior is
// in api/impl/probe, where `nightseam init probe` wrote it; everything
// between that and the wire — the envelope, correlation, cancellation,
// backpressure, the validator that holds every frame to the declaration —
// is the generated binding and the runtime beneath it.
package main

import (
	"context"
	"log"
	"net/http"
	"os"

	binding "example.com/probe/api/go/probe-binding"
	probe "example.com/probe/api/impl/probe"
	runtime "github.com/Bitspark/nightseam/runtime/go"
)

func main() {
	address := os.Getenv("PROBE_ADDRESS")
	if address == "" {
		address = "127.0.0.1:8080"
	}
	// Authentication and the origin policy are the consumer's to decide, and
	// the runtime refuses a server that decides neither. An example on the
	// loopback accepts everyone; a deployment does not.
	handler, err := binding.NewHandler(probe.Handler{}, runtime.ServerOptions{
		Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil },
		CheckOrigin:  func(*http.Request) bool { return true },
	})
	if err != nil {
		log.Fatal(err)
	}
	http.Handle("/probe", handler)
	log.Printf("probe listening on ws://%s/probe", address)
	log.Fatal(http.ListenAndServe(address, nil))
}
