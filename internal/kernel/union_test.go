package kernel

import (
	"strings"
	"testing"
	"testing/fstest"
)

// These declarations reach the neutral checker through the real schemas,
// before either target's refusal of new rendering forms can hide a defect.
func TestUnionDiscriminatorsAndInheritance(t *testing.T) {
	model := func(types string) string { return `{"nightseam":2,"types":{` + types + `}}` }
	imported := func(types string) string {
		return strings.Replace(model(types), `"types":`, `"imports":["remote"],"types":`, 1)
	}
	const literalPayload = `"Payload":{"kind":"record","fields":[{"name":"kind","type":{"literal":"ok"}}]}`
	const plainPayload = `"Payload":{"kind":"record","fields":[{"name":"kind","type":"string"}]}`
	const localUnion = `"Result":{"kind":"union","tag":"kind","variants":{"ok":"Payload"}}`
	const importedUnion = `"Result":{"kind":"union","tag":"kind","variants":{"ok":"remote.Payload"}}`
	for name, tc := range map[string]struct {
		models map[string]string
		want   string
	}{
		"required literal": {models: map[string]string{"x": model(literalPayload + "," + localUnion)}},
		"nullable literal sugar": {
			models: map[string]string{"x": model(`"Payload":{"kind":"record","fields":[{"name":"kind","type":{"literal":"ok"},"nullable":true}]},` + localUnion)},
		},
		"nullable literal expression": {
			models: map[string]string{"x": model(`"Payload":{"kind":"record","fields":[{"name":"kind","type":{"nullable":{"literal":"ok"}}}]},` + localUnion)},
		},
		"optional literal": {
			models: map[string]string{"x": model(`"Payload":{"kind":"record","fields":[{"name":"kind","type":{"literal":"ok"},"required":false}]},` + localUnion)},
		},
		"inherited nullable literal": {
			models: map[string]string{"x": model(`"Base":{"kind":"record","fields":[{"name":"kind","type":{"literal":"ok"},"nullable":true}]},"Payload":{"kind":"record","extends":["Base"],"fields":[]},` + localUnion)},
		},
		"inline nullable literal": {
			models: map[string]string{"x": model(`"Result":{"kind":"union","tag":"kind","variants":{"ok":{"kind":"record","fields":[{"name":"kind","type":{"literal":"ok"},"nullable":true}]}}}`)},
		},
		"imported plain payload alongside local literal": {
			models: map[string]string{"x": imported(literalPayload + "," + importedUnion), "remote": model(plainPayload)},
		},
		"imported literal payload alongside local plain field": {
			models: map[string]string{"x": imported(plainPayload + "," + importedUnion), "remote": model(literalPayload)},
		},
		"imported inherited plain payload": {
			models: map[string]string{
				"x":      imported(`"Base":{"kind":"record","fields":[{"name":"kind","type":{"literal":"ok"}}]},` + importedUnion),
				"remote": model(`"Base":{"kind":"record","fields":[{"name":"kind","type":"string"}]},"Payload":{"kind":"record","extends":["Base"],"fields":[]}`),
			},
		},
		"imported applied payload": {
			models: map[string]string{
				"x":      imported(literalPayload + `,"Result":{"kind":"union","tag":"kind","variants":{"ok":{"apply":"remote.Payload","with":{"T":"string"}}}}`),
				"remote": model(`"Payload":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"kind","type":"string"},{"name":"data","type":"T"}]}`),
			},
		},
		"conflicting bases": {
			models: map[string]string{"x": model(`"A":{"kind":"union","tag":"kind","variants":{"same":"string"}},"B":{"kind":"union","tag":"kind","variants":{"same":"integer"}},"Result":{"kind":"union","tag":"kind","extends":["A","B"],"variants":{"own":"boolean"}}`)},
			want:   "variant_collision@model.json#/types/Result/extends/1",
		},
		"conflicting transitive bases": {
			models: map[string]string{"x": model(`"A":{"kind":"union","tag":"kind","variants":{"same":"string"}},"B":{"kind":"union","tag":"kind","extends":["A"],"variants":{"b":"boolean"}},"C":{"kind":"union","tag":"kind","variants":{"same":"integer"}},"Result":{"kind":"union","tag":"kind","extends":["B","C"],"variants":{"own":"boolean"}}`)},
			want:   "variant_collision@model.json#/types/Result/extends/1",
		},
		"separate declarations with identical payloads": {
			models: map[string]string{"x": model(`"A":{"kind":"union","tag":"kind","variants":{"same":"string"}},"B":{"kind":"union","tag":"kind","variants":{"same":"string"}},"Result":{"kind":"union","tag":"kind","extends":["A","B"],"variants":{"own":"boolean"}}`)},
			want:   "variant_collision@model.json#/types/Result/extends/1",
		},
		"disjoint bases":                    {models: map[string]string{"x": model(`"A":{"kind":"union","tag":"kind","variants":{"a":"string"}},"B":{"kind":"union","tag":"kind","variants":{"b":"integer"}},"Result":{"kind":"union","tag":"kind","extends":["A","B"],"variants":{"own":"boolean"}}`)}},
		"one declaration through a diamond": {models: map[string]string{"x": model(`"A":{"kind":"union","tag":"kind","variants":{"a":"string"}},"B":{"kind":"union","tag":"kind","extends":["A"],"variants":{"b":"integer"}},"C":{"kind":"union","tag":"kind","extends":["A"],"variants":{"c":"boolean"}},"Result":{"kind":"union","tag":"kind","extends":["B","C"],"variants":{"own":"boolean"}}`)}},
	} {
		t.Run(name, func(t *testing.T) {
			files := fstest.MapFS{}
			for family, data := range tc.models {
				files["api/contracts/"+family+"/model.json"] = &fstest.MapFile{Data: []byte(data)}
			}
			world := Load(files, "api/contracts", nil)
			for family, problems := range world.Problems {
				if len(problems) > 0 {
					t.Fatalf("%s was refused by the schemas: %v", family, problems)
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
