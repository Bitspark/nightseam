package check

import (
	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/model"
)

// Session holds governance against the complete inherited operation
// surface. A session has one conversation source; extending a side may
// repeat that source but cannot replace it with a different event or path.
func Session(f *analysis.Family) []diag.Diagnostic {
	c := newChecker(f)
	s, p := f.Session, f.Protocol
	if s == nil || p == nil {
		return nil
	}
	methods := map[string]bool{} // name -> server side, as Protocol.Method resolves it
	events := map[string]bool{}
	type source struct {
		family *analysis.Family
		server bool
		at     diag.Location
	}
	var inherited []source
	for _, server := range []bool{true, false} {
		addOperations := func(side *model.Side) {
			for _, method := range side.Methods {
				if _, exists := methods[method.Name]; !exists {
					methods[method.Name] = server
				}
			}
			for _, event := range side.Events {
				events[event.Name] = true
			}
		}
		side := sessionSide(f, server)
		seen := map[*analysis.Family]bool{f: true}
		for i, name := range side.Extends {
			at := side.At.Sub("extends", i)
			walkSessionSide(f.Imported[name.Name], server, seen, func(base *analysis.Family, side *model.Side) {
				addOperations(side)
				if base.Session != nil {
					inherited = append(inherited, source{base, server, at})
				}
			})
		}
		addOperations(side)
	}
	for i, name := range s.Decides {
		if _, ok := methods[name]; !ok {
			c.Addf(s.At.Sub("decides", i), "unknown_operation", "Decides names %s, which is not a method of this family.", name)
		}
	}
	for i, name := range s.Asks {
		server, ok := methods[name]
		switch {
		case !ok:
			c.Addf(s.At.Sub("asks", i), "unknown_operation", "Asks names %s, which is not a method of this family.", name)
		case server:
			c.Addf(s.At.Sub("asks", i), "invalid_side", "Asks names %s, which the client sends; an asking method is one the server sends, on the client side.", name)
		}
	}
	var first *model.Conversation
	var firstFamily string
	conversations := map[model.Conversation]bool{}
	conversation := func(value *model.Conversation, family string, at diag.Location) {
		if conversations[*value] {
			return
		}
		conversations[*value] = true
		if first == nil {
			first, firstFamily = value, family
			return
		}
		c.Addf(at, "incompatible_governance", "Conversation %s/%s from %s conflicts with %s/%s from %s: an extended session has one conversation source.", value.Event, value.Path, family, first.Event, first.Path, firstFamily)
	}
	for _, source := range inherited {
		value := source.family.Session.Conversation
		if value == nil {
			continue
		}
		// Only the selected side brings its event and its governance. The
		// declaring family may itself name an event it inherited.
		found := false
		walkSessionSide(source.family, source.server, map[*analysis.Family]bool{}, func(_ *analysis.Family, side *model.Side) {
			for _, event := range side.Events {
				found = found || event.Name == value.Event
			}
		})
		if found {
			conversation(value, source.family.Name, source.at)
		}
	}
	if value := s.Conversation; value != nil {
		if !events[value.Event] {
			c.Addf(s.At.Sub("conversation", "event"), "unknown_operation", "The conversation arrives in %s, which is not an event of this family.", value.Event)
		} else {
			conversation(value, f.Name, s.At.Sub("conversation"))
		}
	}
	return c.Diagnostics
}

func sessionSide(f *analysis.Family, server bool) *model.Side {
	if server {
		return &f.Protocol.Server
	}
	return &f.Protocol.Client
}

// Walk each declaration once on one side, inherited first. Invalid bases
// and cycles belong to Protocol's diagnostics and cannot trap this walk.
func walkSessionSide(f *analysis.Family, server bool, seen map[*analysis.Family]bool, visit func(*analysis.Family, *model.Side)) {
	if f == nil || f.Protocol == nil || seen[f] {
		return
	}
	seen[f] = true
	side := sessionSide(f, server)
	for _, name := range side.Extends {
		walkSessionSide(f.Imported[name.Name], server, seen, visit)
	}
	visit(f, side)
}
