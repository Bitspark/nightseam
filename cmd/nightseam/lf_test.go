package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestFixturesAreLF: every fixture under testdata ends its lines with LF
// alone, so that a byte comparison against a rendering means the same on
// Windows as on Linux. .gitattributes holds the checkout to this; the test
// holds a file written by hand or by -update.
func TestFixturesAreLF(t *testing.T) {
	err := filepath.WalkDir("testdata", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(data, []byte{'\r'}) {
			t.Errorf("%s has a carriage return", filepath.ToSlash(path))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
