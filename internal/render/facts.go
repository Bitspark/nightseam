package render

import (
	"slices"
	"sort"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/model"
)

type builder struct {
	families map[*analysis.Family]*Family
}

func (r *Family) other(name string) *Family {
	if name == "" || name == r.Name {
		return r
	}
	if f := r.f.Imported[name]; f != nil {
		return r.builder.build(f)
	}
	seen := map[*analysis.Family]bool{}
	var find func(*analysis.Family) *analysis.Family
	find = func(f *analysis.Family) *analysis.Family {
		if seen[f] {
			return nil
		}
		seen[f] = true
		if f.Name == name {
			return f
		}
		for _, imported := range f.Imported {
			if found := find(imported); found != nil {
				return found
			}
		}
		return nil
	}
	if found := find(r.f); found != nil {
		return r.builder.build(found)
	}
	return nil
}

// InlineType associates an original inline expression with the declaration
// its target renders. Its source location, rather than a rewritten name,
// remains the identity used by diagnostics and the wire descriptor.
func (r *Family) InlineType(inline model.Inline) *Type {
	if t := r.inlines[inline.Type]; t != nil {
		return t
	}
	for _, family := range r.builder.families {
		if t := family.inlines[inline.Type]; t != nil {
			return t
		}
	}
	return nil
}

func (r *Family) complete() {
	for _, name := range r.References {
		r.other(name)
	}
	for _, inline := range r.f.Inlines() {
		t := inline.Type
		scope := slices.Clone(r.f.Parameters())
		if len(inline.Path) > 0 {
			if parent := r.Type(inline.Path[0]); parent != nil {
				scope = slices.Clone(parent.Scope)
			}
		}
		rt := &Type{Name: inline.Name, Kind: t.Kind, Description: t.Description, Key: t.Key,
			Parameters: t.Parameters, Scope: scope, Open: t.Open, Values: t.Values,
			Alias: t.Alias, Tag: t.Tag, Value: t.ValueMember(), Declaration: t,
			Origin: Origin{Family: r.Name, Declaration: inline.Name, At: t.At}, Inline: true, At: t.At}
		for _, own := range t.Fields {
			rt.Own = append(rt.Own, field(rt.Name, own))
		}
		rt.Fields = slices.Clone(rt.Own)
		r.Types = append(r.Types, rt)
		r.types[rt.Name] = rt
		r.inlines[t] = rt
	}
	sort.Slice(r.Types, func(i, j int) bool { return r.Types[i].Name < r.Types[j].Name })
	for _, t := range r.Types {
		for i := range t.Fields {
			at := &t.Fields[i]
			at.Origin = Origin{Family: t.Origin.Family, Declaration: at.Owner, At: at.At}
		}
		for i := range t.Own {
			at := &t.Own[i]
			at.Origin = Origin{Family: t.Origin.Family, Declaration: t.Name, At: at.At}
		}
		t.Variants = r.variants(t, map[string]bool{})
		for _, variant := range t.Variants {
			if variant.Origin.Declaration == t.Name {
				t.OwnVariants = append(t.OwnVariants, variant)
			}
		}
	}
	if r.f.Protocol != nil {
		r.Server = r.resolvedSide(true)
		r.Client = r.resolvedSide(false)
		r.Errors = r.resolvedErrors()
	}
	r.resolveSession()
	r.surfaceReferences()
	r.resolveUses()
	for _, t := range r.Types {
		r.resolvePayloads(t)
	}
}

func (r *Family) resolveSession() {
	if r.f.Session == nil {
		return
	}
	out := &Session{Declaration: r.f.Session}
	seen := map[string]bool{}
	appendNames := func(to *[]string, names []string) {
		for _, name := range names {
			if !slices.Contains(*to, name) {
				*to = append(*to, name)
			}
		}
	}
	var visit func(*Family, bool)
	visit = func(source *Family, server bool) {
		label := "client"
		if server {
			label = "server"
		}
		key := source.Name + "." + label
		if seen[key] || source.f.Protocol == nil {
			return
		}
		seen[key] = true
		side := source.f.Protocol.Client
		if server {
			side = source.f.Protocol.Server
		}
		for _, name := range side.Extends {
			if base := source.other(name); base != nil {
				visit(base, server)
			}
		}
		session := source.f.Session
		if session == nil {
			return
		}
		if source != r {
			out.Inherited = append(out.Inherited, SessionSource{Family: source.Name, Side: label, Declaration: session})
		}
		resolved := source.Client
		if server {
			resolved = source.Server
		}
		for _, name := range session.Decides {
			for _, method := range resolved.Methods {
				if method.Name == name {
					appendNames(&out.Decides, []string{name})
					break
				}
			}
		}
		if !server {
			appendNames(&out.Asks, session.Asks)
		}
		if conversation := session.Conversation; conversation != nil {
			for _, event := range resolved.Events {
				if event.Name != conversation.Event {
					continue
				}
				duplicate := false
				for _, previous := range out.Conversations {
					if previous.Conversation == *conversation {
						duplicate = true
						break
					}
				}
				if !duplicate {
					out.Conversations = append(out.Conversations, ConversationSource{Family: source.Name, Conversation: *conversation})
				}
			}
		}
	}
	visit(r, true)
	visit(r, false)
	if len(out.Conversations) == 1 {
		conversation := out.Conversations[0].Conversation
		out.Conversation = &conversation
	}
	r.Session = out
}

