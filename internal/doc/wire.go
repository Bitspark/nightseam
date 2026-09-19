package doc

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
)

// The wire layer of the document: what a value of a type looks like, what
// an exchange looks like as frames of the profile, and how much each side
// says. All of it is synthesized from the declaration, deterministically,
// so that a page shows a reader the wire without a peer having run, and so
// that a test can hold every example to the validators.

// Frames is one exchange as the frames of the nightseam.duplex/1 profile
// carry it: the request, the response that answers it, and a refusal for
// each error the method declares.
type Frames struct {
	Request  json.RawMessage
	Response json.RawMessage
	Refusals []Refusal
}

// Refusal is the response that answers a request with one of the method's
// declared errors.
type Refusal struct {
	Code  string
	Frame json.RawMessage
}

// Weight is how much each side says in an exchange, counted in the values
// its example carries: what a request's params carry, and what the result
// or an event's data carries.
type Weight struct{ Request, Result int }

// Reference is one place a type is used: an operation of a side, at its
// request, result or data, or a field of another type.
type Reference struct {
	Side string // server or client; empty for a type
	Kind string // method, event or type
	Name string // the operation's, or the type's
	At   string // request, result, data, or the field's name
}

// Timestamp is the one timestamp every example carries.
const Timestamp = "2026-01-01T00:00:00Z"

// value is an example value as it is built: an object with its members in
// order, an array, or a scalar as its JSON text. It is serialized once,
// canonically, when the document takes it.
type value struct {
	members []member // an object's, in order; nil for an object with none
	items   []value  // an array's
	scalar  json.RawMessage
	kind    byte // 'o', 'a' or 's'
}

// member is one member of an example object.
type member struct {
	key string
	value
}

func scalar(raw string) value        { return value{kind: 's', scalar: json.RawMessage(raw)} }
func object(members ...member) value { return value{kind: 'o', members: members} }
func array(items ...value) value     { return value{kind: 'a', items: items} }
func text(s string) value            { data, _ := json.Marshal(s); return value{kind: 's', scalar: data} }
func placeholder(name string) value  { return text("‹" + name + "›") }
func (v value) isObject() bool       { return v.kind == 'o' }
func (v value) raw() json.RawMessage {
	var b bytes.Buffer
	v.write(&b)
	return json.RawMessage(b.Bytes())
}

var null = scalar("null")

func (v value) write(b *bytes.Buffer) {
	switch v.kind {
	case 'o':
		b.WriteByte('{')
		for i, m := range v.members {
			if i > 0 {
				b.WriteByte(',')
			}
			b.Write(text(m.key).scalar)
			b.WriteByte(':')
			m.value.write(b)
		}
		b.WriteByte('}')
	case 'a':
		b.WriteByte('[')
		for i, item := range v.items {
			if i > 0 {
				b.WriteByte(',')
			}
			item.write(b)
		}
		b.WriteByte(']')
	default:
		b.Write(v.scalar)
	}
}

// weight counts the values an example carries: every scalar that is not
// null, an empty object or array counting nothing.
func (v value) weight() int {
	switch v.kind {
	case 'o':
		n := 0
		for _, m := range v.members {
			n += m.value.weight()
		}
		return n
	case 'a':
		n := 0
		for _, item := range v.items {
			n += item.weight()
		}
		return n
	}
	if string(v.scalar) == "null" {
		return 0
	}
	return 1
}

// exampler synthesizes example values of a family's types. A placeholder
// string is the member's name between angle quotes, ‹text›, so that a
// reader sees which member a value stands for; a number is its lower bound
// or zero; an enum is its first value; a union is its first variant by
// tag, which is the order the model holds them in, a declaration's
// variants being members of an object; a recursion is folded to null where
// the type reappears on the path.
type exampler struct {
	f     *render.Family
	trail map[string]bool
	// subst is a type's parameters as an application fills them: each an
	// example of the filler, taken in the applying family's scope, which is
	// where the filler was spelled.
	subst map[string]filler
}

type filler func(name string, c constraints) value

func newExampler(f *render.Family) *exampler {
	return &exampler{f: f, trail: map[string]bool{}, subst: map[string]filler{}}
}

func (x *exampler) in(f *render.Family) *exampler {
	return &exampler{f: f, trail: x.trail, subst: map[string]filler{}}
}

// constraints are the constraints a field puts on its value.
type constraints struct {
	min, max *json.Number
	length   *model.Length
}

func of(f model.Field) constraints { return constraints{min: f.Min, max: f.Max, length: f.Length} }

// fieldOf is a render field as a model field, for its constraints.
func fieldOf(f render.Field) model.Field {
	return model.Field{Name: f.Name, Type: f.Type, Required: f.Required, Nullable: f.Nullable, Min: f.Min, Max: f.Max, Length: f.Length, Pattern: f.Pattern}
}

// example is an example value of a type of the family.
func (x *exampler) example(t *render.Type) value { return x.typed(t, t.Name, constraints{}) }

