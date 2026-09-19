package kernel

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
)

// The schemas and neutral checker must agree on the two parameter kinds,
// before a target's refusal of a new form can hide an unbound type slot.
func TestImportedGenericBindings(t *testing.T) {
	const typeSlot = `[{"name":"T"}]`
	const familySlot = `[{"name":"P","of":"protocol"}]`
	const sessionSlot = `[{"name":"P","of":"session"}]`
	const mixedSlots = `[{"name":"T"},{"name":"P","of":"protocol"}]`
	const record = `"kind":"record","fields":[{"name":"item","type":%s}]`
	peer := func(parameters, body string, onFamily bool) string {
		if onFamily {
			return `{"profile":"nightseam.duplex/1","parameters":` + parameters + `,"types":{"Box":{` + body + `}}}`
		}
		return `{"profile":"nightseam.duplex/1","types":{"Box":{"parameters":` + parameters + `,` + body + `}}}`
	}
	plainRecord := func(parameters, item string, onFamily bool) string {
		return peer(parameters, fmt.Sprintf(record, item), onFamily)
	}
	const oneFamily = `[{"name":"S","of":"protocol"}]`
	const oneSession = `[{"name":"S","of":"session"}]`
	const twoFamilies = `[{"name":"S","of":"protocol"},{"name":"R","of":"protocol"}]`
	const onlyType = `[{"name":"T"}]`
	const mixedCaller = `[{"name":"S","of":"protocol"},{"name":"U"}]`
	for name, tc := range map[string]struct {
		peer, parameters, expression string
		valid                        bool
	}{
		"record type slot":                                 {peer: plainRecord(typeSlot, `"T"`, false)},
		"alias type slot":                                  {peer: peer(typeSlot, `"kind":"alias","type":"T"`, false)},
		"union type slot":                                  {peer: peer(typeSlot, `"kind":"union","tag":"kind","variants":{"item":"T"}`, false)},
		"family type slot":                                 {peer: plainRecord(typeSlot, `"T"`, true)},
		"family type in alias":                             {peer: peer(typeSlot, `"kind":"alias","type":"T"`, true)},
		"family type in union":                             {peer: peer(typeSlot, `"kind":"union","tag":"kind","variants":{"item":"T"}`, true)},
		"nested family type":                               {peer: plainRecord(typeSlot, `{"array":{"map":{"nullable":"T"}}}`, true)},
		"inline family type capture":                       {peer: plainRecord(typeSlot, `{"kind":"record","fields":[{"name":"value","type":"T"}]}`, true)},
		"named alias family capture":                       {peer: `{"profile":"nightseam.duplex/1","parameters":[{"name":"T"}],"types":{"Inner":{"kind":"alias","type":"T"},"Box":{"kind":"record","fields":[{"name":"value","type":"Inner"}]}}}`},
		"mixed declaration slots":                          {peer: plainRecord(mixedSlots, `{"kind":"record","fields":[{"name":"value","type":"T"},{"name":"frame","type":"P.Envelope"}]}`, false)},
		"mixed family slots":                               {peer: plainRecord(mixedSlots, `{"kind":"record","fields":[{"name":"value","type":"T"},{"name":"frame","type":"P.Envelope"}]}`, true)},
		"same named caller type is not implicit binding":   {peer: plainRecord(typeSlot, `"T"`, true), parameters: onlyType},
		"zero caller parameters":                           {peer: plainRecord(familySlot, `"P.Envelope"`, true), parameters: `[]`},
		"two caller family parameters":                     {peer: plainRecord(familySlot, `"P.Envelope"`, true), parameters: twoFamilies},
		"type-only caller cannot fill family":              {peer: plainRecord(familySlot, `"P.Envelope"`, true), parameters: onlyType},
		"compatible family slot":                           {peer: plainRecord(familySlot, `"P.Envelope"`, true), valid: true},
		"compatible declaration family slot":               {peer: plainRecord(familySlot, `"P.Envelope"`, false), valid: true},
		"extra caller type does not make family ambiguous": {peer: plainRecord(familySlot, `"P.Envelope"`, true), parameters: mixedCaller, valid: true},
		"stronger family bound":                            {peer: plainRecord(familySlot, `"P.Envelope"`, true), parameters: oneSession, valid: true},
		"weaker family bound":                              {peer: plainRecord(sessionSlot, `"P.Envelope"`, true)},
		"weaker bound on declaration slot":                 {peer: plainRecord(sessionSlot, `"P.Envelope"`, false)},
		"plain concrete type":                              {peer: plainRecord(`[]`, `"string"`, false), valid: true},
		"explicit declaration type":                        {peer: plainRecord(typeSlot, `"T"`, false), expression: `{"apply":"peer.Box","with":{"T":{"array":{"nullable":"string"}}}}`, valid: true},
		"explicit family type":                             {peer: plainRecord(typeSlot, `"T"`, true), expression: `{"apply":"peer.Box","with":{"T":"integer"}}`, valid: true},
		"explicit mixed arguments":                         {peer: plainRecord(mixedSlots, `{"kind":"record","fields":[{"name":"value","type":"T"},{"name":"frame","type":"P.Envelope"}]}`, false), expression: `{"apply":"peer.Box","with":{"T":"string","P":"probe"}}`, valid: true},
		"explicit family argument":                         {peer: plainRecord(sessionSlot, `"P.Envelope"`, true), expression: `{"apply":"peer.Box","with":{"P":"probe"}}`, valid: true},
	} {
		t.Run(name, func(t *testing.T) {
			parameters := tc.parameters
			if parameters == "" {
				parameters = oneFamily
			}
			expression := tc.expression
			if expression == "" {
				expression = `"peer.Box"`
			}
			var slots string
			switch parameters {
			case oneFamily, oneSession:
				slots = `,{"name":"slot","type":"S.Envelope"}`
			case twoFamilies:
				slots = `,{"name":"slot","type":"S.Envelope"},{"name":"other","type":"R.Envelope"}`
			case onlyType:
				slots = `,{"name":"slot","type":"T"}`
			case mixedCaller:
				slots = `,{"name":"slot","type":"S.Envelope"},{"name":"other","type":"U"}`
			}
			files := fstest.MapFS{
				"api/contracts/peer/model.json":     {Data: []byte(`{"nightseam":2}`)},
				"api/contracts/peer/protocol.json":  {Data: []byte(tc.peer)},
				"api/contracts/x/model.json":        {Data: []byte(`{"nightseam":2,"imports":["peer","probe"]}`)},
				"api/contracts/x/protocol.json":     {Data: []byte(`{"profile":"nightseam.duplex/1","parameters":` + parameters + `,"types":{"Use":{"kind":"record","fields":[{"name":"box","type":` + expression + `}` + slots + `]}}}`)},
				"api/contracts/probe/model.json":    {Data: []byte(`{"nightseam":2}`)},
				"api/contracts/probe/protocol.json": {Data: []byte(`{"profile":"nightseam.duplex/1"}`)},
				"api/contracts/probe/session.json":  {Data: []byte(`{}`)},
			}
			world := Load(files, "api/contracts", nil)
			for family, problems := range world.Problems {
				if len(problems) != 0 {
					t.Fatalf("%s failed schemas: %v", family, problems)
				}
			}
			if problems := Validate(world, "peer"); len(problems) != 0 {
				t.Fatalf("invalid peer declaration: %v", problems)
			}
			var got []string
			for _, d := range Validate(world, "x") {
				got = append(got, d.Code+"@"+d.At().String())
			}
			want := ""
			if !tc.valid {
				want = "ambiguous_application@protocol.json#/types/Use/fields/0/type"
			}
			if actual := strings.Join(got, " "); actual != want {
				t.Fatalf("got %q, want %q", actual, want)
			}
		})
	}
}