func (r *Family) variants(t *Type, stack map[string]bool) []Variant {
	if stack[t.Name] {
		return nil
	}
	stack[t.Name] = true
	defer delete(stack, t.Name)
	var variants []Variant
	for _, parent := range t.Extends {
		if base := r.Type(parent); base != nil {
			variants = append(variants, r.variants(base, stack)...)
		}
	}
	for _, variant := range t.Declaration.Variants {
		variants = append(variants, Variant{Variant: variant, Origin: t.Origin})
	}
	// A diamond reaches the same declaration twice. Distinct declarations
	// with the same tag are deliberately not collapsed: check refuses those.
	type key struct {
		Origin
		Tag string
	}
	seen := map[key]bool{}
	var unique []Variant
	for _, variant := range variants {
		id := key{variant.Origin, variant.Tag}
		if !seen[id] {
			unique = append(unique, variant)
			seen[id] = true
		}
	}
	return unique
}

func (r *Family) surfaceReferences() {
	names := map[string]bool{}
	for _, name := range r.References {
		names[name] = true
	}
	visit := func(e model.TypeExpr) {
		model.Walk(e, func(e model.TypeExpr) bool {
			switch x := e.(type) {
			case model.Imported:
				if x.Family != r.Name {
					names[x.Family] = true
				}
			case model.Apply:
				if x.Family != "" && x.Family != r.Name {
					names[x.Family] = true
				}
				for _, filler := range x.With {
					if filler.Family != "" && filler.Family != r.Name {
						names[filler.Family] = true
					}
				}
			}
			return true
		})
	}
	for _, side := range []Side{r.Server, r.Client} {
		for _, m := range side.Methods {
			visit(m.Request)
			visit(m.Result)
		}
		for _, e := range side.Events {
			visit(e.Type)
		}
	}
	r.References = nil
	for name := range names {
		r.References = append(r.References, name)
	}
	sort.Strings(r.References)
}

func (r *Family) resolvedSide(server bool) Side {
	declared := r.f.Protocol.Server
	if !server {
		declared = r.f.Protocol.Client
	}
	out := Side{Extends: slices.Clone(declared.Extends), At: declared.At}
	seen := map[*analysis.Family]bool{}
	var visit func(*Family)
	visit = func(source *Family) {
		if seen[source.f] || source.f.Protocol == nil {
			return
		}
		seen[source.f] = true
		side := source.f.Protocol.Server
		if !server {
			side = source.f.Protocol.Client
		}
		for _, name := range side.Extends {
			if parent := source.other(name); parent != nil {
				visit(parent)
			}
		}
		for i := range side.Methods {
			m := &side.Methods[i]
			method := Method{Name: m.Name, Description: m.Description,
				Request: r.qualify(m.Request, source), Result: r.qualify(m.Result, source),
				Errors: slices.Clone(m.Errors), At: m.At, Declaration: m,
				Origin: Origin{Family: source.Name, Declaration: m.Name, At: m.At}}
			out.Methods = append(out.Methods, method)
			if source == r {
				out.OwnMethods = append(out.OwnMethods, method)
			}
		}
		for i := range side.Events {
			e := &side.Events[i]
			event := Event{Name: e.Name, Description: e.Description, Type: r.qualify(e.Type, source),
				At: e.At, Declaration: e, Origin: Origin{Family: source.Name, Declaration: e.Name, At: e.At}}
			out.Events = append(out.Events, event)
			if source == r {
				out.OwnEvents = append(out.OwnEvents, event)
			}
		}
	}
	visit(r)
	return out
}

func (r *Family) resolvedErrors() []Error {
	errors := map[string]Error{}
	seen := map[*analysis.Family]bool{}
	var visit func(*Family)
	visit = func(source *Family) {
		if seen[source.f] || source.f.Protocol == nil {
			return
		}
		seen[source.f] = true
		for _, side := range []model.Side{source.f.Protocol.Server, source.f.Protocol.Client} {
			for _, parent := range side.Extends {
				if base := source.other(parent); base != nil {
					visit(base)
				}
			}
		}
		for _, e := range source.f.Protocol.Errors {
			errors[e.Code] = Error{Code: e.Code, Description: e.Description, At: e.At,
				Origin: Origin{Family: source.Name, Declaration: e.Code, At: e.At}}
		}
	}
	visit(r)
	var out []Error
	for _, e := range errors {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// qualify preserves the declaring family's scope when an inherited
// operation is viewed from another family. Parameter names stay parameters.
func (r *Family) qualify(e model.TypeExpr, source *Family) model.TypeExpr {
	if source == r {
		return e
	}
	return r.substitute(e, source, nil, source.f.Parameters())
}
