package kernel

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestAppliedTypeInheritanceRequiresExplicitBindings(t *testing.T) {
	for name, tc := range map[string]struct{ base, edge, parameters, want string }{
		"fix":                           {`{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"value","type":"T"}]}`, `{"apply":"base.Base","with":{"T":"string"}}`, ``, ``},
		"forward renamed nested filler": {`{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"value","type":"T"}]}`, `{"apply":"base.Base","with":{"T":{"array":{"nullable":"Item"}}}}`, `,"parameters":[{"name":"Item"}]`, ``},
		"bare generic":                  {`{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"value","type":"T"}]}`, `"base.Base"`, `,"parameters":[{"name":"T"}]`, `unbound_parameter`},
		"missing":                       {`{"kind":"record","parameters":[{"name":"T"}],"fields":[]}`, `{"apply":"base.Base","with":{}}`, ``, `unbound_parameter`},
		"unknown":                       {`{"kind":"record","parameters":[{"name":"T"}],"fields":[]}`, `{"apply":"base.Base","with":{"T":"string","X":"integer"}}`, ``, `unresolved_parameter`},
		"family in type slot":           {`{"kind":"record","parameters":[{"name":"T"}],"fields":[]}`, `{"apply":"base.Base","with":{"T":"base"}}`, ``, `invalid_filler`},
		"plain non-generic":             {`{"kind":"record","fields":[]}`, `"base.Base"`, ``, ``},
		"needless application":          {`{"kind":"record","fields":[]}`, `{"apply":"base.Base","with":{}}`, ``, `needless_application`},
	} {
		t.Run(name, func(t *testing.T) {
			fsys := fstest.MapFS{
				"contracts/base/model.json":  {Data: []byte(`{"nightseam":2,"types":{"Base":` + tc.base + `}}`)},
				"contracts/child/model.json": {Data: []byte(`{"nightseam":2,"imports":["base"],"types":{"Child":{"kind":"record","extends":[` + tc.edge + `],"fields":[]` + tc.parameters + `}}}`)},
			}
			world := Load(fsys, "contracts", nil)
			for _, problems := range world.Problems {
				if len(problems) > 0 {
					t.Fatal(problems)
				}
			}
			problems := Validate(world, "child")
			if tc.want == "" {
				if len(problems) > 0 {
					t.Fatal(problems)
				}
				return
			}
			if len(problems) != 1 || problems[0].Code != tc.want || !strings.HasPrefix(problems[0].Pointer, "/types/Child/extends/0") {
				t.Fatalf("got %v, want %s at the edge", problems, tc.want)
			}
		})
	}
}

func TestAppliedSideInheritanceBindsTheBaseFamilyParameters(t *testing.T) {
	for _, edge := range []string{`"base"`, `{"apply":"base","with":{"T":{"array":{"nullable":"Item"}}}}`} {
		fsys := fstest.MapFS{
			"contracts/base/model.json":     {Data: []byte(`{"nightseam":2}`)},
			"contracts/base/protocol.json":  {Data: []byte(`{"profile":"nightseam.duplex/1","parameters":[{"name":"T"}],"server":{"methods":{"read":{"result":"T"}}}}`)},
			"contracts/child/model.json":    {Data: []byte(`{"nightseam":2,"imports":["base"]}`)},
			"contracts/child/protocol.json": {Data: []byte(`{"profile":"nightseam.duplex/1","parameters":[{"name":"Item"}],"server":{"extends":[` + edge + `],"events":{"own":{"type":"Item"}}}}`)},
		}
		world := Load(fsys, "contracts", nil)
		for _, problems := range world.Problems {
			if len(problems) > 0 {
				t.Fatal(problems)
			}
		}
		problems := Validate(world, "child")
		if edge[0] == '"' {
			if len(problems) != 1 || problems[0].Code != "unbound_parameter" {
				t.Fatalf("bare generic side: %v", problems)
			}
		} else if len(problems) > 0 {
			t.Fatal(problems)
		}
	}
}

