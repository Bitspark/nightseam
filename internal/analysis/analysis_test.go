package analysis

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
)

// A world of a session family, probe, a carrier generic in one session
// family, and an album generic in two that applies the carrier's Frame.
func slotWorld() World {
	return World(modeltest.World(map[string]map[string]string{
		"probe": {
			"model.json":    `{"nightseam": 2, "types": {"Base": {"kind": "record", "fields": [{"name": "text", "type": "string"}]}, "Payload": {"kind": "record", "extends": ["Base"], "fields": [{"name": "count", "type": "integer"}]}, "Payloads": {"kind": "alias", "type": {"array": "Payload"}}}}`,
			"protocol.json": modeltest.Protocol(`"server": {"methods": {"echo": {"request": "Payload", "result": "Payload"}}}`),
			"session.json":  `{}`,
		},
		"codex": {
			"model.json":    `{"nightseam": 2, "types": {"Payload": {"kind": "enum", "values": ["a"]}}}`,
			"protocol.json": modeltest.Protocol(``),
			"session.json":  `{}`,
		},
		"carrier": {
			"model.json": `{"nightseam": 2}`,
			"protocol.json": modeltest.Protocol(`"imports": ["probe"], "parameters": [{"name": "S", "of": "session"}],
				"types": {
					"Frame": {"kind": "record", "fields": [{"name": "sequence", "type": "integer"}, {"name": "message", "type": "S.Envelope"}]},
					"Attachment": {"kind": "record", "fields": [{"name": "connection", "type": "S.Handle"}]},
					"Frames": {"kind": "alias", "type": {"array": "Frame"}},
					"Plain": {"kind": "record", "fields": [{"name": "n", "type": "integer"}]}
				},
				"server": {"methods": {"relay": {"request": "Frame", "result": "probe.Envelope"}}, "events": {"framed": {"type": "Frames"}}}`),
		},
		"album": {
			"model.json": `{"nightseam": 2}`,
			"protocol.json": modeltest.Protocol(`"imports": ["carrier", "probe"], "parameters": [{"name": "A", "of": "session"}, {"name": "B", "of": "session"}],
				"types": {
					"Mine": {"kind": "record", "fields": [{"name": "held", "type": "A.Envelope"}, {"name": "payload", "type": "A.Payload"}]},
					"Borrowed": {"kind": "record", "fields": [{"name": "frame", "type": {"apply": "carrier.Frame", "with": {"S": "B"}}}]},
					"Fixed": {"kind": "record", "fields": [{"name": "frame", "type": {"apply": "carrier.Frame", "with": {"S": "probe"}}}]},
					"Both": {"kind": "record", "fields": [{"name": "mine", "type": "Mine"}, {"name": "borrowed", "type": "Borrowed"}, {"name": "fixed", "type": "Fixed"}]}
				},
				"server": {"methods": {"look": {"request": "Mine", "result": "Both"}}}`),
		},
	}))
}

// TestResolveGivesAFamilyItsWorld: the families and session families of
// the world, the imports resolved in turn, the members a parameter may
// bind, and the two injected types where there is a protocol.
func TestResolveGivesAFamilyItsWorld(t *testing.T) {
	world := slotWorld()
	album := Resolve(world, "album")
	if strings.Join(album.Families, ",") != "album,carrier,codex,probe" || strings.Join(album.Sessions, ",") != "codex,probe" {
		t.Fatalf("world is %v / %v", album.Families, album.Sessions)
	}
	if album.Imported["carrier"] == nil || album.Imported["carrier"].Imported["probe"] == nil || album.Imported["probe"] != album.Imported["carrier"].Imported["probe"] {
		t.Fatal("imports are not resolved transitively and once")
	}
	if len(album.Members) != 2 || album.Members["probe"] == nil || album.Members["codex"] == nil {
		t.Fatalf("members are %v", album.Members)
	}
	probe := Resolve(world, "probe")
	if len(probe.Members) != 1 || probe.Members["codex"] == nil || !probe.IsSession() || album.IsSession() {
		t.Fatal("a session family's members are the other session families")
	}
	if album.Types[model.EnvelopeType] == nil || album.Rank(model.EnvelopeType) != 1 || album.Rank("Mine") != 1 || probe.Rank("Base") != 0 || probe.Rank("Missing") != -1 {
		t.Fatal("injected types and ranks are wrong")
	}
	if Resolve(world, "nobody") != nil {
		t.Fatal("an unknown family resolved")
	}
}

