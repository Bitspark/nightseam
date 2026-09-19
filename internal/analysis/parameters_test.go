package analysis

import (
	"reflect"
	"testing"

	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
)

func TestGenericUsesIncludeTypeParametersAndInlineCaptures(t *testing.T) {
	world := World(modeltest.World(map[string]map[string]string{
		"payload": {"model.json": `{"nightseam":2}`, "protocol.json": modeltest.Protocol("")},
		"boxes": {"model.json": `{"nightseam":2}`, "protocol.json": modeltest.Protocol(`
			"parameters":[{"name":"Item"},{"name":"Unused"},{"name":"S","of":"protocol"}],
			"types":{
				"Wrapper":{"kind":"record","fields":[{"name":"child","type":{"kind":"record","fields":[{"name":"value","type":"Item"},{"name":"handle","type":"S.Handle"}]}}]},
				"Mixed":{"kind":"record","parameters":[{"name":"T"},{"name":"F","of":"protocol"}],"fields":[{"name":"value","type":"T"},{"name":"message","type":"F.Envelope"},{"name":"item","type":"Item"}]},
				"Local":{"kind":"alias","type":{"apply":"Mixed","with":{"T":{"array":{"nullable":"Item"}},"F":"S"}}},
				"Base":{"kind":"union","tag":"kind","variants":{"item":"Item"}},
				"Derived":{"kind":"union","tag":"kind","extends":["Base"],"variants":{"text":"string"}}
			}`)},
		"consumer": {"model.json": `{"nightseam":2,"imports":["boxes","payload"]}`, "protocol.json": modeltest.Protocol(`
			"parameters":[{"name":"V"},{"name":"A","of":"protocol"}],
			"types":{"Applied":{"kind":"alias","type":{"apply":"boxes.Wrapper","with":{"Item":{"map":{"nullable":"V"}},"S":"A"}}},
			"Fixed":{"kind":"alias","type":{"apply":"boxes.Wrapper","with":{"Item":"string","S":"payload"}}}}`)},
	}))
	boxes := Resolve(world, "boxes")
	want := map[string][]Use{
		"Wrapper": {{"Item", ""}, {"S", "Handle"}},
		"Mixed":   {{"Item", ""}, {"T", ""}, {"F", "Envelope"}},
		"Local":   {{"Item", ""}, {"S", "Envelope"}},
		"Derived": {{"Item", ""}},
	}
	for name, expected := range want {
		if got := boxes.Generics().Types[name]; !reflect.DeepEqual(got, expected) {
			t.Errorf("boxes.%s uses = %v, want %v", name, got, expected)
		}
	}
	if got, expected := boxes.Generics().Family, []Use{{"Item", ""}, {"S", "Envelope"}, {"S", "Handle"}}; !reflect.DeepEqual(got, expected) {
		t.Errorf("family uses = %v, want %v", got, expected)
	}
	consumer := Resolve(world, "consumer")
	if got, expected := consumer.Generics().Types["Applied"], []Use{{"V", ""}, {"A", "Handle"}}; !reflect.DeepEqual(got, expected) {
		t.Errorf("imported application uses = %v, want %v", got, expected)
	}
	if got := consumer.Generics().Types["Fixed"]; len(got) != 0 {
		t.Errorf("concrete application uses = %v", got)
	}
	if got := consumer.UsesOf(consumer.Types["Applied"].Alias); !reflect.DeepEqual(got, consumer.Generics().Types["Applied"]) {
		t.Errorf("expression and declaration uses disagree: %v", got)
	}
	if got := boxes.UsesOf(model.Named{Name: "Item"}); !reflect.DeepEqual(got, []Use{{"Item", ""}}) {
		t.Errorf("family type parameter uses = %v", got)
	}
}

func TestGenericAnalysisStopsAtAnInlineInheritanceCycle(t *testing.T) {
	world := World(modeltest.World(map[string]map[string]string{
		"x": {"model.json": `{"nightseam":2,"types":{"Node":{"kind":"record","fields":[{"name":"child","type":{"kind":"record","extends":["Node"],"fields":[]}}]}}}`},
	}))
	if got := Resolve(world, "x").Generics(); got.Generic() {
		t.Fatalf("a malformed inheritance cycle invented parameters: %+v", got)
	}
}
