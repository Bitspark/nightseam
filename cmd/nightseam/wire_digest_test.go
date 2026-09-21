package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
	"github.com/Bitspark/nightseam/internal/render"
	"github.com/Bitspark/nightseam/internal/spi"
	"github.com/Bitspark/nightseam/internal/targets/golang"
	"github.com/Bitspark/nightseam/internal/targets/typescript"
)

func TestGeneratedWireDescriptorsKeepTheirBytesWithDeclarationDigests(t *testing.T) {
	data, err := os.ReadFile("../../conformance/tables/digests.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Cases []struct{ Name, Declaration, Wire, Canonical, Digest string }
	}
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}
	if len(table.Cases) == 0 {
		t.Fatal("digest table has no cases")
	}
	for _, row := range table.Cases {
		t.Run(row.Name, func(t *testing.T) {
			source := row.Declaration
			if source == "" {
				source = `{"nightseam":2,` + row.Wire[1:]
			}
			sources := map[string]string{"model.json": source}
			if row.Name == "UTF-8 enum" {
				sources["go.json"] = `{"names":{"Greeting.你好":"GreetingChinese"}}`
			}
			world := analysis.World(modeltest.World(map[string]map[string]string{"same": sources}))
			family := render.Build(analysis.Resolve(world, "same"))
			digest := sha256.Sum256([]byte(family.Declaration))
			wantDigest := hex.EncodeToString(digest[:])
			if family.Declaration != row.Canonical || wantDigest != row.Digest {
				t.Fatalf("generated canonical declaration or digest differs from the shared table: declaration %q, digest %q", family.Declaration, wantDigest)
			}
			declarationJSON, err := json.Marshal(family.Declaration)
			if err != nil {
				t.Fatal(err)
			}
			for _, target := range []spi.Target{golang.New(golang.Config{Module: "example.test/m"}), typescript.New(typescript.Config{Scope: "@example"})} {
				files, err := target.Render(family)
				if err != nil {
					t.Fatal(err)
				}
				byPath := map[string]string{}
				for _, file := range files {
					byPath[file.Path] = string(file.Data)
				}
				var checks map[string][]string
				if target.Name() == "go" {
					checks = map[string][]string{"api/go/same-protocol/validation_generated.go": {
						"MustSchema(" + strconv.Quote(row.Wire) + ", WireDigest(),",
						"}).MustWithDeclaration(WireDeclaration())",
						"func WireDeclaration() string {",
						"return " + strconv.Quote(family.Declaration),
						`func WireDigest() string { return "` + wantDigest + `" }`,
					}}
				} else {
					checks = map[string][]string{
						"api/ts/same-client/src/types.ts": {"const contractTypes = " + row.Wire + " as unknown as WireFamily;", "export const wireDeclaration = " + string(declarationJSON) + ";", `export const wireDigest = "` + wantDigest + `";`, "createValidator(contractTypes, wireDigest,"},
						"api/ts/same-client/src/index.ts": {"export * from './types.ts';"},
					}
				}
				for path, wants := range checks {
					for _, want := range wants {
						if !strings.Contains(byPath[path], want) {
							t.Errorf("%s does not contain %q", path, want)
						}
					}
				}
			}
		})
	}
}

func TestGeneratedWireDigestIsReexportedByBothTypeScriptRoles(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"same": {"model.json": `{"nightseam":2}`, "protocol.json": modeltest.Protocol("")},
	}))
	family := render.Build(analysis.Resolve(world, "same"))
	files, err := typescript.New(typescript.Config{Scope: "@example"}).Render(family)
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]string{
		"api/ts/same-client/src/types.ts":  `export const wireDigest = "` + family.WireDigest + `";`,
		"api/ts/same-client/src/index.ts":  "export * from './types.ts';",
		"api/ts/same-binding/src/index.ts": `export * from "@example/same-client/types";`,
	}
	for _, file := range files {
		if want, ok := checks[file.Path]; ok {
			if !strings.Contains(string(file.Data), want) {
				t.Errorf("%s does not contain %q", file.Path, want)
			}
			delete(checks, file.Path)
		}
	}
	if len(checks) != 0 {
		t.Fatalf("missing generated files: %v", checks)
	}
}

func TestGeneratedWireDigestNamesAreReserved(t *testing.T) {
	for _, row := range []struct {
		target spi.Target
		name   string
	}{
		{golang.New(golang.Config{Module: "example.test/m"}), "WireDigest"},
		{golang.New(golang.Config{Module: "example.test/m"}), "WireDeclaration"},
		{typescript.New(typescript.Config{Scope: "@example"}), "wireDigest"},
		{typescript.New(typescript.Config{Scope: "@example"}), "wireDeclaration"},
		{typescript.New(typescript.Config{Scope: "@example"}), "withDeclaration"},
	} {
		t.Run(row.target.Name()+"/"+row.name, func(t *testing.T) {
			world := analysis.World(modeltest.World(map[string]map[string]string{
				"same": {"model.json": `{"nightseam":2,"types":{"Payload":{"kind":"record"}}}`, row.target.Name() + ".json": `{"names":{"Payload":"` + row.name + `"}}`},
			}))
			diagnostics := row.target.Check(render.Build(analysis.Resolve(world, "same")))
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == "reserved_name" {
					return
				}
			}
			t.Fatalf("digest identifier was not reserved: %v", diagnostics)
		})
	}
}