// value is an example of an expression; name is what a placeholder stands
// for.
func (x *exampler) value(e model.TypeExpr, name string, c constraints) value {
	switch v := e.(type) {
	case nil:
		return null
	case model.Primitive:
		return x.primitive(v, name, c)
	case model.Named:
		if filled, ok := x.subst[v.Name]; ok {
			return filled(name, c)
		}
		if t := x.f.Type(v.Name); t != nil {
			return x.typed(t, name, c)
		}
		return placeholder(v.Name)
	case model.Imported:
		if other := x.f.ReferencedFamily(v.Family); other != nil && other != x.f {
			if t := other.Type(v.Name); t != nil {
				return x.in(other).typed(t, name, c)
			}
		}
		return placeholder(v.Family + "." + v.Name)
	case model.Drawn:
		return placeholder(v.Parameter + "." + v.Name)
	case model.Array:
		return array(x.value(v.Elem, name, c))
	case model.Map:
		return object(member{"‹key›", x.value(v.Elem, name, c)})
	case model.Nullable:
		return x.value(v.Elem, name, c)
	case model.Literal:
		return text(v.Value)
	case model.Ref:
		if t := x.f.Type(v.Entity); t != nil && t.Key != "" {
			for _, field := range t.Fields {
				if field.Name == t.Key {
					return x.value(field.Type, field.Name, of(fieldOf(field)))
				}
			}
		}
		return placeholder(v.Entity)
	case model.Apply:
		return x.applied(v, name, c)
	case model.Inline:
		return x.shape(v.Type.Kind, v.Type.Fields, v.Type.Values, v.Type.Alias, v.Type.Tag, v.Type.ValueMember(), v.Type.Variants, name, c)
	}
	return null
}

func (x *exampler) primitive(p model.Primitive, name string, c constraints) value {
	switch p {
	case "string":
		return text(bounded("‹"+name+"›", c.length))
	case "boolean":
		return scalar("true")
	case "integer":
		return number("0", c)
	case "number":
		return number("0.5", c)
	case "timestamp":
		return text(Timestamp)
	case "json":
		return object()
	}
	return null
}

// typed is an example of a declared type, folded to null where it recurs.
func (x *exampler) typed(t *render.Type, name string, c constraints) value {
	key := x.f.Name + "." + t.Name
	if x.trail[key] {
		return null
	}
	x.trail[key] = true
	defer delete(x.trail, key)
	var fields []model.Field
	for _, field := range t.Fields {
		fields = append(fields, fieldOf(field))
	}
	var variants []model.Variant
	for _, variant := range t.Variants {
		variants = append(variants, variant.Variant)
	}
	return x.shape(t.Kind, fields, t.Values, t.Alias, t.Tag, t.Value, variants, name, c)
}

// shape is an example of a type by its kind.
func (x *exampler) shape(kind string, fields []model.Field, values []string, alias model.TypeExpr, tag, valueMember string, variants []model.Variant, name string, c constraints) value {
	switch kind {
	case model.KindRecord, model.KindEntity:
		return x.record(fields)
	case model.KindEnum:
		if len(values) == 0 {
			return null
		}
		return text(values[0])
	case model.KindAlias:
		return x.value(alias, name, c)
	case model.KindUnion:
		if len(variants) == 0 {
			return null
		}
		return x.variant(tag, valueMember, variants[0])
	}
	return null
}

// record is an example record: every field, in wire order.
func (x *exampler) record(fields []model.Field) value {
	var members []member
	for _, field := range fields {
		members = append(members, member{field.Name, x.value(field.Type, field.Name, of(field))})
	}
	return object(members...)
}

// variant is an example of a union at one variant, as the wire carries
// it: the tag, and the complete payload under the union's value member —
// a record, a map, a scalar, null, whatever the variant admits, so that
// every payload survives the wire unchanged — or, for an arm declared with
// no payload, the tag alone.
func (x *exampler) variant(tag, valueMember string, v model.Variant) value {
	tagged := member{tag, text(v.Tag)}
	if v.Type == nil {
		return object(tagged)
	}
	return object(tagged, member{valueMember, x.value(v.Type, v.Tag, constraints{})})
}

// applied is an example of a generic type with its parameters filled.
func (x *exampler) applied(a model.Apply, name string, c constraints) value {
	f := x.f
	if a.Family != "" {
		if f = x.f.ReferencedFamily(a.Family); f == nil {
			return placeholder(a.Family + "." + a.Name)
		}
	}
	t := f.Type(a.Name)
	if t == nil {
		return placeholder(a.Name)
	}
	y := x.in(f)
	for _, p := range t.Parameters {
		if with, ok := a.With[p.Name]; ok && with.Type != nil {
			// A filler is spelled in the applying family's scope, so its
			// example is taken there, by this exampler, when the applied
			// type reaches the parameter.
			expr := with.Type
			y.subst[p.Name] = func(name string, c constraints) value { return x.value(expr, name, c) }
		}
	}
	return y.typed(t, name, c)
}

