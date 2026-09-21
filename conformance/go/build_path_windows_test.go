package conformance

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildWindowsExecutablePaths(t *testing.T) {
	recipe, dir := processRecipe(t, "success")
	program := filepath.Join(dir, "directory with space", "helper.exe")
	if err := os.MkdirAll(filepath.Dir(program), 0o700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(program, data, 0o700); err != nil {
		t.Fatal(err)
	}
	volume := filepath.VolumeName(program)
	for _, path := range []string{
		program,
		strings.TrimSuffix(program, ".exe"),
		filepath.Join(".", "directory with space", "helper.exe"),
		filepath.Join(".", "directory with space", "helper"),
		strings.TrimPrefix(program, volume),
		strings.TrimSuffix(strings.TrimPrefix(program, volume), ".exe"),
		volume + filepath.Join("directory with space", "helper.exe"),
	} {
		t.Run(path, func(t *testing.T) {
			recipe.Build[0].Argv[0] = path
			baseline := recipe.command(context.Background(), recipe.Build[0], Places{})
			if output, err := baseline.CombinedOutput(); err != nil {
				t.Fatalf("standard command rejected fixture path: %v: %s", err, output)
			}
			_ = os.Remove(filepath.Join(dir, "success"))
			if err := recipe.RunBuild(context.Background(), Places{}, false); err != nil {
				t.Fatalf("owned command changed executable resolution: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, "success")); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("explicit missing extension is not searched again", func(t *testing.T) {
		missing := filepath.Join(dir, "missing.exe")
		if err := os.WriteFile(missing+".exe", data, 0o700); err != nil {
			t.Fatal(err)
		}
		recipe.Build[0].Argv[0] = missing
		baseline := recipe.command(context.Background(), recipe.Build[0], Places{})
		if err := baseline.Run(); err == nil {
			t.Fatal("standard command accepted missing explicit executable")
		}
		if err := recipe.RunBuild(context.Background(), Places{}, false); err == nil {
			t.Fatal("owned command ran the wrong executable by adding a second suffix")
		}
	})
}
