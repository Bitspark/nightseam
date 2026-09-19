package kernel

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
)

// A tier contributes the built-in's ordinary side, retaining the built-in
// payload's identity. Extending a session family's application protocol does
// not opt a protocol-only family into the session tier.
func TestSessionVocabularyEntersOnlySessionFamilySides(t *testing.T) {
	files := fstest.MapFS{}
	add := func(name, imports, server string, session bool) {
		files["api/contracts/"+name+"/model.json"] = &fstest.MapFile{Data: []byte(`{"nightseam":2,"imports":` + imports + `}`)}
		files["api/contracts/"+name+"/protocol.json"] = &fstest.MapFile{Data: []byte(`{"profile":"nightseam.duplex/1","server":` + server + `}`)}
		if session {
			files["api/contracts/"+name+"/session.json"] = &fstest.MapFile{Data: []byte(`{}`)}
		}
	}
	add("base", `[]`, `{"events":{"changed":{"type":"string"}}}`, true)
	add("left", `["base"]`, `{"extends":["base"]}`, true)
	add("right", `["base"]`, `{"extends":["base"]}`, true)
	add("diamond", `["left","right"]`, `{"extends":["left","right"]}`, true)
	add("plain", `[]`, `{"events":{"changed":{"type":"string"}}}`, false)
	add("plainDerived", `["base"]`, `{"extends":["base"]}`, false)
	world := Load(files, "api/contracts", nil)
	for _, name := range world.Names {
		t.Run(name, func(t *testing.T) {
			if problems := Validate(world, name); len(problems) != 0 {
				t.Fatal(problems)
			}
			declaration := world.Families[name]
			before, err := json.Marshal(declaration)
			if err != nil {
				t.Fatal(err)
			}
			facts := render.Build(Resolve(world, name))
			var control, cursor, changed int
			for _, event := range facts.Server.Events {
				switch event.Name {
				case "session.control", "session.cursor":
					typeName := "Control"
					if event.Name == "session.control" {
						control++
					} else {
						cursor++
						typeName = "Cursor"
					}
					want := model.Imported{Family: "session", Name: typeName}
					if !reflect.DeepEqual(event.Type, want) || event.Origin.Family != "session" || event.At.File != "nightseam:session/protocol.json" {
						t.Errorf("built-in event lost its payload or origin: %+v", event)
					}
				case "changed":
					changed++
				}
			}
			want := 0
			if declaration.Session != nil {
				want = 1
			}
			if control != want || cursor != want || changed != 1 {
				t.Errorf("events: control=%d cursor=%d changed=%d; want %d, %d, 1", control, cursor, changed, want, want)
			}
			for _, events := range [][]render.Event{facts.Client.Events, facts.Server.OwnEvents} {
				for _, event := range events {
					if strings.HasPrefix(event.Name, "session.") {
						t.Errorf("session event became an application declaration or client emission: %+v", event)
					}
				}
			}
			if want == 1 {
				if !slices.Contains(facts.References, "session") {
					t.Error("the session payload package is absent from the rendering's references")
				}
				source := facts.ReferencedFamily("session")
				if source == nil || source.Source != "nightseam:session" || source.Type("Control") == nil {
					t.Error("the built-in payload declaration is not available through source-family resolution")
				}
			}
			after, err := json.Marshal(declaration)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Error("resolving an implicit side rewrote the consumer's declaration")
			}
		})
	}
}

func TestSessionOperationNamespaceBelongsToBuiltin(t *testing.T) {
	for _, session := range []bool{false, true} {
		for _, side := range []string{"server", "client"} {
			for _, kind := range []string{"methods", "events"} {
				for _, name := range []string{"session.control", "session.future", "session_control"} {
					t.Run(fmt.Sprintf("session=%t/%s/%s/%s", session, side, kind, name), func(t *testing.T) {
						member := `{"result":"string"}`
						if kind == "events" {
							member = `{"type":"string"}`
						}
						files := fstest.MapFS{
							"api/contracts/x/model.json":    {Data: []byte(`{"nightseam":2}`)},
							"api/contracts/x/protocol.json": {Data: []byte(fmt.Sprintf(`{"profile":"nightseam.duplex/1",%q:{%q:{%q:%s}}}`, side, kind, name, member))},
						}
						if session {
							files["api/contracts/x/session.json"] = &fstest.MapFile{Data: []byte(`{}`)}
						}
						problems := Validate(Load(files, "api/contracts", nil), "x")
						if name == "session_control" {
							if len(problems) != 0 {
								t.Fatalf("a name outside the session namespace was refused: %v", problems)
							}
							return
						}
						if len(problems) != 1 || problems[0].Code != "reserved_name" || problems[0].File != "protocol.json" || problems[0].Pointer != "/"+side+"/"+kind+"/"+name {
							t.Fatalf("expected one reserved-name refusal at the consumer operation, got %v", problems)
						}
					})
				}
			}
		}
	}
}

func TestInheritedSessionNamespaceIsRefusedWhenOnlyChildIsValidated(t *testing.T) {
	files := fstest.MapFS{
		"api/contracts/base/model.json":     {Data: []byte(`{"nightseam":2}`)},
		"api/contracts/base/protocol.json":  {Data: []byte(`{"profile":"nightseam.duplex/1","server":{"events":{"session.future":{"type":"string"}}}}`)},
		"api/contracts/child/model.json":    {Data: []byte(`{"nightseam":2,"imports":["base"]}`)},
		"api/contracts/child/protocol.json": {Data: []byte(`{"profile":"nightseam.duplex/1","server":{"extends":["base"]}}`)},
	}
	problems := Validate(Load(files, "api/contracts", nil), "child")
	if len(problems) != 1 || problems[0].Code != "reserved_name" || problems[0].Family != "child" || problems[0].Pointer != "/server/extends/0" {
		t.Fatalf("selecting the child must not bypass the reserved namespace of an inherited declaration: %v", problems)
	}
}