// bounded holds a placeholder to a field's length: padded with itself to
// the least length, cut to the greatest.
func bounded(s string, length *model.Length) string {
	if length == nil {
		return s
	}
	runes := []rune(s)
	if length.Min != nil {
		for len(runes) < *length.Min {
			runes = append(runes, []rune(s)...)
		}
	}
	if length.Max != nil && len(runes) > *length.Max {
		runes = runes[:*length.Max]
	}
	return string(runes)
}

// number is a number within a field's bounds: the least where there is
// one, else the default where it is within the greatest, else the greatest.
func number(zero string, c constraints) value {
	if c.min != nil {
		return scalar(c.min.String())
	}
	if c.max != nil {
		if max, err := c.max.Float64(); err == nil {
			if def, _ := strconv.ParseFloat(zero, 64); def > max {
				return scalar(c.max.String())
			}
		}
	}
	return scalar(zero)
}

// envelope is one frame of the profile: version 1, a kind, and the
// members of that kind.
func envelope(kind string, members ...member) json.RawMessage {
	return object(append([]member{{"version", scalar("1")}, {"kind", text(kind)}}, members...)...).raw()
}

// frames is one method's exchange on the wire. A method of the server is
// called by the client, whose ids are c:N; one of the client by the server,
// s:N. params is {} where the method takes nothing, result null where it
// answers with nothing, and a refusal's message is the error's description
// or, where it has none, its code, since the profile has it non-empty.
func (x *exampler) frames(side string, m render.Method, errors []render.Error) Frames {
	id := "c:1"
	if side == "client" {
		id = "s:1"
	}
	params := object()
	if m.Request != nil {
		params = x.value(m.Request, "params", constraints{})
	}
	result := null
	if m.Result != nil {
		result = x.value(m.Result, "result", constraints{})
	}
	out := Frames{
		Request:  envelope("request", member{"id", text(id)}, member{"method", text(m.Name)}, member{"params", params}),
		Response: envelope("response", member{"id", text(id)}, member{"result", result}),
	}
	described := map[string]string{}
	for _, e := range errors {
		described[e.Code] = e.Description
	}
	for _, code := range m.Errors {
		message := strings.TrimSpace(described[code])
		if message == "" {
			message = code
		}
		refusal := object(member{"code", text(code)}, member{"message", text(message)})
		out.Refusals = append(out.Refusals, Refusal{Code: code, Frame: envelope("response", member{"id", text(id)}, member{"error", refusal})})
	}
	return out
}

// event is one event's frame on the wire: data null where it carries
// nothing.
func (x *exampler) event(e render.Event) json.RawMessage {
	data := null
	if e.Type != nil {
		data = x.value(e.Type, "data", constraints{})
	}
	return envelope("event", member{"event", text(e.Name)}, member{"data", data})
}

// references indexes where every type of the family is used: by the fields
// of other types, and by the operations of both sides at their request,
// result or data, each once, in the order the declaration reaches them.
func references(f *render.Family) map[string][]Reference {
	out := map[string][]Reference{}
	seen := map[string]bool{}
	add := func(typeName string, r Reference) {
		key := typeName + "|" + r.Side + "|" + r.Kind + "|" + r.Name + "|" + r.At
		if !seen[key] {
			seen[key] = true
			out[typeName] = append(out[typeName], r)
		}
	}
	names := func(e model.TypeExpr, visit func(string)) {
		model.Walk(e, func(x model.TypeExpr) bool {
			switch v := x.(type) {
			case model.Named:
				if f.Type(v.Name) != nil {
					visit(v.Name)
				}
			case model.Ref:
				if v.Family == "" || v.Family == f.Name {
					visit(v.Entity)
				}
			case model.Apply:
				if v.Family == "" && f.Type(v.Name) != nil {
					visit(v.Name)
				}
			}
			return true
		})
	}
	for _, t := range f.Types {
		for _, field := range t.Own {
			names(field.Type, func(name string) { add(name, Reference{Kind: "type", Name: t.Name, At: field.Name}) })
		}
		if t.Alias != nil {
			names(t.Alias, func(name string) { add(name, Reference{Kind: "type", Name: t.Name, At: "alias"}) })
		}
		for _, variant := range t.Variants {
			names(variant.Type, func(name string) { add(name, Reference{Kind: "type", Name: t.Name, At: variant.Tag}) })
		}
	}
	for _, side := range []struct {
		name string
		side render.Side
	}{{"server", f.Server}, {"client", f.Client}} {
		for _, m := range side.side.Methods {
			names(m.Request, func(name string) { add(name, Reference{Side: side.name, Kind: "method", Name: m.Name, At: "request"}) })
			names(m.Result, func(name string) { add(name, Reference{Side: side.name, Kind: "method", Name: m.Name, At: "result"}) })
		}
		for _, e := range side.side.Events {
			names(e.Type, func(name string) { add(name, Reference{Side: side.name, Kind: "event", Name: e.Name, At: "data"}) })
		}
	}
	return out
}
