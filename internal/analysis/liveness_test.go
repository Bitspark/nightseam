package analysis

import (
	"testing"

	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
)

// liveWorld is a family whose live tier declares callables, one that imports
// them, and one generic in a family of the live tier. It is where liveness is
// decided from, without a checkout, so that shapes the checker refuses before
// they reach a target can still be put to the fixed point.
func liveWorld(t *testing.T) World {
	t.Helper()
	return World(modeltest.World(map[string]map[string]string{
		"worker": {
			"model.json":    `{"nightseam":2,"types":{"Percent":{"kind":"alias","type":"integer"},"Ticket":{"kind":"entity","key":"id","fields":[{"name":"id","type":"string"}]},"Page":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"items","type":{"array":"T"}}]}}}`,
			"protocol.json": `{"profile":"nightseam.duplex/1","server":{"methods":{"describe":{"request":"Ticket","result":"string"}}}}`,
			"live.json": `{"types":{
				"Report":{"kind":"callable","request":"Percent"},
				"Cancel":{"kind":"callable"},
				"Sink":{"kind":"record","fields":[{"name":"report","type":"Report"}]},
				"Job":{"kind":"record","fields":[{"name":"ticket","type":{"ref":"Ticket"}},{"name":"cancel","type":"Cancel"}]},
				"Sinks":{"kind":"alias","type":{"map":"Sink"}},
				"Watchers":{"kind":"alias","type":{"array":{"nullable":"Report"}}},
				"Outcome":{"kind":"union","tag":"state","variants":{"running":"Job","finished":{"empty":true}}},
				"Start":{"kind":"record","fields":[{"name":"ticket","type":"Ticket"},{"name":"progress","type":"Sink"}]}
			},"server":{"methods":{"start":{"request":"Start","result":"Job"}}}}`,
		},
		"reader": {
			"model.json":    `{"nightseam":2,"types":{"Note":{"kind":"record","fields":[{"name":"text","type":"string"}]}}}`,
			"protocol.json": `{"profile":"nightseam.duplex/1","server":{"methods":{"read":{"request":"Note","result":"string"}}}}`,
			"live.json":     `{"imports":["worker"],"types":{"Watch":{"kind":"record","fields":[{"name":"note","type":"Note"},{"name":"sink","type":"worker.Sink"}]}},"server":{"methods":{"watch":{"request":"Watch","result":"worker.Job"}}}}`,
		},
		"relay": {
			"model.json":    `{"nightseam":2,"types":{}}`,
			"protocol.json": `{"profile":"nightseam.duplex/1","imports":["worker"],"parameters":[{"name":"S","of":"live"}],"types":{"Carried":{"kind":"record","fields":[{"name":"message","type":"S.Envelope"}]}},"server":{"methods":{"relay":{"request":"Carried","result":"S.Envelope"}}}}`,
		},
	}))
}

// TestLivenessIsDerivedFromTheDeclarations: a value is live when it carries a
// callable, and nothing says so — it is the least fixed point of "contains a
// callable" over the declarations. Every edge the fixed point follows, and the
// two it deliberately does not, are held here rather than inferred from what a
// target happened to render.
func TestLivenessIsDerivedFromTheDeclarations(t *testing.T) {
	world := liveWorld(t)
	worker := Resolve(world, "worker")
	for name, want := range map[string]bool{
		// A callable is live; so is a record that holds one, a container of
		// one, a union one of whose arms carries one, and an alias of any of
		// those.
		"Report": true, "Cancel": true,
		"Sink": true, "Job": true, "Sinks": true, "Watchers": true,
		"Outcome": true, "Start": true,
		// Data stays data, in the same family and in a lower tier.
		"Percent": false, "Ticket": false, "Page": false,
		// A carried built-in is nobody's callable.
		"Envelope": false, "Handle": false,
	} {
		if got := worker.IsLiveType(name); got != want {
			t.Errorf("%s is live=%t; want %t", name, got, want)
		}
	}
	live := worker.LiveTypes()
	if len(live) != 8 {
		t.Errorf("the live types of worker are %v; the eight above are", live)
	}
	if callables := worker.Callables(); len(callables) != 2 || callables[0] != "Cancel" || callables[1] != "Report" {
		t.Errorf("the callables of worker are %v; they are Cancel and Report, in byte order", callables)
	}
}

