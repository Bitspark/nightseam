// Package auth is the optional authority profile of Nightseam: the
// authenticated connection — the audience a connection binds to, the
// possession proof and the challenge/prove exchange that establishes one
// immutable context per connection, the decision at every protected call,
// and the bootstrap that turns a login into a grant — and the exposure that
// binds a policy of one treatment per declared member to a generated
// surface and decides each call at dispatch and again at the owner's
// effect. What it verifies is specified in docs/auth/connection.md and
// docs/auth/exposure.md and held to conformance/tables/auth-boot.json and
// auth-exposure.json; the grant it verifies against is package grant.
//
// It is a module of its own so that github.com/Bitspark/nightseam pulls in
// no identity layer: a consumer that adopts no authority profile installs
// nothing for one. It requires the root module at its own version and is
// released by a tag of its own, auth/go/vX.Y.Z, cut beside vX.Y.Z. Its
// identity layer is Archon — github.com/Bitspark/archon/core/go for the
// Ed25519 floor and the envelope, sdk/go for possession and login — and it
// depends on nothing else beside the runtime it composes onto.
package auth
