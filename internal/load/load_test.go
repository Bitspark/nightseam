package load

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/model"
)

func checkout(files map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for name, data := range files {
		fsys["api/contracts/"+name] = &fstest.MapFile{Data: []byte(data)}
	}
	return fsys
}

const probeModel = `{"nightseam": 2, "types": {
	"Payload": {"kind": "record", "fields": [{"name": "text", "type": "string"}]},
	"Status": {"kind": "enum", "values": ["on", "off"]}
}}`

const probeProtocol = `{"profile": "nightseam.duplex/1", "imports": ["other"],
	"types": {"Seen": {"kind": "record", "fields": [{"name": "at", "type": "timestamp"}]}},
	"server": {"methods": {"echo": {"request": "Payload", "result": "Payload"}}, "events": {"changed": {"type": "Payload"}}},
	"client": {"methods": {"reverse": {"request": "Payload", "result": "Payload"}}},
	"errors": {"denied": "The caller is denied."}}`

const probeSession = `{"decides": ["echo"], "asks": ["reverse"], "conversation": {"event": "changed", "path": "text"}}`

// TestCheckoutReadsFamilies: a checkout's directories are its families, each
// tier file decoded into the one model, types and imports from every tier,
// override files read raw, and the names in order.
func TestCheckoutReadsFamilies(t *testing.T) {
	world, problems := Checkout(checkout(map[string]string{
		"probe/model.json":      probeModel,
		"probe/protocol.json":   probeProtocol,
		"probe/session.json":    probeSession,
		"probe/go.json":         `{"names": {"Payload.text": "Text"}}`,
		"album/model.json":      `{"nightseam": 2, "imports": ["probe"]}`,
		"album/typescript.json": `{}`,
	}), "api/contracts", []string{"go", "typescript"})
	if len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
	if strings.Join(world.Names, ",") != "album,probe" {
		t.Fatalf("names are %v", world.Names)
	}
	probe := world.Families["probe"]
	if strings.Join(probe.Files, ",") != "model.json,protocol.json,session.json" || !probe.Has("session.json") {
		t.Fatalf("files are %v", probe.Files)
	}
	if probe.Types["Payload"].At.File != "model.json" || probe.Types["Seen"].At.File != "protocol.json" {
		t.Fatal("types do not know their tier")
	}
	if strings.Join(probe.Imports, ",") != "other" || strings.Join(world.Families["album"].Imports, ",") != "probe" {
		t.Fatal("imports are not the union of the tiers'")
	}
	if probe.Protocol == nil || probe.Protocol.Server.Methods[0].Name != "echo" || probe.Session == nil || probe.Session.Asks[0] != "reverse" {
		t.Fatal("the protocol or session tier did not decode")
	}
	if string(probe.Overrides["go"]) != `{"names": {"Payload.text": "Text"}}` || string(world.Families["album"].Overrides["typescript"]) != `{}` {
		t.Fatalf("overrides are %v", probe.Overrides)
	}
	if o, err := model.DecodeOverrides("go.json", probe.Overrides["go"]); err != nil || o.Names["Payload.text"] != "Text" {
		t.Fatal("the override file does not decode")
	}
}

// TestCheckoutRefuses: what the loader holds a checkout to, each named by
// file and pointer.
func TestCheckoutRefuses(t *testing.T) {
	for name, c := range map[string]struct {
		files map[string]string
		code  string
		at    string
		says  string
	}{
		"a layer file": {
			files: map[string]string{"probe.rpc.json": `{}`},
			code:  "layer_file", at: "probe.rpc.json#", says: "run nightseam upgrade",
		},
		"no model tier": {
			files: map[string]string{"probe/protocol.json": probeProtocol},
			code:  "missing_tier", at: "model.json#", says: "declares its model in model.json",
		},
		"a session without a protocol": {
			files: map[string]string{"probe/model.json": probeModel, "probe/session.json": probeSession},
			code:  "missing_tier", at: "session.json#", says: "needs protocol.json beside it",
		},
		"an unknown file": {
			files: map[string]string{"probe/model.json": probeModel, "probe/rust.json": `{}`},
			code:  "unknown_file", at: "rust.json#", says: "go.json, typescript.json",
		},
		"a section of another tier": {
			files: map[string]string{"probe/model.json": `{"nightseam": 2, "server": {}}`},
			code:  "invalid_shape", at: "model.json#", says: "server",
		},
		"the wrong language version": {
			files: map[string]string{"probe/model.json": `{"nightseam": 1}`},
			code:  "invalid_shape", at: "model.json#/nightseam", says: "2",
		},
		"a slot object of the previous language": {
			files: map[string]string{"probe/model.json": `{"nightseam": 2, "types": {"Frame": {"kind": "record", "fields": [{"name": "m", "type": {"envelope": "S"}}]}}}`},
			code:  "invalid_shape", at: "model.json#/types/Frame/fields/0/type",
		},
		"a type declared twice": {
			files: map[string]string{"probe/model.json": probeModel, "probe/protocol.json": `{"profile": "nightseam.duplex/1", "types": {"Payload": {"kind": "enum", "values": ["x"]}}}`},
			code:  "duplicate_type", at: "protocol.json#/types/Payload", says: "as well as in model.json",
		},
		"an override file that declares": {
			files: map[string]string{"probe/model.json": probeModel, "probe/go.json": `{"types": {}}`},
			code:  "invalid_shape", at: "go.json#",
		},
		"malformed JSON": {
			files: map[string]string{"probe/model.json": `{"nightseam": 2,`},
			code:  "invalid_json", at: "model.json#",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, problems := Checkout(checkout(c.files), "api/contracts", []string{"go", "typescript"})
			for _, p := range problems {
				if p.Code == c.code && p.At().String() == c.at && strings.Contains(p.Message, c.says) {
					return
				}
			}
			t.Fatalf("no %s at %s saying %q among %v", c.code, c.at, c.says, problems)
		})
	}
}

// TestNoContracts: a checkout without a contracts directory has no families
// and nothing to say.
func TestNoContracts(t *testing.T) {
	world, problems := Checkout(fstest.MapFS{}, "api/contracts", nil)
	if len(world.Names) != 0 || len(problems) != 0 {
		t.Fatal("an absent directory is not empty")
	}
}

// TestTiersAreATable: the tiers rank lowest first and each file names its
// tier; a file no tier owns ranks below all.
func TestTiersAreATable(t *testing.T) {
	if model.Rank("model.json") != 0 || model.Rank("protocol.json") != 1 || model.Rank("session.json") != 2 || model.Rank("go.json") != -1 {
		t.Fatal("ranks are wrong")
	}
	if tier, ok := model.TierOf("session.json"); !ok || tier.Name != "session" || !strings.Contains(strings.Join(tier.Sections, ","), "decides") {
		t.Fatal("session.json is not the session tier")
	}
	if model.OverrideFile("go") != "go.json" {
		t.Fatal("override files are <target>.json")
	}
	for _, tier := range model.Tiers {
		if schemas[tier.Name] == "" {
			t.Errorf("tier %s has no schema", tier.Name)
		}
	}
	var _ diag.Location
}
