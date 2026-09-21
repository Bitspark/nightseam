package analysis

import (
	"reflect"
	"testing"

	"github.com/Bitspark/nightseam/internal/model/modeltest"
)

func TestCallableSignaturesCaptureParametersAndAssociatedTypes(t *testing.T) {
	world := World(modeltest.World(map[string]map[string]string{
		"source": {
			"model.json":    `{"nightseam":2}`,
			"protocol.json": modeltest.Protocol(`"parameters":[{"name":"Item"},{"name":"S","of":"live"}]`),
			"live.json": `{"types":{
				"Function":{"kind":"callable","parameters":[{"name":"A"},{"name":"B"}],"request":"A","result":"B"},
				"Captured":{"kind":"callable","request":"Item","result":"S.Job"}
			}}`,
		},
		"consumer": {
			"model.json":    `{"nightseam":2}`,
			"protocol.json": modeltest.Protocol(`"parameters":[{"name":"V"},{"name":"R","of":"live"}]`),
			"live.json":     `{"imports":["source"],"types":{"Applied":{"kind":"alias","type":{"apply":"source.Captured","with":{"Item":{"array":"V"},"S":"R"}}}}}`,
		},
	}))
	for _, row := range []struct {
		family, name string
		want         []Use
	}{
		{"source", "Function", []Use{{"A", ""}, {"B", ""}}},
		{"source", "Captured", []Use{{"Item", ""}, {"S", "Job"}}},
		{"consumer", "Applied", []Use{{"V", ""}, {"R", "Job"}}},
	} {
		if got := Resolve(world, row.family).Generics().Types[row.name]; !reflect.DeepEqual(got, row.want) {
			t.Errorf("%s.%s uses %v, want %v", row.family, row.name, got, row.want)
		}
	}
}
