package check

import (
	"fmt"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
)

func TestDrawConstraintsFollowActualApplications(t *testing.T) {
	for _, member := range []struct {
		name, declaration string
		valid             bool
	}{
		{"record", `"Job":{"kind":"record","fields":[]}`, true},
		{"enum", `"Job":{"kind":"enum","values":["one"]}`, true},
		{"missing", `"Other":{"kind":"record","fields":[]}`, false},
		{"alias", `"Job":{"kind":"alias","type":"integer"}`, false},
		{"callable", `"Job":{"kind":"callable","request":"integer"}`, false},
		{"generic", `"Job":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"value","type":"T"}]}`, false},
	} {
		for _, forwarded := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/forwarded=%v", member.name, forwarded), func(t *testing.T) {
				files := map[string]map[string]string{
					"holder":    {"model.json": `{"nightseam":2}`, "protocol.json": modeltest.Protocol(`"parameters":[{"name":"S","of":"protocol"}],"types":{"Held":{"kind":"record","fields":[{"name":"job","type":"S.Job"}]}}`)},
					"provider":  {"model.json": `{"nightseam":2,"types":{` + member.declaration + `}}`, "protocol.json": modeltest.Protocol(``)},
					"unrelated": {"model.json": `{"nightseam":2}`, "protocol.json": modeltest.Protocol(``)},
					"forward":   {"model.json": `{"nightseam":2}`, "protocol.json": modeltest.Protocol(`"imports":["holder"],"parameters":[{"name":"U","of":"protocol"}],"types":{"Held":{"kind":"alias","type":{"apply":"holder.Held","with":{"S":"U"}}}}`)},
				}
				target, parameter := "holder", "S"
				if forwarded {
					target, parameter = "forward", "U"
				}
				files["consumer"] = map[string]string{"model.json": `{"nightseam":2}`, "protocol.json": modeltest.Protocol(fmt.Sprintf(`"imports":[%q,"provider"],"types":{"Bound":{"kind":"alias","type":{"apply":%q,"with":{%q:"provider"}}}}`, target, target+".Held", parameter))}
				w := analysis.World(modeltest.World(files))
				if diagnostics := Family(analysis.Resolve(w, "holder")); len(diagnostics) != 0 {
					t.Fatalf("unrelated family affected generic consumer: %v", diagnostics)
				}
				diagnostics := Family(analysis.Resolve(w, "consumer"))
				if (len(diagnostics) == 0) != member.valid {
					t.Fatalf("valid=%v: %v", member.valid, diagnostics)
				}
			})
		}
	}
}

func TestInheritedDrawRequestRequiresObject(t *testing.T) {
	for _, member := range []struct {
		declaration string
		valid       bool
	}{
		{`{"kind":"record","fields":[]}`, true},
		{`{"kind":"enum","values":["one"]}`, false},
	} {
		w := analysis.World(modeltest.World(map[string]map[string]string{
			"base":     {"model.json": `{"nightseam":2}`, "protocol.json": modeltest.Protocol(`"parameters":[{"name":"S","of":"protocol"}],"types":{"Request":{"kind":"alias","type":"S.Job"}},"server":{"methods":{"call":{"request":"Request","result":"S.Job"}}}`)},
			"provider": {"model.json": `{"nightseam":2,"types":{"Job":` + member.declaration + `}}`, "protocol.json": modeltest.Protocol(``)},
			"consumer": {"model.json": `{"nightseam":2}`, "protocol.json": modeltest.Protocol(`"imports":["base","provider"],"server":{"extends":[{"apply":"base","with":{"S":"provider"}}]}`)},
		}))
		if got := Family(analysis.Resolve(w, "base")); len(got) != 0 {
			t.Fatalf("open request: %v", got)
		}
		got := Family(analysis.Resolve(w, "consumer"))
		if (len(got) == 0) != member.valid {
			t.Fatalf("valid=%v: %v", member.valid, got)
		}
	}
}
