package model

import "github.com/Bitspark/nightseam/internal/diag"

// Tier is one tier of declaration: the file it is declared in, its rank
// among the tiers — a declaration refers to its own tier or a lower one —
// and the sections its file may carry beyond the types and imports every
// tier carries.
type Tier struct {
	Name     string
	Rank     int
	File     string
	Sections []string
}

// The tier files, lowest first.
const (
	ModelFile    = "model.json"
	ProtocolFile = "protocol.json"
	SessionFile  = "session.json"
)

// Tiers is every tier, lowest first. A concern is added here, with its
// shape schema in load, its checker in check and its field on Family.
var Tiers = []Tier{
	{Name: "model", Rank: 0, File: ModelFile, Sections: []string{"nightseam"}},
	{Name: "protocol", Rank: 1, File: ProtocolFile, Sections: []string{"profile", "parameters", "server", "client", "errors"}},
	{Name: "session", Rank: 2, File: SessionFile, Sections: []string{"decides", "asks", "conversation", "extensions"}},
}

// Common are the sections every tier file may carry.
var Common = []string{"imports", "types"}

// TierOf finds a tier by its file name.
func TierOf(file string) (Tier, bool) {
	for _, tier := range Tiers {
		if tier.File == file {
			return tier, true
		}
	}
	return Tier{}, false
}

// Rank is the rank of the tier a file belongs to; a file no tier owns
// ranks below every tier.
func Rank(file string) int {
	if tier, ok := TierOf(file); ok {
		return tier.Rank
	}
	return -1
}

// TierName is the name of the tier of a rank.
func TierName(rank int) string {
	if rank >= 0 && rank < len(Tiers) {
		return Tiers[rank].Name
	}
	return "unknown"
}

// OverrideFile is the override file of a target: <target>.json.
func OverrideFile(target string) string { return target + ".json" }

// The types every family with a protocol carries, so that another family
// may hold one of its messages or a handle to a channel that speaks it. A
// family may not declare either itself.
const (
	EnvelopeType = "Envelope"
	HandleType   = "Handle"
)

// SessionRole is the role a family carries when it has a session tier: what
// a parameter of session binds to.
const SessionRole = "session"

// Injected are the types every family with a protocol carries, located in
// the protocol tier. An envelope is one nightseam.duplex/1 message: version
// and kind, the id that correlates a response or a cancel with its request,
// the method a request names and its params, a response's result or error,
// the event an event frame names and its data, and the W3C Trace Context
// the frame carries — traceparent and tracestate, verbatim, so that a slot
// of a family's envelope accepts what the runtimes now propagate. A handle
// is a reference to a channel on the carrying connection.
func Injected() map[string]*Type {
	at := func(string) diag.Location { return diag.Location{File: ProtocolFile} }
	field := func(name, primitive string, required bool) Field {
		return Field{Name: name, Type: Primitive(primitive), Required: required}
	}
	return map[string]*Type{
		EnvelopeType: {Name: EnvelopeType, Kind: KindRecord, At: at(EnvelopeType), Fields: []Field{
			field("version", "integer", true), field("kind", "string", true), field("id", "string", false),
			field("method", "string", false), field("params", "json", false), field("result", "json", false),
			field("error", "json", false), field("event", "string", false), field("data", "json", false),
			field("traceparent", "string", false), field("tracestate", "string", false),
		}},
		HandleType: {Name: HandleType, Kind: KindRecord, At: at(HandleType), Fields: []Field{field("channel", "integer", true)}},
	}
}

// IsInjected reports whether a type name is one every family carries.
func IsInjected(name string) bool { return name == EnvelopeType || name == HandleType }
