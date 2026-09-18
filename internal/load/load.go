// Package load reads a checkout's families from their tier files into the
// typed model. A family is a directory under the contracts root; each tier
// is one file of it, named in the Tiers table with its rank, the sections it
// may carry and the schema its shape is held to. A target's override file
// is read raw for the target to decode. Nothing here resolves a name or
// checks a rule beyond the shape of a file; that is analysis and check.
package load

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/model"
)

// Tier is one tier of declaration: the file it is declared in, its rank
// among the tiers — a declaration refers to its own tier or a lower one —
// the sections its file may carry beyond the types and imports every tier
// carries, and the schema that holds the file's shape.
type Tier struct {
	Name     string
	Rank     int
	File     string
	Sections []string
	schema   string
}

// The tiers, lowest first. A concern is added here, with its checker in
// check and its field on the model.
var Tiers = []Tier{
	{Name: "model", Rank: 0, File: "model.json", Sections: []string{"nightseam"}, schema: "urn:nightseam:v2:model"},
	{Name: "protocol", Rank: 1, File: "protocol.json", Sections: []string{"profile", "parameters", "server", "client", "errors"}, schema: "urn:nightseam:v2:protocol"},
	{Name: "session", Rank: 2, File: "session.json", Sections: []string{"decides", "asks", "conversation", "extensions"}, schema: "urn:nightseam:v2:session"},
}

// Common are the sections every tier file may carry.
var Common = []string{"imports", "types"}

// TierOf finds a tier by its file name.
func TierOf(file string) (Tier, bool) {
	for _, tier := range Tiers {
		if tier.File == file {
			return tier, true
		}
	}
	return Tier{}, false
}

// Rank is the rank of the tier a file belongs to; a file no tier owns
// ranks below every tier.
func Rank(file string) int {
	if tier, ok := TierOf(file); ok {
		return tier.Rank
	}
	return -1
}

// OverrideFile is the override file of a target: <target>.json.
func OverrideFile(target string) string { return target + ".json" }

//go:embed schemas/common.schema.json
var commonSchema []byte

//go:embed schemas/model.schema.json
var modelSchema []byte

//go:embed schemas/protocol.schema.json
var protocolSchema []byte

//go:embed schemas/session.schema.json
var sessionSchema []byte

//go:embed schemas/overrides.schema.json
var overridesSchema []byte

type offlineLoader struct{}

func (offlineLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema resources are disabled: %s", url)
}

var compiled = sync.OnceValues(func() (map[string]*jsonschema.Schema, error) {
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.UseLoader(offlineLoader{})
	sources := map[string][]byte{
		"urn:nightseam:v2:common":    commonSchema,
		"urn:nightseam:v2:model":     modelSchema,
		"urn:nightseam:v2:protocol":  protocolSchema,
		"urn:nightseam:v2:session":   sessionSchema,
		"urn:nightseam:v2:overrides": overridesSchema,
	}
	for id, source := range sources {
		var value any
		decoder := json.NewDecoder(bytes.NewReader(source))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			return nil, fmt.Errorf("%s: %w", id, err)
		}
		if err := compiler.AddResource(id, value); err != nil {
			return nil, err
		}
	}
	schemas := map[string]*jsonschema.Schema{}
	for id := range sources {
		if id == "urn:nightseam:v2:common" {
			continue
		}
		schema, err := compiler.Compile(id)
		if err != nil {
			return nil, err
		}
		schemas[id] = schema
	}
	return schemas, nil
})

// World is every family of a checkout, by name, and their names in order.
type World struct {
	Families map[string]*model.Family
	Names    []string
}

