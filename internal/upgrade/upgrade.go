// Package upgrade rewrites a family declared in the previous language — the
// layer files <family>.dto.json, .rpc.json and .sess.json, or one file with
// every section — into the directory form: model.json, protocol.json,
// session.json, and an override file per target where a name the family
// spelled by hand differs from what the convention derives. It works on the
// JSON alone, reads nothing of the old generator, and stays after the old
// generator is gone, for whoever still has layer files.
package upgrade

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Bitspark/nightseam/internal/naming"
)

// Layers are the layer files of the previous language, lowest first, and
// the tier file each becomes.
var Layers = []struct{ Layer, File string }{
	{"dto", "model.json"},
	{"rpc", "protocol.json"},
	{"sess", "session.json"},
}

// Family converts one family. sources maps a layer — "dto", "rpc", "sess",
// or "" for a single file holding every section — to its bytes. The result
// maps each tier file to its bytes, ready to write under the family's
// directory; an override file is present only when it has an entry.
func Family(sources map[string][]byte) (map[string][]byte, error) {
	files := map[string]*object{}
	overrides := map[string]*object{"go": newObject(), "typescript": newObject()}
	for layer, data := range sources {
		var raw map[string]json.RawMessage
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&raw); err != nil {
			return nil, fmt.Errorf("%s layer: %w", layer, err)
		}
		if err := convertLayer(layer, raw, files, overrides); err != nil {
			return nil, err
		}
	}
	if _, ok := files["model.json"]; !ok {
		files["model.json"] = newObject()
	}
	// A family carried the session role in the old language by declaring
	// it; in the new one, by having a session tier.
	if role, ok := sources[""]; ok && bytes.Contains(role, []byte(`"role"`)) {
		var raw struct {
			Role string `json:"role"`
		}
		if json.Unmarshal(role, &raw) == nil && raw.Role == "session" {
			if _, ok := files["session.json"]; !ok {
				files["session.json"] = newObject()
			}
		}
	}
	files["model.json"].prepend("nightseam", 2)
	// Each file reads in one order whatever order its sections came in.
	for file, order := range map[string][]string{
		"model.json":    {"nightseam", "imports", "types"},
		"protocol.json": {"profile", "parameters", "imports", "types", "server", "client", "errors"},
		"session.json":  {"decides", "asks", "conversation", "extensions", "imports", "types"},
	} {
		if object, ok := files[file]; ok {
			object.order(order...)
		}
	}
	out := map[string][]byte{}
	for file, object := range files {
		if err := object.write(out, file); err != nil {
			return nil, err
		}
	}
	for target, object := range overrides {
		if len(object.keys) == 0 {
			continue
		}
		wrapped := newObject()
		wrapped.set("names", object)
		if err := wrapped.write(out, target+".json"); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// convertLayer moves one layer file's sections into the tier files they
// belong to.
func convertLayer(layer string, raw map[string]json.RawMessage, files map[string]*object, overrides map[string]*object) error {
	single := layer == ""
	tierFile := "model.json"
	for _, l := range Layers {
		if l.Layer == layer {
			tierFile = l.File
		}
	}
	tier := func(file string) *object {
		if files[file] == nil {
			files[file] = newObject()
		}
		return files[file]
	}
	// Every family a declaration names is imported in the new language: the
	// ones the layer imported, and the ones its slots and applications name.
	named := map[string]bool{}
	if imports, ok := raw["imports"]; ok {
		var names []string
		if err := json.Unmarshal(imports, &names); err != nil {
			return fmt.Errorf("imports: %w", err)
		}
		for _, name := range names {
			named[name] = true
		}
	}
	for _, key := range []string{"types", "methods", "events"} {
		if raw, ok := raw[key]; ok {
			collectNamed(raw, named)
		}
	}
	if len(named) > 0 {
		names := sortedKeys(named)
		importsInto := tierFile
		if single {
			// One file holds everything: what a slot or an application names
			// is of the protocol, where the types that hold them go.
			importsInto = "protocol.json"
			if raw, ok := raw["imports"]; ok {
				var declared []string
				if json.Unmarshal(raw, &declared) == nil && len(declared) > 0 {
					importsInto = "model.json"
				}
			}
		}
		tier(importsInto).set("imports", names)
	}
	if rawTypes, ok := raw["types"]; ok {
		var types map[string]map[string]json.RawMessage
		if err := json.Unmarshal(rawTypes, &types); err != nil {
			return fmt.Errorf("types: %w", err)
		}
		names := sortedKeys(types)
		converted := map[string]*object{}
		protocolTier := map[string]bool{}
		for _, name := range names {
			out, holdsSlot, err := convertType(name, types[name], overrides["go"])
			if err != nil {
				return err
			}
			converted[name] = out
			protocolTier[name] = holdsSlot
		}
		if single {
			// One file holds everything: a type that draws on a parameter
			// or another family's messages is of the protocol, and so is a
			// type that refers to one, since a declaration refers to its
			// own tier or a lower one.
			for changed := true; changed; {
				changed = false
				for _, name := range names {
					if protocolTier[name] {
						continue
					}
					for _, referred := range referredTypes(types[name], types) {
						if protocolTier[referred] {
							protocolTier[name] = true
							changed = true
							break
						}
					}
				}
			}
		}
		for _, name := range names {
			file := tierFile
			if single && protocolTier[name] {
				file = "protocol.json"
			}
			tier(file).object("types").set(name, converted[name])
		}
	}
	if single || layer == "rpc" || layer == "sess" {
		protocol := func() *object {
			p := tier("protocol.json")
			if !p.has("profile") {
				profile := json.RawMessage(`"nightseam.duplex/1"`)
				if v, ok := raw["profile"]; ok {
					profile = json.RawMessage(v)
				}
				p.prepend("profile", profile)
			}
			return p
		}
		if parameters, ok := raw["parameters"]; ok {
			protocol().set("parameters", json.RawMessage(parameters))
		}
		if rawMethods, ok := raw["methods"]; ok {
			if err := convertMethods(rawMethods, protocol(), overrides); err != nil {
				return err
			}
		}
		if rawEvents, ok := raw["events"]; ok {
			if err := convertEvents(rawEvents, protocol(), overrides); err != nil {
				return err
			}
		}
		if rawErrors, ok := raw["errors"]; ok {
			var errors []struct{ Code, Description string }
			if err := json.Unmarshal(rawErrors, &errors); err != nil {
				return fmt.Errorf("errors: %w", err)
			}
			if len(errors) > 0 {
				codes := newObject()
				sort.Slice(errors, func(i, j int) bool { return errors[i].Code < errors[j].Code })
				for _, e := range errors {
					codes.set(e.Code, e.Description)
				}
				protocol().set("errors", codes)
			}
		}
		if rawSession, ok := raw["session"]; ok {
			var session map[string]json.RawMessage
			if err := json.Unmarshal(rawSession, &session); err != nil {
				return fmt.Errorf("session: %w", err)
			}
			s := tier("session.json")
			for _, key := range []string{"decides", "asks", "conversation"} {
				if v, ok := session[key]; ok {
					s.set(key, json.RawMessage(v))
				}
			}
			extensions := newObject()
			for _, key := range []string{"options", "platforms"} {
				if v, ok := session[key]; ok {
					extensions.set(key, json.RawMessage(v))
				}
			}
			if len(extensions.keys) > 0 {
				s.set("extensions", extensions)
			}
		}
	}
	return nil
}

// referredTypes names the declared types one type's raw JSON refers to, in
// its fields, its alias target and its parents.
func referredTypes(t map[string]json.RawMessage, declared map[string]map[string]json.RawMessage) []string {
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			if _, ok := declared[x]; ok {
				out = append(out, x)
			}
		case map[string]any:
			for key, child := range x {
				if key == "apply" || key == "with" || key == "name" || key == "description" {
					continue
				}
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	for _, key := range []string{"fields", "type", "extends"} {
		if raw, ok := t[key]; ok {
			var value any
			if json.Unmarshal(raw, &value) == nil {
				walk(value)
			}
		}
	}
	return out
}

// collectNamed finds every family the raw JSON names as a slot's family or
// an application's filler, anywhere beneath it.
func collectNamed(raw json.RawMessage, named map[string]bool) {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return
	}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for _, slot := range []string{"envelope", "connection"} {
				if target, ok := x[slot].(string); ok && !isParameter(target) {
					named[target] = true
				}
			}
			if reference, ok := x["apply"].(string); ok {
				if family, _, ok := strings.Cut(reference, "."); ok {
					named[family] = true
				}
				if with, ok := x["with"].(map[string]any); ok {
					for _, filler := range with {
						if target, ok := filler.(string); ok && !isParameter(target) {
							named[target] = true
						}
					}
				}
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(value)
}

func isParameter(name string) bool { return name != "" && name[0] >= 'A' && name[0] <= 'Z' }

// convertType rewrites one type: its expressions to the one reference
// form, a field's go_name to an override where the convention differs.
func convertType(name string, t map[string]json.RawMessage, goOverrides *object) (*object, bool, error) {
	out := newObject()
	holdsSlot := false
	for _, key := range []string{"kind", "description", "extends", "open", "values"} {
		if v, ok := t[key]; ok {
			out.set(key, json.RawMessage(v))
		}
	}
	if rawFields, ok := t["fields"]; ok {
		var fields []map[string]json.RawMessage
		if err := json.Unmarshal(rawFields, &fields); err != nil {
			return nil, false, fmt.Errorf("%s fields: %w", name, err)
		}
		converted := make([]any, 0, len(fields))
		for _, field := range fields {
			f := newObject()
			var fieldName string
			if err := json.Unmarshal(field["name"], &fieldName); err != nil {
				return nil, false, fmt.Errorf("%s field: %w", name, err)
			}
			f.set("name", fieldName)
			if v, ok := field["description"]; ok {
				f.set("description", json.RawMessage(v))
			}
			expr, slot, err := convertExpr(field["type"])
			if err != nil {
				return nil, false, fmt.Errorf("%s.%s: %w", name, fieldName, err)
			}
			holdsSlot = holdsSlot || slot
			f.set("type", expr)
			for _, key := range []string{"required", "nullable"} {
				if v, ok := field[key]; ok {
					f.set(key, json.RawMessage(v))
				}
			}
			if v, ok := field["go_name"]; ok {
				var goName string
				if err := json.Unmarshal(v, &goName); err == nil && goName != naming.UpperCamel(fieldName) {
					goOverrides.set(name+"."+fieldName, goName)
				}
			}
			converted = append(converted, f)
		}
		out.set("fields", converted)
	}
	if v, ok := t["type"]; ok {
		expr, slot, err := convertExpr(v)
		if err != nil {
			return nil, false, fmt.Errorf("%s: %w", name, err)
		}
		holdsSlot = holdsSlot || slot
		out.set("type", expr)
	}
	return out, holdsSlot, nil
}

type operation struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	GoName      string          `json:"go_name"`
	TSName      string          `json:"ts_name"`
	Direction   string          `json:"direction"`
	Request     json.RawMessage `json:"request"`
	Result      json.RawMessage `json:"result"`
	Type        json.RawMessage `json:"type"`
}

func convertMethods(raw json.RawMessage, protocol *object, overrides map[string]*object) error {
	var methods []operation
	if err := json.Unmarshal(raw, &methods); err != nil {
		return fmt.Errorf("methods: %w", err)
	}
	sort.Slice(methods, func(i, j int) bool { return methods[i].Name < methods[j].Name })
	for _, m := range methods {
		side := "server"
		if m.Direction == "server_to_client" {
			side = "client"
		}
		out := newObject()
		if m.Description != "" {
			out.set("description", m.Description)
		}
		if m.Request != nil {
			expr, _, err := convertExpr(m.Request)
			if err != nil {
				return fmt.Errorf("method %s request: %w", m.Name, err)
			}
			out.set("request", expr)
		}
		expr, _, err := convertExpr(m.Result)
		if err != nil {
			return fmt.Errorf("method %s result: %w", m.Name, err)
		}
		out.set("result", expr)
		protocol.object(side).object("methods").set(m.Name, out)
		noteNames(m, overrides)
	}
	return nil
}

func convertEvents(raw json.RawMessage, protocol *object, overrides map[string]*object) error {
	var events []operation
	if err := json.Unmarshal(raw, &events); err != nil {
		return fmt.Errorf("events: %w", err)
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Name < events[j].Name })
	for _, e := range events {
		side := "server"
		if e.Direction == "client_to_server" {
			side = "client"
		}
		out := newObject()
		if e.Description != "" {
			out.set("description", e.Description)
		}
		expr, _, err := convertExpr(e.Type)
		if err != nil {
			return fmt.Errorf("event %s: %w", e.Name, err)
		}
		out.set("type", expr)
		protocol.object(side).object("events").set(e.Name, out)
		noteNames(e, overrides)
	}
	return nil
}

