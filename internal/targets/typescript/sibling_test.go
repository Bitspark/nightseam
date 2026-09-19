package typescript

import (
	"encoding/json"
	"testing"

	"github.com/Bitspark/nightseam/internal/model/modeltest"
)

func TestSiblingDependencies(t *testing.T) {
	f := family(map[string]string{"model.json": fixtureModel,
		"protocol.json": modeltest.Protocol(`"imports": ["probe"], "server": {"events": {"changed": {"type": "probe.Payload"}}}`)})
	for _, tc := range []struct {
		name, mode, layout, want string
		place                    map[string]string
	}{
		{name: "default", want: "file:../probe-client"},
		{name: "file", mode: "file", want: "file:../probe-client"},
		{name: "workspace", mode: "workspace", want: "workspace:*"},
		{name: "version", mode: "version", want: "0.0.0"},
		{name: "layout", layout: "packages/{family}/client", want: "file:../../probe/client"},
		{name: "placed importer", place: map[string]string{"x": "frontend/{family}"}, want: "file:../../api/ts/probe-client"},
		{name: "placed dependency", place: map[string]string{"probe": "lib/{family}"}, want: "file:../../../lib/probe"},
		{name: "nested dependency", place: map[string]string{"probe": "api/ts/x-client/{family}"}, want: "file:probe"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files, err := New(Config{Scope: "@example", Sibling: tc.mode, Layout: tc.layout, Place: tc.place}).Render(f)
			if err != nil {
				t.Fatal(err)
			}
			var manifest struct{ Dependencies map[string]string }
			if err := json.Unmarshal(files[2].Data, &manifest); err != nil {
				t.Fatal(err)
			}
			if got := manifest.Dependencies["@example/probe-client"]; got != tc.want {
				t.Fatalf("dependency = %q, want %q", got, tc.want)
			}
			if got := manifest.Dependencies[DefaultRuntime]; got != DefaultRuntimeVersion {
				t.Fatalf("runtime dependency changed: %q", got)
			}
		})
	}
	if err := (Config{Scope: "@example", Sibling: "registry"}).Validate(); err == nil {
		t.Fatal("invalid sibling mode was accepted")
	}
}
