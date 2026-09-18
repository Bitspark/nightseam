package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/load"
	"github.com/Bitspark/nightseam/internal/upgrade"
)

// The v2 corpus under testdata/v2 is what upgrade makes of the v1 corpus
// and of the families the tests here declare inline: held byte for byte, so
// that the converter is its own golden and the two corpora are provably
// the same families. It moves to testdata/corpus when the CLI switches.
const v2Root = "testdata/v2"

// TestUpgradeMatchesTheCorpus: converting every family of the v1 corpus
// gives exactly the v2 corpus.
func TestUpgradeMatchesTheCorpus(t *testing.T) {
	a := &app{root: corpusRoot}
	names, err := a.families()
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
	holdGolden(t, filepath.Join(v2Root, "corpus"), converted)
}

// TestUpgradeMatchesTheFamilies: the families the tests declare inline as
// single files convert to the directories under testdata/v2/families; probe
// and codex carry the session role, so each gains a session tier.
func TestUpgradeMatchesTheFamilies(t *testing.T) {
	converted := map[string][]byte{}
	for name, source := range map[string]string{
		"probe":     withRole(t, probeContract, "probe"),
		"codex":     withRole(t, probeContract, "codex"),
		"carrier":   carrierContract,
		"pair":      pairContract,
		"album":     applyContract,
		"holder":    drawnContract,
		"workbench": readFixture(t, "testdata/workbench-api.json"),
	} {
		files, err := upgrade.Family(map[string][]byte{"": []byte(source)})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for file, data := range files {
			converted["api/contracts/"+name+"/"+file] = data
		}
	}
	holdGolden(t, filepath.Join(v2Root, "families"), converted)
}

// withRole gives an inline contract a name and the session role, as the v1
// tests build their session families.
func withRole(t *testing.T, source, name string) string {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal([]byte(source), &raw); err != nil {
		t.Fatal(err)
	}
	raw["name"] = name
	raw["role"] = "session"
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func readFixture(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// holdGolden compares rendered files against a golden tree, or rewrites the
// tree under -update.
func holdGolden(t *testing.T, root string, files map[string][]byte) {
	t.Helper()
	if *update {
		if err := os.RemoveAll(root); err != nil {
			t.Fatal(err)
		}
		for p, data := range files {
			writeFixture(t, root, p, data)
		}
		t.Logf("rewrote %d files under %s", len(files), root)
		return
	}
	for p, data := range files {
		want, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if os.IsNotExist(err) {
			t.Errorf("%s has no file under %s; run with -update", p, root)
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if string(want) != string(data) {
			t.Errorf("%s differs:\n%s", p, diff(string(want), string(data)))
		}
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if _, produced := files[filepath.ToSlash(rel)]; !produced {
			t.Errorf("%s under %s is produced by nothing; run with -update", rel, root)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestV2CorporaLoad: the loader reads both v2 corpora as worlds without a
// word, every family with the tiers its v1 form had.
func TestV2CorporaLoad(t *testing.T) {
	for _, corpus := range []string{"corpus", "families"} {
		world, problems := load.Checkout(os.DirFS(filepath.Join(v2Root, corpus)), "api/contracts", []string{"go", "typescript"})
		if len(problems) != 0 {
			t.Fatalf("%s: %v", corpus, problems)
		}
		if len(world.Names) == 0 {
			t.Fatalf("%s holds no families", corpus)
		}
		for _, name := range world.Names {
			f := world.Families[name]
			if f.Protocol == nil {
				t.Errorf("%s/%s has no protocol tier", corpus, name)
			}
		}
	}
	world, _ := load.Checkout(os.DirFS(filepath.Join(v2Root, "corpus")), "api/contracts", nil)
	probe := world.Families["probe"]
	if probe.Session == nil || probe.Session.Decides[0] != "echo" || probe.Types["Payload"].At.File != "model.json" || !world.Families["carrier"].Has("protocol.json") {
		t.Fatal("the v2 corpus did not load as the v1 corpus declares")
	}
	if strings.Join(world.Families["album"].Imports, ",") != "carrier" {
		t.Fatal("album does not import carrier")
	}
}

// TestUpgradeCommand: the command rewrites a checkout's layer files into
// directories and removes them; a family already in the directory form is
// refused; --dry-run writes nothing.
func TestUpgradeCommand(t *testing.T) {
	root := t.TempDir()
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
	world, problems := load.Checkout(os.DirFS(root), "api/contracts", nil)
	if len(problems) != 0 || world.Families["probe"] == nil || world.Families["probe"].Session == nil {
		t.Fatalf("the upgraded family does not load: %v", problems)
	}
	writeLayers(t, root, "probe", false)
	if _, _, err := run(t, root, "upgrade", "probe"); err == nil || !strings.Contains(err.Error(), "already in the directory form") {
		t.Fatalf("a family in the directory form was upgraded again: %v", err)
	}
}