// noteNames records an operation's hand-spelled names as overrides where
// the convention would spell them otherwise.
func noteNames(op operation, overrides map[string]*object) {
	if op.GoName != "" && op.GoName != naming.UpperCamel(op.Name) {
		overrides["go"].set(op.Name, op.GoName)
	}
	if op.TSName != "" && op.TSName != naming.LowerCamel(op.Name) {
		overrides["typescript"].set(op.Name, op.TSName)
	}
}

// convertExpr rewrites a type expression of the previous language: the
// envelope and connection slot objects become Target.Envelope and
// Target.Handle, everything else stays. It reports whether the expression
// draws on a parameter or another family's messages.
func convertExpr(raw json.RawMessage) (any, bool, error) {
	if raw == nil {
		return nil, false, fmt.Errorf("a type expression is required")
	}
	trimmed := bytes.TrimSpace(raw)
	if trimmed[0] == '"' {
		var name string
		if err := json.Unmarshal(raw, &name); err != nil {
			return nil, false, err
		}
		qualifier, _, qualified := strings.Cut(name, ".")
		return name, qualified && qualifier != "" && qualifier[0] >= 'A' && qualifier[0] <= 'Z', nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, false, err
	}
	for slot, typeName := range map[string]string{"envelope": "Envelope", "connection": "Handle"} {
		if target, ok := object[slot]; ok {
			var name string
			if err := json.Unmarshal(target, &name); err != nil {
				return nil, false, err
			}
			return name + "." + typeName, true, nil
		}
	}
	for _, container := range []string{"array", "map"} {
		if elem, ok := object[container]; ok {
			converted, slot, err := convertExpr(elem)
			if err != nil {
				return nil, false, err
			}
			out := newObject()
			out.set(container, converted)
			return out, slot, nil
		}
	}
	if _, ok := object["apply"]; ok {
		out := newObject()
		out.set("apply", json.RawMessage(object["apply"]))
		out.set("with", json.RawMessage(object["with"]))
		return out, true, nil
	}
	return nil, false, fmt.Errorf("unknown type expression %s", raw)
}

