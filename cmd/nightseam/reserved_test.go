package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/targets/golang"
	"github.com/Bitspark/nightseam/internal/targets/spec"
	"github.com/Bitspark/nightseam/internal/targets/typescript"
)

// What each target reserves — the identifiers its generated code declares
// of itself, which a family may not name — is held under testdata/reserved
// so that an emitter declaring something new is a reviewed line, not a
// silent reservation.
const reservedRoot = "testdata/reserved"

// TestReservedNamesAreGolden: each target's reserved identifiers are
// exactly what its golden holds.
func TestReservedNamesAreGolden(t *testing.T) {
	files := map[string][]byte{}
	for target, reserved := range map[string][]string{golang.Name: golang.Reserved(), typescript.Name: typescript.Reserved(), spec.Name: spec.Reserved()} {
		files[target+".txt"] = []byte(strings.Join(reserved, "\n") + "\n")
	}
	if *update {
		if err := os.RemoveAll(reservedRoot); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			writeFixture(t, reservedRoot, name, data)
		}
		return
	}
	for name, data := range files {
		want, err := os.ReadFile(filepath.Join(reservedRoot, name))
		if os.IsNotExist(err) {
			t.Fatalf("%s has no golden; run with -update", name)
		}
		if err != nil {
			t.Fatal(err)
		}
		if string(want) != string(data) {
			t.Errorf("the reserved names of %s changed:\n%s", strings.TrimSuffix(name, ".txt"), diff(string(want), string(data)))
		}
	}
}