func TestAppliedSideDiamondRequiresOneBindingPerOperation(t *testing.T) {
	for _, second := range []string{"string", "integer"} {
		fsys := fstest.MapFS{
			"contracts/base/model.json":     {Data: []byte(`{"nightseam":2}`)},
			"contracts/base/protocol.json":  {Data: []byte(`{"profile":"nightseam.duplex/1","parameters":[{"name":"T"}],"server":{"methods":{"read":{"result":"T"}},"events":{"changed":{"type":"T"}}}}`)},
			"contracts/left/model.json":     {Data: []byte(`{"nightseam":2,"imports":["base"]}`)},
			"contracts/left/protocol.json":  {Data: []byte(`{"profile":"nightseam.duplex/1","server":{"extends":[{"apply":"base","with":{"T":"string"}}]}}`)},
			"contracts/right/model.json":    {Data: []byte(`{"nightseam":2,"imports":["base"]}`)},
			"contracts/right/protocol.json": {Data: []byte(`{"profile":"nightseam.duplex/1","server":{"extends":[{"apply":"base","with":{"T":"` + second + `"}}]}}`)},
			"contracts/child/model.json":    {Data: []byte(`{"nightseam":2,"imports":["left","right"]}`)},
			"contracts/child/protocol.json": {Data: []byte(`{"profile":"nightseam.duplex/1","server":{"extends":["left","right"]}}`)},
		}
		world := Load(fsys, "contracts", nil)
		problems := Validate(world, "child")
		if second == "string" {
			if len(problems) != 0 {
				t.Fatal(problems)
			}
		} else {
			if len(problems) != 2 {
				t.Fatalf("different diamond bindings: %v", problems)
			}
			for _, problem := range problems {
				if problem.Code != "operation_collision" || problem.Pointer != "/server/extends/1" {
					t.Fatal(problem)
				}
			}
		}
	}
}

func TestLocalInheritanceExplicitlyBindsCapturedFamilyParameters(t *testing.T) {
	for _, edge := range []string{`"Base"`, `{"apply":"Base","with":{"T":"string"}}`} {
		world := Load(fstest.MapFS{
			"contracts/x/model.json":    {Data: []byte(`{"nightseam":2}`)},
			"contracts/x/protocol.json": {Data: []byte(`{"profile":"nightseam.duplex/1","parameters":[{"name":"T"}],"types":{"Base":{"kind":"record","fields":[{"name":"value","type":"T"}]},"Child":{"kind":"record","extends":[` + edge + `],"fields":[]}}}`)},
		}, "contracts", nil)
		problems := Validate(world, "x")
		if edge[0] == '"' {
			if len(problems) != 1 || problems[0].Code != "unbound_parameter" {
				t.Fatal(problems)
			}
		} else if len(problems) != 0 {
			t.Fatal(problems)
		}
	}
}

func TestInheritedReferenceBindingsKeepTheirFamilyIdentity(t *testing.T) {
	files := fstest.MapFS{
		"contracts/base/model.json":     {Data: []byte(`{"nightseam":2,"types":{"Choice":{"kind":"union","tag":"kind","parameters":[{"name":"T"}],"variants":{"chosen":"T"}}}}`)},
		"contracts/base/protocol.json":  {Data: []byte(`{"profile":"nightseam.duplex/1","parameters":[{"name":"Item"}],"server":{"methods":{"read":{"result":"Item"}}}}`)},
		"contracts/child/model.json":    {Data: []byte(`{"nightseam":2,"imports":["left","right"],"types":{"Choice":{"kind":"union","tag":"kind","extends":["left.Choice","right.Choice"],"variants":{}}}}`)},
		"contracts/child/protocol.json": {Data: []byte(`{"profile":"nightseam.duplex/1","server":{"extends":["left","right"]}}`)},
	}
	for _, name := range []string{"left", "right"} {
		files["contracts/"+name+"/model.json"] = &fstest.MapFile{Data: []byte(`{"nightseam":2,"imports":["base"],"types":{"Entity":{"kind":"entity","key":"id","fields":[{"name":"id","type":"string"}]},"Choice":{"kind":"union","tag":"kind","extends":[{"apply":"base.Choice","with":{"T":{"ref":"Entity"}}}],"variants":{}}}}`)}
		files["contracts/"+name+"/protocol.json"] = &fstest.MapFile{Data: []byte(`{"profile":"nightseam.duplex/1","server":{"extends":[{"apply":"base","with":{"Item":{"ref":"Entity"}}}]}}`)}
	}
	world := Load(files, "contracts", nil)
	for name, problems := range world.Problems {
		if len(problems) != 0 {
			t.Fatalf("%s: %v", name, problems)
		}
	}
	problems := Validate(world, "child")
	if len(problems) != 2 || problems[0].Code != "variant_collision" || problems[1].Code != "operation_collision" {
		t.Fatalf("references to separate same-named entities must collide: %v", problems)
	}
}
