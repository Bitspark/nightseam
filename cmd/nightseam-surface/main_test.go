package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareCheckouts(t *testing.T) {
	for _, tt := range []struct {
		name, source string
		fails        bool
	}{
		{"unchanged", "package probe\nfunc Echo() string { return \"new implementation\" }\nfunc private() {}", false},
		{"changed signature", "package probe\nfunc Echo() int { return 1 }", true},
		{"invalid source", "package probe\nfunc", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			from, against := t.TempDir(), t.TempDir()
			write := func(root, name, source string) {
				file := filepath.Join(root, "api/go/probe", name)
				if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte(source), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			write(against, "probe_generated.go", "package probe\nfunc Echo() string { return \"old\" }")
			write(from, "probe_generated.go", tt.source)
			write(from, "new_generated.go", "package probe\ntype Added string")
			write(from, "handwritten.go", "ignored non-generated source")
			var out bytes.Buffer
			err := run([]string{from, against}, &out)
			if (err != nil) != tt.fails {
				t.Fatalf("error=%v, output=%s", err, &out)
			}
			if tt.name == "changed signature" && (!strings.Contains(out.String(), "Echo() int") || !strings.Contains(out.String(), "Echo() string")) {
				t.Fatalf("missing signature difference: %s", &out)
			}
		})
	}
}

func TestCompareRefusesMissingArgumentsAndEmptyCheckouts(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	for _, root := range []string{left, right} {
		if err := os.MkdirAll(filepath.Join(root, "api/go"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{nil, {t.TempDir()}, {t.TempDir(), t.TempDir()}, {left, right}} {
		if err := run(args, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
