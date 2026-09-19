// Package builtin holds the families Nightseam declares of itself: the
// profile's `duplex` and the tunnel's `tunnel`, written as ordinary tier
// files in the declaration language and carried in the binary. They are how a tier reaches a family that carries it —
// there is no injection — and they are read by the same decoders a
// consumer's family is.
//
// A built-in's tier file is located as `nightseam:<family>/<tier>.json`,
// which is plainly not a path in the consumer's checkout, so that a
// diagnostic pointing into one says whose file it is.
package builtin

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/Bitspark/nightseam/internal/model"
)

//go:embed duplex/*.json tunnel/*.json
var files embed.FS

// Prefix marks a built-in's tier file apart from a consumer's.
const Prefix = "nightseam:"

// Locate is the file name a diagnostic about a built-in's declaration
// carries: nightseam:duplex/model.json.
func Locate(family, file string) string { return Prefix + family + "/" + file }

// Files is the embedded tier files, for a test that holds them to the same
// shape schemas a consumer's are held to.
func Files() fs.FS { return files }

var loaded = sync.OnceValues(func() (map[string]*model.Family, error) {
	families := map[string]*model.Family{}
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		f, err := read(entry.Name())
		if err != nil {
			return nil, err
		}
		families[entry.Name()] = f
	}
	return families, nil
})

func read(name string) (*model.Family, error) {
	f := &model.Family{Name: name, Source: Prefix + name, Types: map[string]*model.Type{}, Overrides: map[string]json.RawMessage{}}
	imports := map[string]bool{}
	for _, tier := range model.Tiers {
		data, err := fs.ReadFile(files, path.Join(name, tier.File))
		if err != nil {
			continue
		}
		file := Locate(name, tier.File)
		f.Files = append(f.Files, tier.File)
		var sections map[string]json.RawMessage
		if err := json.Unmarshal(data, &sections); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		if raw, ok := sections["imports"]; ok {
			var names []string
			if err := json.Unmarshal(raw, &names); err != nil {
				return nil, fmt.Errorf("%s: %w", file, err)
			}
			for _, imported := range names {
				imports[imported] = true
			}
		}
		if raw, ok := sections["types"]; ok {
			types, err := model.DecodeTypes(file, raw)
			if err != nil {
				return nil, err
			}
			for typeName, t := range types {
				f.Types[typeName] = t
			}
		}
		if tier.Name == "protocol" {
			if f.Protocol, err = model.DecodeProtocol(file, data); err != nil {
				return nil, err
			}
		}
	}
	for imported := range imports {
		f.Imports = append(f.Imports, imported)
	}
	sort.Strings(f.Imports)
	return f, nil
}

// Families is every built-in family by name, decoded once. A malformed
// built-in is a defect of this repository, not of a consumer's checkout,
// so it panics rather than becoming a diagnostic about their family.
func Families() map[string]*model.Family {
	families, err := loaded()
	if err != nil {
		panic("nightseam: the built-in families do not decode: " + err.Error())
	}
	return families
}

// Family is one built-in family by name.
func Family(name string) (*model.Family, bool) {
	f, ok := Families()[name]
	return f, ok
}

// Names is every built-in family's name, sorted.
func Names() []string {
	names := make([]string, 0, len(Families()))
	for name := range Families() {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Carried is the built-in families whose types a family with these tier
// files takes as its own: the tiers' table says which tier carries which
// built-in. A family with protocol.json carries `duplex`'s `Envelope` and
// `Handle`, since a family's envelope is a message of that family.
func Carried(files []string) []string {
	var out []string
	for _, file := range files {
		tier, ok := model.TierOf(file)
		if !ok || tier.Builtin == "" {
			continue
		}
		if _, exists := Family(tier.Builtin); exists {
			out = append(out, tier.Builtin)
		}
	}
	sort.Strings(out)
	return out
}

// Namespaces is the operation namespace of every built-in family that
// speaks on the wire, mapped to the family that owns it: the prefix before
// the first dot of every operation a built-in declares — `channel.` for
// the tunnel, `live.` for the live layer. A layer owns its namespace so
// that it is never contested, and the reservation is read off the
// declarations rather than written out a second time, so that a layer that
// gains an operation gains nothing else to keep in step.
func Namespaces() map[string]string {
	namespaces := map[string]string{}
	for name, f := range Families() {
		if f.Protocol == nil {
			continue
		}
		for _, side := range []*model.Side{&f.Protocol.Server, &f.Protocol.Client} {
			for _, m := range side.Methods {
				if prefix, _, ok := strings.Cut(m.Name, "."); ok {
					namespaces[prefix] = name
				}
			}
			for _, e := range side.Events {
				if prefix, _, ok := strings.Cut(e.Name, "."); ok {
					namespaces[prefix] = name
				}
			}
		}
	}
	return namespaces
}
