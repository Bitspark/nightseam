package kernel

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
)

func TestExplicitFamilyArguments(t *testing.T) {
	for name, tc := range map[string]struct {
		filler, kind, nesting                       string
		family, local, typeParameter, absent, mixed bool
		want                                        string
	}{
		"family equal bound":                            {family: true},
		"family mixed arguments":                        {family: true, mixed: true},
		"owned mixed arguments":                         {mixed: true},
		"alias owned parameter":                         {},
		"record owned parameter":                        {kind: "record"},
		"union owned parameter":                         {kind: "union"},
		"local target":                                  {local: true},
		"inline captures owned parameter":               {kind: "record", nesting: "inline"},
		"collection captures owned parameter":           {kind: "record", nesting: "collection"},
		"nested type argument captures owned parameter": {nesting: "application"},
		"owned type cannot fill family":                 {typeParameter: true, want: "invalid_filler"},
		"family type cannot fill family":                {family: true, typeParameter: true, want: "invalid_filler"},
		"unknown parameter":                             {absent: true, filler: `"Missing"`, want: "unresolved_parameter"},
		"named concrete type cannot fill family":        {absent: true, filler: `"Payload"`, want: "invalid_filler"},
		"primitive cannot fill family":                  {absent: true, filler: `"string"`, want: "invalid_filler"},
		"concrete family with a protocol":               {absent: true, filler: `"probe"`},
		"another concrete family with a protocol":       {absent: true, filler: `"plain"`},
		"missing concrete family":                       {absent: true, filler: `"missing"`, want: "unresolved_type"},
		"self family":                                   {absent: true, filler: `"x"`, want: "self_slot"},
	} {
		t.Run(name, func(t *testing.T) {
			const bound, required = "protocol", "protocol"
			parameter := map[string]any{"name": "S", "of": bound}
			if tc.typeParameter {
				delete(parameter, "of")
			}
			var parameters []any
			if !tc.absent {
				parameters = []any{parameter}
			}
			box := map[string]any{"kind": "record", "parameters": []any{map[string]any{"name": "P", "of": required}}, "fields": []any{map[string]any{"name": "frame", "type": "P.Envelope"}}}
			if tc.mixed {
				parameters = append(parameters, map[string]any{"name": "T"})
				box["parameters"] = append(box["parameters"].([]any), map[string]any{"name": "Value"})
				box["fields"] = append(box["fields"].([]any), map[string]any{"name": "value", "type": "Value"})
			}
			target := "peer.Box"
			if tc.local {
				target = "Box"
			}
			filler := tc.filler
			if filler == "" {
				filler = `"S"`
			}
			extra := ""
			if tc.mixed {
				extra = `,"Value":"T"`
			}
			expression := fmt.Sprintf(`{"apply":%q,"with":{"P":%s%s}}`, target, filler, extra)
			path := "protocol.json#/types/Use"
			if tc.nesting == "application" {
				expression = `{"apply":"Wrap","with":{"Element":` + expression + `}}`
			}
			if tc.nesting == "collection" {
				expression = `{"array":{"nullable":` + expression + `}}`
			}
			if tc.nesting == "inline" {
				expression = `{"kind":"record","fields":[{"name":"inner","type":` + expression + `}]}`
			}
			use := map[string]any{"kind": "alias", "type": json.RawMessage(expression)}
			path += "/type"
			if tc.kind == "record" {
				use = map[string]any{"kind": "record", "fields": []any{map[string]any{"name": "value", "type": json.RawMessage(expression)}}}
				path = "protocol.json#/types/Use/fields/0/type"
			} else if tc.kind == "union" {
				use = map[string]any{"kind": "union", "tag": "kind", "variants": map[string]any{"item": json.RawMessage(expression)}}
				path = "protocol.json#/types/Use/variants/item"
			}
			if tc.nesting == "application" {
				path += "/with/Element"
			}
			if tc.nesting == "inline" {
				path += "/fields/0/type"
			}
			if !tc.family && len(parameters) > 0 {
				use["parameters"] = parameters
			}
			types := map[string]any{
				"Use":     use,
				"Payload": map[string]any{"kind": "record", "fields": []any{}},
				"Wrap":    map[string]any{"kind": "record", "parameters": []any{map[string]any{"name": "Element"}}, "fields": []any{map[string]any{"name": "value", "type": "Element"}}},
			}
			if tc.local {
				types["Box"] = box
			}
			caller := map[string]any{"profile": "nightseam.duplex/1", "types": types}
			if tc.family {
				caller["parameters"] = parameters
			}
			encode := func(value any) []byte {
				data, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				return data
			}
			files := fstest.MapFS{
				"api/contracts/peer/model.json":     {Data: []byte(`{"nightseam":2}`)},
				"api/contracts/peer/protocol.json":  {Data: encode(map[string]any{"profile": "nightseam.duplex/1", "types": map[string]any{"Box": box}})},
				"api/contracts/x/model.json":        {Data: []byte(`{"nightseam":2,"imports":["peer","probe","plain"]}`)},
				"api/contracts/x/protocol.json":     {Data: encode(caller)},
				"api/contracts/probe/model.json":    {Data: []byte(`{"nightseam":2}`)},
				"api/contracts/probe/protocol.json": {Data: []byte(`{"profile":"nightseam.duplex/1"}`)},
				"api/contracts/plain/model.json":    {Data: []byte(`{"nightseam":2}`)},
				"api/contracts/plain/protocol.json": {Data: []byte(`{"profile":"nightseam.duplex/1"}`)},
			}
			world := Load(files, "api/contracts", nil)
			for family, problems := range world.Problems {
				if len(problems) != 0 {
					t.Fatalf("%s failed schemas: %v", family, problems)
				}
			}
			if problems := Validate(world, "peer"); len(problems) != 0 {
				t.Fatalf("invalid target: %v", problems)
			}
			var got []string
			for _, d := range Validate(world, "x") {
				got = append(got, d.Code+"@"+d.At().String())
			}
			want := ""
			if tc.want != "" {
				want = tc.want + "@" + path + "/with/P"
			}
			if actual := strings.Join(got, " "); actual != want {
				t.Fatalf("got %q, want %q", actual, want)
			}
		})
	}
}
