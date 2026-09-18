package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/load"
	"github.com/Bitspark/nightseam/internal/upgrade"
)

// The corpus under testdata/corpus is what upgrade makes of the v1 corpus
// under testdata/corpus-v1, held byte for byte, so that the converter is
// its own golden and the two corpora are provably the same families.

// TestUpgradeMatchesTheCorpus: converting every family of the v1 corpus
// gives exactly the corpus.
func TestUpgradeMatchesTheCorpus(t *testing.T) {
	a := &app{root: legacyCorpusRoot}
	names, err := a.layerFamilies()
	if err != nil {
		t.Fatal(err)
	}
	converted := map[string][]byte{}
	for _, name := range names {
		sources, err := a.layerSources(name)
		if err != nil {
			t.Fatal(err)
		}
		files, err := upgrade.Family(sources)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for file, data := range files {
			converted["api/contracts/"+name+"/"+file] = data
		}
	}
	holdGolden(t, corpusRoot, converted)
}

// TestCorporaLoad: the loader reads the corpus and the families as worlds
// without a word, each family with the tiers its declaration has.
func TestCorporaLoad(t *testing.T) {
	for _, root := range []string{corpusRoot, familiesRoot} {
		world, problems := load.Checkout(os.DirFS(root), "api/contracts", []string{"go", "typescript"})
		if len(problems) != 0 {
			t.Fatalf("%s: %v", root, problems)
		}
		if len(world.Names) == 0 {
			t.Fatalf("%s holds no families", root)
		}
		for _, name := range world.Names {
			if world.Families[name].Protocol == nil {
				t.Errorf("%s/%s has no protocol tier", root, name)
			}
		}
	}
	world, _ := load.Checkout(os.DirFS(corpusRoot), "api/contracts", nil)
	probe := world.Families["probe"]
	if probe.Session == nil || probe.Session.Decides[0] != "echo" || probe.Types["Payload"].At.File != "model.json" || !world.Families["carrier"].Has("protocol.json") {
		t.Fatal("the corpus did not load as declared")
	}
	if strings.Join(world.Families["album"].Imports, ",") != "carrier,probe" {
		t.Fatal("album does not import carrier and probe")
	}
}

// TestUpgradeCommand: the command rewrites a checkout's layer files into
// directories and removes them; a family already in the directory form is
// refused; --dry-run writes nothing; a checkout without layer files has
// nothing to upgrade.
func TestUpgradeCommand(t *testing.T) {
	root := t.TempDir()
	if _, _, err := run(t, root, "upgrade"); err == nil || !strings.Contains(err.Error(), "nothing to upgrade") {
		t.Fatalf("an empty checkout upgraded: %v", err)
	}
	writeLayers(t, root, "probe", true)
	out, _, err := run(t, root, "upgrade", "--dry-run")
	if err != nil || !strings.Contains(out, "--- probe/session.json") {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(root, "api/contracts/probe")); !os.IsNotExist(err) {
		t.Fatal("a dry run wrote the directory")
	}
	out, _, err = run(t, root, "upgrade")
	if err != nil || strings.Count(out, "wrote api/contracts/probe/") != 3 {
		t.Fatalf("upgrade: %v\n%s", err, out)
	}
	for _, file := range []string{"probe.dto.json", "probe.rpc.json", "probe.sess.json"} {
		if _, err := os.Stat(filepath.Join(root, "api/contracts", file)); !os.IsNotExist(err) {
			t.Errorf("%s remains", file)
		}
	}
	if out, _, err := run(t, root, "validate"); err != nil || out != "1 families; valid\n" {
		t.Fatalf("the upgraded family does not validate: %v\n%s", err, out)
	}
	writeLayers(t, root, "probe", false)
	if _, _, err := run(t, root, "upgrade", "probe"); err == nil || !strings.Contains(err.Error(), "already in the directory form") {
		t.Fatalf("a family in the directory form was upgraded again: %v", err)
	}
}