// Checkout reads every family under the contracts directory of a
// filesystem: each directory is a family, read by Family. A JSON file at
// the top of the directory is a layer file of the previous declaration
// language and is reported, since the tool no longer reads it. The targets
// name the override files a family may carry.
func Checkout(fsys fs.FS, contracts string, targets []string) (*World, []diag.Diagnostic) {
	world := &World{Families: map[string]*model.Family{}}
	entries, err := fs.ReadDir(fsys, contracts)
	if errors.Is(err, fs.ErrNotExist) {
		return world, nil
	}
	if err != nil {
		return world, []diag.Diagnostic{{Code: "unreadable", Message: err.Error()}}
	}
	var diagnostics []diag.Diagnostic
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() {
			if family, layer, ok := layerFile(name); ok {
				diagnostics = append(diagnostics, diag.Diagnostic{Family: family, File: name, Code: "layer_file", Message: fmt.Sprintf("%s is declared in the %s layer file of the previous declaration language; run nightseam upgrade.", family, layer)})
			}
			continue
		}
		family, problems := Family(fsys, path.Join(contracts, name), name, targets)
		diagnostics = append(diagnostics, problems...)
		if family != nil {
			world.Families[name] = family
			world.Names = append(world.Names, name)
		}
	}
	sort.Strings(world.Names)
	diag.Sort(diagnostics)
	return world, diagnostics
}

// layerFile recognises <family>.<dto|rpc|sess>.json.
func layerFile(name string) (family, layer string, ok bool) {
	stem, isJSON := strings.CutSuffix(name, ".json")
	if !isJSON {
		return "", "", false
	}
	family, layer, ok = strings.Cut(stem, ".")
	if !ok || strings.Contains(layer, ".") {
		return "", "", false
	}
	switch layer {
	case "dto", "rpc", "sess":
		return family, layer, true
	}
	return "", "", false
}

// Family reads one family from its directory: each tier file present,
// shape-checked and decoded; each target's override file, raw. A family
// with any diagnostic is returned as far as it was read, so that what was
// read may still be listed, and nil when its model tier cannot be read at
// all.
func Family(fsys fs.FS, dir, name string, targets []string) (*model.Family, []diag.Diagnostic) {
	problems := diag.List{Family: name}
	family := &model.Family{Name: name, Types: map[string]*model.Type{}, Overrides: map[string]json.RawMessage{}}
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		problems.Add(diag.Location{}, "unreadable", err.Error())
		return nil, problems.Diagnostics
	}
	present := map[string]bool{}
	for _, entry := range entries {
		file := entry.Name()
		if entry.IsDir() {
			problems.Add(diag.Location{File: file}, "unknown_file", fmt.Sprintf("A family directory holds tier files and override files, not a directory: %s.", file))
			continue
		}
		if _, isTier := TierOf(file); isTier {
			present[file] = true
			continue
		}
		if target, isOverride := overrideOf(file, targets); isOverride {
			present[file] = true
			family.Overrides[target] = nil
			continue
		}
		problems.Add(diag.Location{File: file}, "unknown_file", fmt.Sprintf("A family directory holds tier files — %s — and a target's override file — %s — not %s.", tierFiles(), overrideFiles(targets), file))
	}
	imports := map[string]bool{}
	for _, tier := range Tiers {
		if !present[tier.File] {
			if tier.Rank == 0 {
				problems.Add(diag.Location{File: tier.File}, "missing_tier", fmt.Sprintf("Every family declares its model in %s.", tier.File))
				return nil, problems.Diagnostics
			}
			continue
		}
		if tier.Rank > 0 && !present[Tiers[tier.Rank-1].File] {
			problems.Add(diag.Location{File: tier.File}, "missing_tier", fmt.Sprintf("The %s tier builds on the %s tier: %s needs %s beside it.", tier.Name, Tiers[tier.Rank-1].Name, tier.File, Tiers[tier.Rank-1].File))
			continue
		}
		family.Files = append(family.Files, tier.File)
		data, err := fs.ReadFile(fsys, path.Join(dir, tier.File))
		if err != nil {
			problems.Add(diag.Location{File: tier.File}, "unreadable", err.Error())
			continue
		}
		readTier(tier, data, family, imports, &problems)
	}
	for target := range family.Overrides {
		file := OverrideFile(target)
		data, err := fs.ReadFile(fsys, path.Join(dir, file))
		if err != nil {
			problems.Add(diag.Location{File: file}, "unreadable", err.Error())
			continue
		}
		if shape(file, "urn:nightseam:v2:overrides", data, &problems) == nil {
			continue
		}
		family.Overrides[target] = json.RawMessage(data)
	}
	for imported := range imports {
		family.Imports = append(family.Imports, imported)
	}
	sort.Strings(family.Imports)
	diag.Sort(problems.Diagnostics)
	return family, problems.Diagnostics
}

