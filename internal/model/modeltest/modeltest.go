// Package modeltest builds families from tier files written inline, for
// the tests of the packages above model that may not read a checkout: the
// loader's work without its schemas.
package modeltest

import (
	"encoding/json"
	"sort"

	"github.com/Bitspark/nightseam/internal/model"
)

// Family decodes one family from its tier files by file name — model.json,
// protocol.json, and a target's override file — as the loader would,
// without holding them to a schema. It panics on what does not
// decode, since a test wrote it.
func Family(name string, files map[string]string) *model.Family {
	f := &model.Family{Name: name, Types: map[string]*model.Type{}, Overrides: map[string]json.RawMessage{}}
	imports := map[string]bool{}
	for _, tier := range model.Tiers {
		source, ok := files[tier.File]
		if !ok {
			continue
		}
		f.Files = append(f.Files, tier.File)
		var sections map[string]json.RawMessage
		if err := json.Unmarshal([]byte(source), &sections); err != nil {
			panic(tier.File + ": " + err.Error())
		}
		if raw, ok := sections["imports"]; ok {
			var names []string
			if err := json.Unmarshal(raw, &names); err != nil {
				panic(err)
			}
			for _, n := range names {
				imports[n] = true
			}
		}
		if raw, ok := sections["types"]; ok {
			types, err := model.DecodeTypes(tier.File, raw)
			if err != nil {
				panic(err)
			}
			for typeName, t := range types {
				f.Types[typeName] = t
			}
		}
		if tier.Name == "protocol" {
			protocol, err := model.DecodeProtocol(tier.File, json.RawMessage(source))
			if err != nil {
				panic(err)
			}
			f.Protocol = protocol
		}
	}
	for file, source := range files {
		if _, isTier := model.TierOf(file); !isTier {
			f.Overrides[file[:len(file)-len(".json")]] = json.RawMessage(source)
		}
	}
	for name := range imports {
		f.Imports = append(f.Imports, name)
	}
	sort.Strings(f.Imports)
	return f
}

// World builds several families at once, by name.
func World(families map[string]map[string]string) map[string]*model.Family {
	world := map[string]*model.Family{}
	for name, files := range families {
		world[name] = Family(name, files)
	}
	return world
}

// Protocol is a protocol tier with the profile every family speaks and the
// given sections spliced in, for tests that write one inline.
func Protocol(sections string) string {
	if sections == "" {
		return `{"profile": "nightseam.duplex/1"}`
	}
	return `{"profile": "nightseam.duplex/1", ` + sections + `}`
}
