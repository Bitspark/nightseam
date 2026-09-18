module github.com/Bitspark/nightseam/otel/go

go 1.25.0

require (
	github.com/Bitspark/nightseam v0.2.0
	go.opentelemetry.io/otel v1.46.0
	go.opentelemetry.io/otel/sdk v1.46.0
	go.opentelemetry.io/otel/trace v1.46.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/coder/websocket v1.8.15 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/metric v1.46.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
)

// The adapter is built and tested against the checkout it lives in. A
// consumer ignores a dependency's replace and gets the version required
// above, which the release cuts a tag for beside the root module's.
replace github.com/Bitspark/nightseam => ../..
