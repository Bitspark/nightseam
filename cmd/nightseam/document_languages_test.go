package main

import (
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/compose"
	"github.com/Bitspark/nightseam/internal/load"
	"github.com/Bitspark/nightseam/internal/render"
	"github.com/Bitspark/nightseam/internal/spi"
)

func testSpellers() map[string]spi.Speller {
	spellers := map[string]spi.Speller{}
	for _, target := range compose.Targets(module, scope, "") {
		if speller, ok := target.(spi.Speller); ok {
			spellers[target.Name()] = speller
		}
	}
	return spellers
}

func TestDocumentCompositionUsesEnabledLanguages(t *testing.T) {
	f := render.Build(analysis.Resolve(proofWorld(t), "proof"))
	for _, disabled := range [][]string{nil, {"go"}, {"typescript"}, {"go", "typescript"}} {
		targets, diagnostics := compose.Configure(load.Config{Disabled: disabled}, module, scope, "")
		if len(diagnostics) != 0 {
			t.Fatal(diagnostics)
		}
		var page string
		for _, target := range targets {
			if target.Name() != "markdown" {
				continue
			}
			files, err := target.Render(f)
			if err != nil {
				t.Fatal(err)
			}
			page = string(files[0].Data)
		}
		if page == "" {
			t.Fatal("no document")
		}
		for _, name := range []string{"go", "typescript"} {
			want := true
			for _, absent := range disabled {
				if absent == name {
					want = false
				}
			}
			if got := strings.Contains(page, "\n```"+name+"\n"); got != want {
				t.Errorf("disabled %v: %s blocks present=%v, want %v", disabled, name, got, want)
			}
		}
	}
}
