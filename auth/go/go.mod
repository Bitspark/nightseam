module github.com/Bitspark/nightseam/auth/go

go 1.26.0

require (
	github.com/Bitspark/archon/core/go v0.7.0
	github.com/Bitspark/archon/sdk/go v0.7.0
	github.com/Bitspark/nightseam v0.5.0
)

// The profile is built and tested against the checkout it lives in. A
// consumer ignores a dependency's replace and gets the version required
// above, which the release cuts a tag for beside the root module's.
replace github.com/Bitspark/nightseam => ../..
