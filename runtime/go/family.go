package runtime

import "encoding/json"

// Family binds a family where a generic package is instantiated: its name
// and the validator of its wire types.
//
// Go has no associated types, so a contract parameter becomes one type
// parameter per slot kind — SE and SH for a parameter S. Nothing in the
// generated code relates the two, so on their own they could be bound to one
// family's Envelope and another's Handle, which is a pairing no binding of
// the contract produces and no TypeScript peer can express. Taking the
// binding as one argument pairs them again: an entry point that takes
// Family[SE, SH] infers both from it, so the two come from one family or the
// call does not compile.
//
// What fills a slot is validated by the bound family's own codec, which the
// instantiated types carry: Frame[probeprotocol.Envelope] marshals its
// message through probe's MarshalJSON. Validate is the same check over raw
// bytes, for a consumer that holds the binding and not the type.
type Family[E, H any] struct {
	Name     string
	Validate func(typeName string, data []byte) error
}

// Opaque binds a parameter to no family: what fills its slots passes through
// as raw JSON and is not validated here, which is what a relay wants.
func Opaque() Family[json.RawMessage, json.RawMessage] {
	return Family[json.RawMessage, json.RawMessage]{Validate: func(string, []byte) error { return nil }}
}
