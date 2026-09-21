// The example is a consumer checkout: it requires the published module at
// the released version with no `replace`, which is what makes it the
// consumer scripts/smoke-packed.mjs installs from the packed shape and
// scripts/smoke-registry.mjs installs from the tag. In a clone of this
// repository it therefore resolves against nothing until one of those lays
// a proxy or the tag exists; that is the point of it.
module example.com/probe

go 1.26.0

require github.com/Bitspark/nightseam v0.5.0

require (
	github.com/Bitspark/bitwire v0.1.0 // indirect
	github.com/coder/websocket v1.8.15 // indirect
	github.com/go-json-experiment/json v0.0.0-20260820222146-c27c302e5fc3 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	github.com/spf13/cobra v1.10.2 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	golang.org/x/text v0.42.0 // indirect
)

// The generator, as a Go tool: `go tool nightseam generate` renders
// api/contracts into the packages committed beside it.
tool github.com/Bitspark/nightseam/cmd/nightseam
