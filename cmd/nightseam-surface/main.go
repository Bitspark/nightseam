// nightseam-surface compares the generated Go API shared by two checkouts.
package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Bitspark/nightseam/internal/surface"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: nightseam-surface <checkout> <against-checkout>")
	}
	from, against := args[0], args[1]
	compared, changed := 0, 0
	err := filepath.WalkDir(filepath.Join(from, "api/go"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, "_generated.go") {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		theirs, err := os.ReadFile(filepath.Join(against, rel))
		if os.IsNotExist(err) {
			fmt.Fprintf(out, "%s: not in the other checkout\n", filepath.ToSlash(rel))
			return nil
		}
		if err != nil {
			return err
		}
		mine, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		want, err := surface.Declarations(rel, string(theirs))
		if err != nil {
			return err
		}
		got, err := surface.Declarations(rel, string(mine))
		if err != nil {
			return err
		}
		compared++
		if !slices.Equal(want, got) {
			changed++
			fmt.Fprintf(out, "%s:\n--- against\n%s\n+++ checkout\n%s\n", filepath.ToSlash(rel), strings.Join(want, "\n"), strings.Join(got, "\n"))
		}
		return nil
	})
	if err != nil {
		return err
	}
	if compared == 0 {
		return fmt.Errorf("no generated Go files shared by the checkouts")
	}
	if changed != 0 {
		return fmt.Errorf("%d of %d generated Go files changed surface", changed, compared)
	}
	fmt.Fprintf(out, "%d generated Go files have the same surface\n", compared)
	return nil
}