// TestFieldsFlattenThroughInheritance: a record's wire fields are its
// parents' first, in extends order, then its own; a diamond repeats; a
// cycle is cut.
func TestFieldsFlattenThroughInheritance(t *testing.T) {
	f := Resolve(World(modeltest.World(map[string]map[string]string{"x": {
		"model.json": `{"nightseam": 2, "types": {
			"Base": {"kind": "record", "fields": [{"name": "id", "type": "string"}]},
			"Left": {"kind": "record", "extends": ["Base"], "fields": [{"name": "l", "type": "string"}]},
			"Right": {"kind": "record", "extends": ["Base"], "fields": [{"name": "r", "type": "string"}]},
			"Diamond": {"kind": "record", "extends": ["Left", "Right"], "fields": [{"name": "d", "type": "string"}]},
			"Loop": {"kind": "record", "extends": ["Loop"], "fields": [{"name": "x", "type": "string"}]},
			"OfEnum": {"kind": "record", "extends": ["E"], "fields": []},
			"E": {"kind": "enum", "values": ["v"]}
		}}`,
	}})), "x")
	names := func(name string) string {
		var out []string
		f.WalkFields(name, func(at FieldAt) { out = append(out, at.Owner+"."+at.Field.Name) })
		return strings.Join(out, " ")
	}
	if got := names("Diamond"); got != "Base.id Left.l Base.id Right.r Diamond.d" {
		t.Fatalf("Diamond flattens to %s", got)
	}
	if got := names("Loop"); got != "Loop.x" {
		t.Fatalf("Loop flattens to %s", got)
	}
	if got := names("OfEnum"); got != "" {
		t.Fatalf("a non-record parent is skipped, not %s", got)
	}
	if len(f.FlattenedFields("Diamond")) != 5 {
		t.Fatal("FlattenedFields disagrees with WalkFields")
	}
}

// TestGenericsFollowTheParameters: a type that draws on a parameter,
// directly or through what it refers to, is generic in it at the types it
// draws; an application renames an imported type's parameters to this
// family's; a filler that is a family makes no use.
func TestGenericsFollowTheParameters(t *testing.T) {
	world := slotWorld()
	carrier := Resolve(world, "carrier").Generics()
	want := map[string][]Use{
		"Frame":      {{"S", "Envelope"}},
		"Attachment": {{"S", "Handle"}},
		"Frames":     {{"S", "Envelope"}},
	}
	for name, uses := range want {
		if !reflect.DeepEqual(carrier.Types[name], uses) {
			t.Errorf("carrier.%s uses %v, not %v", name, carrier.Types[name], uses)
		}
	}
	if _, generic := carrier.Types["Plain"]; generic {
		t.Error("Plain is generic")
	}
	if !reflect.DeepEqual(carrier.Family, []Use{{"S", "Envelope"}, {"S", "Handle"}}) || !carrier.Generic() {
		t.Errorf("carrier is generic in %v", carrier.Family)
	}
	album := Resolve(world, "album").Generics()
	if !reflect.DeepEqual(album.Types["Mine"], []Use{{"A", "Envelope"}, {"A", "Payload"}}) {
		t.Errorf("Mine uses %v", album.Types["Mine"])
	}
	if !reflect.DeepEqual(album.Types["Borrowed"], []Use{{"B", "Envelope"}}) {
		t.Errorf("Borrowed uses %v", album.Types["Borrowed"])
	}
	if _, generic := album.Types["Fixed"]; generic {
		t.Error("Fixed, filled by a family, is generic")
	}
	if !reflect.DeepEqual(album.Types["Both"], []Use{{"A", "Envelope"}, {"A", "Payload"}, {"B", "Envelope"}}) {
		t.Errorf("Both uses %v", album.Types["Both"])
	}
	if !reflect.DeepEqual(album.Family, []Use{{"A", "Envelope"}, {"A", "Payload"}, {"B", "Envelope"}}) {
		t.Errorf("album is generic in %v", album.Family)
	}
	if !reflect.DeepEqual(album.Imported["carrier"]["Frame"], []Use{{"S", "Envelope"}}) {
		t.Error("the imported family's generics are not carried")
	}
	probe := Resolve(world, "probe").Generics()
	if probe.Generic() || len(probe.Types) != 0 {
		t.Error("probe is generic")
	}
	if uses := Resolve(world, "album").UsesOf(model.MustDecode(`{"array":{"apply":"carrier.Frame","with":{"S":"A"}}}`)); !reflect.DeepEqual(uses, []Use{{"A", "Envelope"}}) {
		t.Errorf("UsesOf an application is %v", uses)
	}
}

// TestObjectsOnTheWire: a request is an object when it is a record here or
// in an import, a type drawn from a parameter every member declares as a
// record, or an application of an imported record — and not otherwise.
func TestObjectsOnTheWire(t *testing.T) {
	world := slotWorld()
	album := Resolve(world, "album")
	for source, want := range map[string]bool{
		`"Mine"`:          true,
		`"carrier.Plain"`: true,
		`"A.Envelope"`:    true,
		`"A.Handle"`:      true,
		`"A.Payload"`:     false, // codex declares Payload as an enum
		`{"apply":"carrier.Frame","with":{"S":"B"}}`: true,
		`"string"`:         false,
		`{"array":"Mine"}`: false,
		`"carrier.Frames"`: false,
		`"Nothing"`:        false,
		`"nobody.Thing"`:   false,
	} {
		if got := album.IsObject(model.MustDecode(source)); got != want {
			t.Errorf("%s is an object: %v", source, got)
		}
	}
}

// TestReferencesAreTheImports: every family a declaration names is
// imported, so the packages a rendering refers to are the imports.
func TestReferencesAreTheImports(t *testing.T) {
	if got := strings.Join(Resolve(slotWorld(), "album").References(), ","); got != "carrier,probe" {
		t.Fatalf("references are %s", got)
	}
}