// readTier shape-checks one tier file and decodes its sections into the
// family: the imports and types every tier carries, then the tier's own.
func readTier(tier Tier, data []byte, family *model.Family, imports map[string]bool, problems *diag.List) {
	sections := shape(tier.File, tier.schema, data, problems)
	if sections == nil {
		return
	}
	if raw, ok := sections["imports"]; ok {
		var names []string
		if err := json.Unmarshal(raw, &names); err == nil {
			for _, name := range names {
				imports[name] = true
			}
		}
	}
	if raw, ok := sections["types"]; ok {
		types, err := model.DecodeTypes(tier.File, raw)
		if err != nil {
			problems.Add(diag.Location{File: tier.File, Pointer: "/types"}, "invalid_type", err.Error())
			return
		}
		for _, name := range sortedKeys(types) {
			t := types[name]
			if previous, declared := family.Types[name]; declared {
				problems.Addf(t.At, "duplicate_type", "Type %s is declared in %s as well as in %s.", name, tier.File, previous.At.File)
				continue
			}
			family.Types[name] = t
		}
	}
	var err error
	switch tier.Name {
	case "protocol":
		family.Protocol, err = model.DecodeProtocol(tier.File, data)
	case "session":
		family.Session, err = model.DecodeSession(tier.File, data)
	}
	if err != nil {
		problems.Add(diag.Location{File: tier.File}, "invalid_declaration", err.Error())
	}
}

// shape decodes a file as an object and holds it to its schema; it returns
// the object's sections, or nil after reporting what is wrong.
func shape(file, schemaID string, data []byte, problems *diag.List) map[string]json.RawMessage {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		problems.Add(diag.Location{File: file}, "invalid_json", err.Error())
		return nil
	}
	schemas, err := compiled()
	if err != nil {
		problems.Add(diag.Location{File: file}, "schema_error", err.Error())
		return nil
	}
	if err := schemas[schemaID].Validate(value); err != nil {
		var validation *jsonschema.ValidationError
		if errors.As(err, &validation) {
			reportShape(file, validation, problems)
		} else {
			problems.Add(diag.Location{File: file}, "schema_error", err.Error())
		}
		return nil
	}
	var sections map[string]json.RawMessage
	if err := json.Unmarshal(data, &sections); err != nil {
		problems.Add(diag.Location{File: file}, "invalid_json", err.Error())
		return nil
	}
	return sections
}

func reportShape(file string, err *jsonschema.ValidationError, problems *diag.List) {
	if len(err.Causes) > 0 {
		for _, cause := range err.Causes {
			reportShape(file, cause, problems)
		}
		return
	}
	at := diag.Location{File: file}
	for _, part := range err.InstanceLocation {
		at = at.Sub(part)
	}
	problems.Add(at, "invalid_shape", err.ErrorKind.LocalizedString(message.NewPrinter(language.English)))
}

func overrideOf(file string, targets []string) (string, bool) {
	for _, target := range targets {
		if OverrideFile(target) == file {
			return target, true
		}
	}
	return "", false
}

func tierFiles() string {
	names := make([]string, len(Tiers))
	for i, tier := range Tiers {
		names[i] = tier.File
	}
	return strings.Join(names, ", ")
}

func overrideFiles(targets []string) string {
	names := make([]string, len(targets))
	for i, target := range targets {
		names[i] = OverrideFile(target)
	}
	return strings.Join(names, ", ")
}

func sortedKeys(types map[string]*model.Type) []string {
	names := make([]string, 0, len(types))
	for name := range types {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
