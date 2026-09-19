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
	"github.com/Bitspark/nightseam/internal/model/builtin"
	"github.com/Bitspark/nightseam/internal/scalarjson"
)

// schemas maps each tier to the schema that holds its file's shape.
var schemas = map[string]string{"model": "urn:nightseam:v1:model", "protocol": "urn:nightseam:v1:protocol", "session": "urn:nightseam:v1:session"}

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
		"urn:nightseam:v1:common":    commonSchema,
		"urn:nightseam:v1:model":     modelSchema,
		"urn:nightseam:v1:protocol":  protocolSchema,
		"urn:nightseam:v1:session":   sessionSchema,
		"urn:nightseam:v1:overrides": overridesSchema,
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
		if id == "urn:nightseam:v1:common" {
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
// the top of the directory is reported rather than passed over, since a
// family is a directory of tier files and a file there is one somebody
// meant to be a family. The targets name the override files a family may
// carry. Built-in family names are reserved for Nightseam's declarations.
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
			if family, ok := strayFile(name); ok {
				diagnostics = append(diagnostics, diag.Diagnostic{Family: family, File: name, Code: "stray_file", Message: fmt.Sprintf("%s is a file; a family is a directory of tier files, %s/%s/.", name, contracts, family)})
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

// strayFile reads the family a misplaced <family>[.suffix].json was meant for.
func strayFile(name string) (family string, ok bool) {
	stem, isJSON := strings.CutSuffix(name, ".json")
	if !isJSON {
		return "", false
	}
	// A stem of its own is the family; one carrying a suffix names the family
	// before the first dot, so that the diagnostic points at the directory the
	// file was meant to be.
	if base, _, cut := strings.Cut(stem, "."); cut {
		stem = base
	}
	if stem == "" {
		return "", false
	}
	return stem, true
}

// Family reads one family from its directory: each tier file present,
// shape-checked and decoded; each target's override file, raw. A family
// with any diagnostic is returned as far as it was read, so that what was
// read may still be listed, and nil when its model tier cannot be read at
// all.
func Family(fsys fs.FS, dir, name string, targets []string) (*model.Family, []diag.Diagnostic) {
	problems := diag.List{Family: name}
	if err := scalarjson.Value(name); err != nil {
		problems.Add(diag.Location{}, "invalid_declaration", err.Error())
		return nil, problems.Diagnostics
	}
	family := &model.Family{Name: name, Source: dir, Types: map[string]*model.Type{}, Overrides: map[string]json.RawMessage{}}
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		problems.Add(diag.Location{}, "unreadable", err.Error())
		return nil, problems.Diagnostics
	}
	if _, reserved := builtin.Family(name); reserved {
		problems.Addf(diag.Location{File: model.ModelFile}, "builtin_name", "Family name %q is reserved for the built-in family at %s; rename the checkout directory %s and update imports and qualified references.", name, builtin.Locate(name, model.ModelFile), dir)
	}
	present := map[string]bool{}
	for _, entry := range entries {
		file := entry.Name()
		if entry.IsDir() {
			problems.Add(diag.Location{File: file}, "unknown_file", fmt.Sprintf("A family directory holds tier files and override files, not a directory: %s.", file))
			continue
		}
		if _, isTier := model.TierOf(file); isTier {
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
	imports := map[string][]diag.Location{}
	for _, tier := range model.Tiers {
		if !present[tier.File] {
			if tier.Rank == 0 {
				problems.Add(diag.Location{File: tier.File}, "missing_tier", fmt.Sprintf("Every family declares its model in %s.", tier.File))
				return nil, problems.Diagnostics
			}
			continue
		}
		if tier.Rank > 0 && !present[model.Tiers[tier.Rank-1].File] {
			problems.Add(diag.Location{File: tier.File}, "missing_tier", fmt.Sprintf("The %s tier builds on the %s tier: %s needs %s beside it.", tier.Name, model.Tiers[tier.Rank-1].Name, tier.File, model.Tiers[tier.Rank-1].File))
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
		file := model.OverrideFile(target)
		data, err := fs.ReadFile(fsys, path.Join(dir, file))
		if err != nil {
			problems.Add(diag.Location{File: file}, "unreadable", err.Error())
			continue
		}
		if shape(file, "urn:nightseam:v1:overrides", data, &problems) == nil {
			continue
		}
		family.Overrides[target] = json.RawMessage(data)
	}
	for imported, locations := range imports {
		family.Imports = append(family.Imports, imported)
		if _, isBuiltin := builtin.Family(imported); !isBuiltin {
			continue
		}
		message := fmt.Sprintf("Family %s is built in and is imported by the tier that brings it, with no imports line.", imported)
		other := path.Join(path.Dir(dir), imported)
		if entry, err := fs.Stat(fsys, other); err == nil && entry.IsDir() {
			message = fmt.Sprintf("The checkout family at %s uses the reserved built-in name %q; rename its directory and update imports and qualified references. %s", path.Join(other, model.ModelFile), imported, message)
		}
		for _, at := range locations {
			problems.Add(at, "implicit_import", message)
		}
	}
	sort.Strings(family.Imports)
	implicit(family, &problems)
	diag.Sort(problems.Diagnostics)
	return family, problems.Diagnostics
}

// implicit settles what the tiers a family has bring it. A type a tier's
// built-in family carries is the family's own and is written with the built-in
// that declares it, duplex.Envelope, which is rewritten here to the plain
// name the family carries it under — the one import code path, taken
// before anything resolves.
func implicit(family *model.Family, problems *diag.List) {
	carried := map[string]*model.Family{}
	for _, name := range builtin.Carried(family.Files) {
		if b, ok := builtin.Family(name); ok {
			carried[name] = b
		}
	}
	family.Expressions(func(site model.ExprAt) {
		named, plain := site.Expr.(model.Named)
		if !plain {
			return
		}
		if _, declared := family.Types[named.Name]; declared {
			return
		}
		for _, name := range sortedFamilies(carried) {
			if _, carries := carried[name].Types[named.Name]; carries {
				problems.Addf(site.At, "implicit_import", "Type %s is carried from the built-in %s family; write %s.%s.", named.Name, name, name, named.Name)
				return
			}
		}
	})
	family.RewriteExpressions(func(e model.TypeExpr) model.TypeExpr {
		imported, ok := e.(model.Imported)
		if !ok {
			return e
		}
		b, isCarried := carried[imported.Family]
		if !isCarried {
			return e
		}
		if _, declares := b.Types[imported.Name]; !declares {
			return e
		}
		return model.Named{Name: imported.Name}
	})
}

func sortedFamilies(m map[string]*model.Family) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// readTier shape-checks one tier file and decodes its sections into the
// family: the imports and types every tier carries, then the tier's own.
func readTier(tier model.Tier, data []byte, family *model.Family, imports map[string][]diag.Location, problems *diag.List) {
	sections := shape(tier.File, schemas[tier.Name], data, problems)
	if sections == nil {
		return
	}
	if raw, ok := sections["imports"]; ok {
		var names []string
		if err := json.Unmarshal(raw, &names); err == nil {
			for i, name := range names {
				imports[name] = append(imports[name], diag.Location{File: tier.File}.Sub("imports", i))
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
	if err := scalarjson.Raw(data); err != nil {
		problems.Add(diag.Location{File: file}, "invalid_json", err.Error())
		return nil
	}
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
		if model.OverrideFile(target) == file {
			return target, true
		}
	}
	return "", false
}

func tierFiles() string {
	names := make([]string, len(model.Tiers))
	for i, tier := range model.Tiers {
		names[i] = tier.File
	}
	return strings.Join(names, ", ")
}

func overrideFiles(targets []string) string {
	names := make([]string, len(targets))
	for i, target := range targets {
		names[i] = model.OverrideFile(target)
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