// object is a JSON object that marshals its members in the order they were
// set, so that a converted file reads as a hand-written one would.
type object struct {
	keys   []string
	values map[string]any
}

func newObject() *object { return &object{values: map[string]any{}} }

func (o *object) has(key string) bool { _, ok := o.values[key]; return ok }

func (o *object) set(key string, value any) {
	if !o.has(key) {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

func (o *object) prepend(key string, value any) {
	if o.has(key) {
		o.values[key] = value
		return
	}
	o.keys = append([]string{key}, o.keys...)
	o.values[key] = value
}

// order puts the named keys first, in that order, the rest after.
func (o *object) order(first ...string) {
	var keys []string
	for _, key := range first {
		if o.has(key) {
			keys = append(keys, key)
		}
	}
	for _, key := range o.keys {
		if !contains(first, key) {
			keys = append(keys, key)
		}
	}
	o.keys = keys
}

func contains(list []string, item string) bool {
	for _, member := range list {
		if member == item {
			return true
		}
	}
	return false
}

// object is the member object under key, made when absent.
func (o *object) object(key string) *object {
	if child, ok := o.values[key].(*object); ok {
		return child
	}
	child := newObject()
	o.set(key, child)
	return child
}

func (o *object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, key := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		name, _ := json.Marshal(key)
		b.Write(name)
		b.WriteByte(':')
		value, err := json.Marshal(o.values[key])
		if err != nil {
			return nil, err
		}
		b.Write(value)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func (o *object) write(out map[string][]byte, file string) error {
	data, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	out[file] = append(data, '\n')
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
