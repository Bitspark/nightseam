package kernel

import (
	"strings"
	"testing"
	"testing/fstest"
)

// Resolve through the real schemas and neutral checker, so a target's
// temporary refusal of extended sides cannot hide a governance defect.
func TestInheritedSessionGovernance(t *testing.T) {
	type family map[string]string
	protocol := func(body string) string { return `{"profile":"nightseam.duplex/1",` + body + `}` }
	child := func(sides, session string) family {
		out := family{
			"model.json":    `{"nightseam":2,"imports":["a","b","middle","left","right"],"types":{}}`,
			"protocol.json": protocol(sides),
		}
		if session != "" {
			out["session.json"] = session
		}
		return out
	}
	const both = `"server":{"extends":["a"]},"client":{"extends":["a"]}`
	const references = `{"decides":["runA"],"asks":["askA"],"conversation":{"event":"startedA","path":"id"}}`
	const distinct = `"server":{"extends":["a","b"]}`
	for name, tc := range map[string]struct {
		child         family
		middleSession string
		want          string
	}{
		"inherited references":                          {child: child(both, references)},
		"transitive references":                         {child: child(`"server":{"extends":["middle"]},"client":{"extends":["middle"]}`, references)},
		"same conversation declared again":              {child: child(both, `{"conversation":{"event":"startedA","path":"id"}}`)},
		"same conversation through intermediate":        {child: child(`"server":{"extends":["middle"]}`, `{}`), middleSession: `{"conversation":{"event":"startedA","path":"id"}}`},
		"same source through diamond":                   {child: child(`"server":{"extends":["left","right"]}`, `{}`)},
		"decides can name inherited client method":      {child: child(`"client":{"extends":["a"]}`, `{"decides":["askA"]}`)},
		"unselected side contributes no conversation":   {child: child(`"server":{"extends":["b"]},"client":{"extends":["a"]}`, `{}`)},
		"protocol-only child has no session governance": {child: child(distinct, "")},
		"conflicting inherited conversations":           {child: child(distinct, `{}`), want: "incompatible_governance@protocol.json#/server/extends/1"},
		"conflicting own conversation":                  {child: child(`"server":{"extends":["a"],"events":{"own":{"type":"json"}}}`, `{"conversation":{"event":"own","path":"id"}}`), want: "incompatible_governance@session.json#/conversation"},
		"conflicting own path":                          {child: child(both, `{"conversation":{"event":"startedA","path":"otherId"}}`), want: "incompatible_governance@session.json#/conversation"},
		"conflict within transitive base":               {child: child(`"server":{"extends":["middle"]}`, `{}`), middleSession: `{"conversation":{"event":"startedA","path":"otherId"}}`, want: "incompatible_governance@protocol.json#/server/extends/0"},
		"conflict on client side":                       {child: child(`"server":{"extends":["a"]},"client":{"extends":["b"]}`, `{}`), want: "incompatible_governance@protocol.json#/client/extends/0"},
		"own conflict against client inheritance":       {child: child(`"server":{"events":{"own":{"type":"json"}}},"client":{"extends":["b"]}`, `{"conversation":{"event":"own","path":"id"}}`), want: "incompatible_governance@session.json#/conversation"},
		"asks still refuses server method":              {child: child(both, `{"asks":["runA"]}`), want: "invalid_side@session.json#/asks/0"},
		"unselected client method is unknown":           {child: child(`"server":{"extends":["a"]}`, `{"asks":["askA"]}`), want: "unknown_operation@session.json#/asks/0"},
		"unselected event is unknown":                   {child: child(`"client":{"extends":["a"]}`, `{"conversation":{"event":"startedA","path":"id"}}`), want: "unknown_operation@session.json#/conversation/event"},
		"unknown references remain invalid":             {child: child(both, `{"decides":["missing"],"asks":["missing"],"conversation":{"event":"missing","path":"id"}}`), want: "unknown_operation@session.json#/asks/0 unknown_operation@session.json#/conversation/event unknown_operation@session.json#/decides/0"},
	} {
		t.Run(name, func(t *testing.T) {
			middleSession := tc.middleSession
			if middleSession == "" {
				middleSession = `{}`
			}
			families := map[string]family{
				"a": {
					"model.json":    `{"nightseam":2,"types":{}}`,
					"protocol.json": protocol(`"server":{"methods":{"runA":{"result":"string"}},"events":{"startedA":{"type":"json"}}},"client":{"methods":{"askA":{"result":"string"}}}`),
					"session.json":  references,
				},
				"b": {
					"model.json":    `{"nightseam":2,"types":{}}`,
					"protocol.json": protocol(`"server":{"events":{"startedB":{"type":"json"}}},"client":{"events":{"startedB":{"type":"json"}}}`),
					"session.json":  `{"conversation":{"event":"startedB","path":"otherId"}}`,
				},
				"middle": {"model.json": `{"nightseam":2,"imports":["a"]}`, "protocol.json": protocol(both), "session.json": middleSession},
				"left":   {"model.json": `{"nightseam":2,"imports":["a"]}`, "protocol.json": protocol(`"server":{"extends":["a"]}`), "session.json": `{}`},
				"right":  {"model.json": `{"nightseam":2,"imports":["a"]}`, "protocol.json": protocol(`"server":{"extends":["a"]}`), "session.json": `{}`},
				"x":      tc.child,
			}
			files := fstest.MapFS{}
			for name, declarations := range families {
				for file, data := range declarations {
					files["api/contracts/"+name+"/"+file] = &fstest.MapFile{Data: []byte(data)}
				}
			}
			world := Load(files, "api/contracts", nil)
			for name, problems := range world.Problems {
				if len(problems) > 0 {
					t.Fatalf("%s failed schema loading: %v", name, problems)
				}
			}
			var got []string
			for _, d := range Validate(world, "x") {
				got = append(got, d.Code+"@"+d.At().String())
			}
			if actual := strings.Join(got, " "); actual != tc.want {
				t.Fatalf("got %q, want %q", actual, tc.want)
			}
		})
	}
}