// TestAnEntityKeyIsNotALiveBinding: `{"ref": "E"}` names a row by its key and
// is data, even when the entity it names carries a callable. The two
// identities — what the application calls a thing, and what a scope calls a
// binding — stay apart, which is the distinction #196 asked for.
func TestAnEntityKeyIsNotALiveBinding(t *testing.T) {
	world := World(modeltest.World(map[string]map[string]string{
		"holder": {
			"model.json":    `{"nightseam":2,"types":{"Row":{"kind":"entity","key":"id","fields":[{"name":"id","type":"string"}]}}}`,
			"protocol.json": `{"profile":"nightseam.duplex/1","server":{"methods":{"read":{"request":"Row","result":"string"}}}}`,
			"live.json":     `{"types":{"Act":{"kind":"callable"},"Live":{"kind":"entity","key":"id","fields":[{"name":"id","type":"string"},{"name":"act","type":"Act"}]},"Named":{"kind":"record","fields":[{"name":"which","type":{"ref":"Live"}}]}},"server":{"methods":{"act":{"request":"Named","result":"Act"}}}}`,
		},
	}))
	f := Resolve(world, "holder")
	if !f.IsLiveType("Live") {
		t.Fatal("an entity with a callable field is not live; its value carries one")
	}
	if f.IsLiveType("Named") {
		t.Fatal("a record holding only a key reference to a live entity is live; a key is data")
	}
	if f.IsLive(model.Ref{Entity: "Live"}) {
		t.Fatal("a key reference to a live entity is live")
	}
}

// TestLivenessIsDecidedUnderAnApplicationsBindings: a type parameter is live
// exactly when what fills it is, so one declaration of Page yields a live
// Page<Job> and a data Page<Percent>. The checker refuses a live application
// for want of a boundary conversion, but the fixed point is what lets it see
// one, so the two cases are held here.
func TestLivenessIsDecidedUnderAnApplicationsBindings(t *testing.T) {
	f := Resolve(liveWorld(t), "worker")
	live := model.Apply{Name: "Page", With: map[string]model.Filler{"T": {Type: model.Named{Name: "Job"}}}}
	data := model.Apply{Name: "Page", With: map[string]model.Filler{"T": {Type: model.Named{Name: "Percent"}}}}
	if !f.IsLive(live) {
		t.Error("Page filled with a live type is not live")
	}
	if f.IsLive(data) {
		t.Error("Page filled with data is live")
	}
	if f.IsLiveType("Page") {
		t.Error("the declaration of Page is live; only an application of it can be")
	}
}

// TestLivenessReachesThroughAnImport: a type of an imported family is live
// when its declaration there is, so a family acquires liveness from what it
// names and not only from what it writes.
func TestLivenessReachesThroughAnImport(t *testing.T) {
	f := Resolve(liveWorld(t), "reader")
	if !f.IsLiveType("Watch") {
		t.Fatal("a record holding an imported callable record is not live")
	}
	if f.IsLiveType("Note") {
		t.Fatal("a data record of the model tier is live")
	}
	if !f.IsLive(model.Imported{Family: "worker", Name: "Job"}) {
		t.Fatal("an imported live type is not live where it is named")
	}
	if f.IsLive(model.Imported{Family: "worker", Name: "Percent"}) {
		t.Fatal("an imported data type is live")
	}
}

// TestADrawIsNotLiveByItsParametersTier: a family parameter `of: "live"` is a
// family that *has* the live tier, and drawing data through it is ordinary. A
// draw that would be live is refused where it is written, by the checker,
// which can name the family that makes it so; the fixed point says nothing
// about it, since what fills the parameter is not known here.
func TestADrawIsNotLiveByItsParametersTier(t *testing.T) {
	f := Resolve(liveWorld(t), "relay")
	if f.IsLive(model.Drawn{Parameter: "S", Name: "Envelope"}) {
		t.Fatal("a draw through a live family parameter is live; the tier of the parameter is not the liveness of what is drawn")
	}
	if f.IsLiveType("Carried") {
		t.Fatal("a record drawing an envelope through a live family parameter is live")
	}
}

// TestLivenessTerminatesOnACycle: the fixed point is least, so a declaration
// reached again while it is being decided contributes nothing on that edge.
// A checkout cannot express this — the value-cycle check refuses one before
// analysis is asked — so it is put to the model directly, which is the only
// place the recursion can be seen at all.
func TestLivenessTerminatesOnACycle(t *testing.T) {
	world := World(modeltest.World(map[string]map[string]string{
		"loop": {
			"model.json":    `{"nightseam":2,"types":{"Leaf":{"kind":"record","fields":[{"name":"text","type":"string"}]}}}`,
			"protocol.json": `{"profile":"nightseam.duplex/1","server":{"methods":{"read":{"request":"Leaf","result":"string"}}}}`,
			"live.json":     `{"types":{"Act":{"kind":"callable"},"Node":{"kind":"record","fields":[{"name":"act","type":"Act"},{"name":"children","type":{"array":"Node"}}]},"Plain":{"kind":"record","fields":[{"name":"leaf","type":"Leaf"},{"name":"children","type":{"array":"Plain"}}]}},"server":{"methods":{"walk":{"request":"Node","result":"Act"}}}}`,
		},
	}))
	f := Resolve(world, "loop")
	done := make(chan [2]bool, 1)
	go func() { done <- [2]bool{f.IsLiveType("Node"), f.IsLiveType("Plain")} }()
	select {
	case got := <-done:
		if !got[0] {
			t.Error("a recursive record holding a callable is not live")
		}
		if got[1] {
			t.Error("a recursive record of data is live; the cycle alone adds nothing")
		}
	case <-t.Context().Done():
		t.Fatal("deciding a recursive declaration did not terminate")
	}
}
