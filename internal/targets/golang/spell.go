package golang

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/Bitspark/nightseam/internal/emit"
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
)

// file is one Go file being emitted: the plan its names come from, the
// writer, the imports it registers as it spells them, and the prefix a
// type of the family's protocol package is spelled with — none in that
// package, protocol. in the others.
type file struct {
	plan         *plan
	family       *render.Family
	config       Config
	w            *emit.Writer
	imports      *emit.Imports
	prefix       string
	uses         []render.Use // lexical type parameters of the declaration being emitted
	codecs       []render.Use // parameters supplied with boundary converters
	scopedCodecs bool         // export converters receive the enclosing live helper's view
}

func (f *file) line(text string)                 { f.w.Line(text) }
func (f *file) linef(format string, args ...any) { f.w.Linef(format, args...) }

// use registers an import and returns the alias to spell it with.
func (f *file) use(alias, path string) string { return f.imports.Use(alias, path) }

// proto is the prefix a type of the family's protocol package is spelled
// with in this file — none in that package — registering the import where
// it is first spelled.
func (f *file) proto() string {
	if f.prefix != "" {
		f.use("protocol", f.config.Module+"/"+expand(f.config.layout(f.family.Name).Protocol, f.family.Name))
	}
	return f.prefix
}

func (f *file) runtime() string { return f.use("runtime", f.config.Runtime) }
func (f *file) seam() string    { return f.use("duplex", f.config.Seam) }
func (f *file) tunnel() string  { return f.use("tunnel", f.config.Tunnel) }
func (f *file) std(name string) string {
	paths := map[string]string{"json": "encoding/json", "http": "net/http"}
	if p, ok := paths[name]; ok {
		return f.use(name, p)
	}
	return f.use(name, name)
}

// peer is the alias of an imported family's protocol package.
func (f *file) peer(family string) string {
	return f.use(importAlias(family), f.config.Module+"/"+expand(f.config.layout(family).Protocol, family))
}

// spell is the Go type of a type expression: a primitive's Go type, a type
// of this family with its type arguments, an imported family's through
// its package, a type drawn from a parameter as the type parameter it
// becomes, an application with its arguments filled.
func (f *file) spell(e model.TypeExpr) string {
	switch x := e.(type) {
	case model.Primitive:
		switch x {
		case "string":
			return "string"
		case "boolean":
			return "bool"
		case "number":
			return "float64"
		case "integer":
			return "int64"
		case "timestamp":
			return f.std("time") + ".Time"
		default:
			return "any"
		}
	case model.Named:
		for _, use := range f.uses {
			if use.Parameter == x.Name && use.Type == "" {
				return parameterName(use)
			}
		}
		return f.named("", x.Name) + apply(f.family.Type(x.Name).Uses)
	case model.Imported:
		return f.named(x.Family, x.Name) + f.arguments(f.family.Arguments(model.Apply{Family: x.Family, Name: x.Name}))
	case model.Drawn:
		return parameterName(render.Use{Parameter: x.Parameter, Type: x.Name})
	case model.Array:
		return "[]" + f.spell(x.Elem)
	case model.Map:
		return "map[string]" + f.spell(x.Elem)
	case model.Nullable:
		return f.runtime() + ".Nullable[" + f.spell(x.Elem) + "]"
	case model.Literal:
		return f.proto() + f.plan.literals[x.Value].name
	case model.Ref:
		return f.spell(f.keyType(x.Entity))
	case model.Apply:
		return f.named(x.Family, x.Name) + f.arguments(f.family.Arguments(x))
	case model.Inline:
		t := f.family.InlineType(x)
		arguments := apply(t.Uses)
		if t.Arguments != nil {
			arguments = f.arguments(t.Arguments)
		}
		return f.named(t.Origin.Family, t.Name) + arguments
	}
	return "any"
}

func (f *file) named(family, name string) string {
	if family == "" || family == f.family.Name {
		return f.proto() + f.plan.types[name]
	}
	source := f.family.ReferencedFamily(family)
	if source != nil {
		if override, ok := source.Override(Name, name); ok {
			name = override
		}
	}
	return f.peer(family) + "." + name
}

func (f *file) arguments(arguments []render.Argument) string {
	if len(arguments) == 0 {
		return ""
	}
	var args []string
	for _, argument := range arguments {
		switch {
		case argument.Type != nil:
			args = append(args, f.spell(argument.Type))
		case argument.Parameter != "":
			args = append(args, parameterName(render.Use{Parameter: argument.Parameter, Type: argument.Use.Type}))
		default:
			args = append(args, f.named(argument.Family, argument.Use.Type))
		}
	}
	return "[" + strings.Join(args, ", ") + "]"
}

// keyType is the type of an entity's key: what a ref to it is on the wire.
func (f *file) keyType(entity string) model.TypeExpr {
	t := f.family.Type(entity)
	for _, field := range t.Fields {
		if field.Name == t.Key {
			return field.Type
		}
	}
	return model.Primitive("string")
}

// fieldType is a field's Go type: its expression's, wrapped for nullness
// and presence.
func (f *file) fieldType(field render.Field) string {
	t := f.spell(field.Type)
	if field.Nullable {
		t = f.runtime() + ".Nullable[" + t + "]"
	}
	if !field.Required {
		t = f.runtime() + ".Optional[" + t + "]"
	}
	return t
}

// expression is a type expression as the validator reads it, quoted.
func expression(e model.TypeExpr) string { return quote(model.String(e)) }

func quote(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func parameters(uses []render.Use) []string {
	names := make([]string, len(uses))
	for i, use := range uses {
		names[i] = parameterName(use)
	}
	return names
}

// declare renders the type parameters of a declaration that makes the
// uses, [SEnvelope, SHandle any], or nothing for a plain one.
func declare(uses []render.Use) string {
	if len(uses) == 0 {
		return ""
	}
	return "[" + strings.Join(parameters(uses), ", ") + " any]"
}

// apply renders the type arguments a reference passes on, [SEnvelope,
// SHandle], or nothing.
func apply(uses []render.Use) string {
	if len(uses) == 0 {
		return ""
	}
	return "[" + strings.Join(parameters(uses), ", ") + "]"
}

// entry renders the type parameters of an entry point that makes the
// uses: each drawn type constrained to its parameter's tag, then the tags,
// which come last so that a caller spelling the drawn types may stop there.
func (f *file) entry(uses []render.Use) string {
	if len(uses) == 0 {
		return ""
	}
	var constrained, tags []string
	for _, use := range uses {
		if use.Type == "" {
			constrained = append(constrained, parameterName(use)+" any")
			continue
		}
		constrained = append(constrained, parameterName(use)+" "+f.runtime()+".Of["+tagName(use.Parameter)+"]")
		if tag := tagName(use.Parameter); !slices.Contains(tags, tag) {
			tags = append(tags, tag)
		}
	}
	if len(tags) > 0 {
		constrained = append(constrained, strings.Join(tags, ", ")+" any")
	}
	return "[" + strings.Join(constrained, ", ") + "]"
}

// request is the parameter a method's signature takes, or nothing.
func (f *file) request(m render.Method) string {
	if m.Request == nil {
		return ""
	}
	return ", params " + f.spell(m.Request)
}

// argument is what a caller sends: params, or an empty object.
func argument(m render.Method) string {
	if m.Request == nil {
		return "struct{}{}"
	}
	return "params"
}
