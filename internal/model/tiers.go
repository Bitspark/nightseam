package model

import "strings"

// Tier is one tier of declaration: the file it is declared in, its rank
// among the tiers — a declaration refers to its own tier or a lower one —
// the sections its file may carry beyond the types and imports every tier
// carries, and the built-in family that declares the tier's own vocabulary.
//
// A family that has the tier file carries that built-in's types implicitly,
// with no imports line: the protocol tier's built-in, `duplex`, is carried
// — its types are the family's own, since a family's envelope is a message
// of that family.
type Tier struct {
	Name     string
	Rank     int
	File     string
	Sections []string
	Builtin  string // the built-in family whose types the tier carries
}

// The tier files, lowest first.
const (
	ModelFile    = "model.json"
	ProtocolFile = "protocol.json"
)

// Tiers is every tier, lowest first. A concern is added here, with its
// shape schema in load, its checker in check and its field on Family.
var Tiers = []Tier{
	{Name: "model", Rank: 0, File: ModelFile, Sections: []string{"nightseam"}},
	{Name: "protocol", Rank: 1, File: ProtocolFile, Sections: []string{"profile", "parameters", "server", "client", "errors"}, Builtin: "duplex"},
}

// TierOf finds a tier by its file name, whether the file is a consumer's or
// a built-in's — nightseam:duplex/model.json is the model tier too.
func TierOf(file string) (Tier, bool) {
	if i := strings.LastIndexByte(file, '/'); i >= 0 {
		file = file[i+1:]
	}
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

// The types every family with a protocol carries from the built-in
// `duplex` family, so that another family may hold one of its messages or
// a handle to a channel that speaks it. A family may not declare either
// itself; where it names one it writes duplex.Envelope, the built-in that
// declares it.
const (
	EnvelopeType = "Envelope"
	HandleType   = "Handle"
)

// TierRoles is every tier a family parameter may be `of` — the tiers above
// the model, which every family has — in tier order.
func TierRoles() []string {
	var roles []string
	for _, tier := range Tiers {
		if tier.Rank > 0 {
			roles = append(roles, tier.Name)
		}
	}
	return roles
}

// IsTierRole reports whether a name is a tier a family may be said to
// carry: what a family parameter is `of`.
func IsTierRole(name string) bool {
	for _, role := range TierRoles() {
		if role == name {
			return true
		}
	}
	return false
}

// Carried reports whether a type name is one a family carries from the
// built-in family of a tier it has, rather than one it declares.
func Carried(name string) bool { return name == EnvelopeType || name == HandleType }
