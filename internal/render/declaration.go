package render

import (
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/model"
)

// CanonicalDeclaration is the language- and backend-independent graph of
// the family's declaration. Its exact bytes are specified in
// docs/declaration/declaration-identity.md. It is not the validator schema.
func CanonicalDeclaration(f *analysis.Family) string {
	g := newDeclarationGraph()
	return g.bytes(g.family(f))
}

// CanonicalExpression selects a type or application and only its reachable
// declaration content, using the same graph and encoding as a family.
func CanonicalExpression(f *analysis.Family, e model.TypeExpr) string {
	g := newDeclarationGraph()
	return g.bytes(g.expression(f, e, familyScope(f), nil))
}

// CanonicalApplication binds an already normalized family/type constructor
// graph to ordered closed argument graphs. Each argument has its own scope,
// so two arguments may carry different revisions of the same nominal path.
// The source-level normalizer resolves aliases before this operation.
func CanonicalApplication(template string, arguments []string) (string, error) {
	decode := func(source string) (declarationObject, error) {
		var graph declarationObject
		decoder := json.NewDecoder(strings.NewReader(source))
		decoder.UseNumber()
		if err := decoder.Decode(&graph); err != nil {
			return nil, err
		}
		if graph["version"] != json.Number("1") {
			return nil, fmt.Errorf("unsupported declaration graph version")
		}
		if _, ok := graph["definitions"].(map[string]any); !ok {
			return nil, fmt.Errorf("declaration graph lacks definitions")
		}
		if _, ok := graph["root"].(map[string]any); !ok {
			return nil, fmt.Errorf("declaration graph lacks root")
		}
		return graph, nil
	}
	graph, err := decode(template)
	if err != nil {
		return "", err
	}
	root := graph["root"].(map[string]any)
	path, ok := root["ref"].(string)
	if !ok {
		return "", fmt.Errorf("application needs a normalized constructor reference")
	}
	definitions := graph["definitions"].(map[string]any)
	constructor, ok := definitions[path].(map[string]any)
	if !ok {
		return "", fmt.Errorf("application constructor %q is absent", path)
	}
	if constructor["kind"] == "alias" {
		return "", fmt.Errorf("application alias must be normalized at its declaration")
	}
	parameters, _ := constructor["parameters"].([]any)
	captures, _ := constructor["captures"].([]any)
	if len(arguments) != len(parameters)+len(captures) {
		return "", fmt.Errorf("application expects %d arguments, got %d", len(parameters)+len(captures), len(arguments))
	}
	args := []any{}
	for _, source := range arguments {
		argument, err := decode(source)
		if err != nil {
			return "", err
		}
		argumentGraph := &declarationGraph{definitions: argument["definitions"].(map[string]any)}
		args = append(args, declarationObject{"graph": argumentGraph.document(argument["root"], true)})
	}
	g := &declarationGraph{definitions: definitions}
	return g.bytes(declarationObject{"apply": path, "arguments": args}), nil
}

type declarationObject = map[string]any
type declarationScope map[string]string
type declarationGraph struct {
	definitions map[string]any
	aliases     map[string]bool
}

func newDeclarationGraph() *declarationGraph {
	return &declarationGraph{definitions: map[string]any{}, aliases: map[string]bool{}}
}

func (g *declarationGraph) bytes(root any) string {
	data, err := json.Marshal(g.document(root, true))
	if err != nil {
		panic(err)
	}
	return string(data)
}

func (g *declarationGraph) document(root any, scopeApplication bool) declarationObject {
	if node, ok := root.(map[string]any); ok && scopeApplication {
		if path, ok := node["apply"].(string); ok {
			args := []any{}
			for _, arg := range node["arguments"].([]any) {
				if object, ok := arg.(map[string]any); ok && object["graph"] != nil {
					args = append(args, arg)
				} else {
					args = append(args, declarationObject{"graph": g.document(arg, true)})
				}
			}
			root = declarationObject{"apply": path, "arguments": args}
		}
	}
	// Alias normalization can make an argument unreachable. Select again
	// from the normalized root so unused arguments do not enter identity.
	selected := map[string]any{}
	var visit func(any)
	visit = func(value any) {
		switch x := value.(type) {
		case map[string]any:
			if _, scoped := x["graph"]; scoped {
				return
			}
			for _, key := range []string{"ref", "apply"} {
				if path, ok := x[key].(string); ok {
					if node, exists := g.definitions[path]; exists {
						if _, seen := selected[path]; !seen {
							selected[path] = node
							visit(node)
						}
					}
				}
			}
			for _, child := range x {
				visit(child)
			}
		case []any:
			for _, child := range x {
				visit(child)
			}
		}
	}
	visit(root)
	return declarationObject{"version": 1, "root": root, "definitions": selected}
}

func declarationRef(path string) any { return declarationObject{"ref": path} }
func parameterRef(path string) any   { return declarationObject{"parameter": path} }
func emptyExpression() any           { return declarationObject{"empty": true} }

func familyScope(f *analysis.Family) declarationScope {
	scope := declarationScope{}
	for _, p := range f.Parameters() {
		scope[p.Name] = f.Name + "/" + p.Name
	}
	return scope
}

func typeScope(f *analysis.Family, t *model.Type) declarationScope {
	scope := familyScope(f)
	for _, p := range t.Parameters {
		scope[p.Name] = f.Name + "/" + t.Name + "/" + p.Name
	}
	return scope
}

func parameters(parameters []model.Parameter) []any {
	out := []any{}
	for _, p := range parameters {
		out = append(out, declarationObject{"name": p.Name, "of": p.Of})
	}
	return out
}

func sortedSet(values []string) []string {
	out := append([]string{}, values...)
	sort.Strings(out)
	if len(out) == 0 {
		return out
	}
	unique := out[:1]
	for _, value := range out[1:] {
		if value != unique[len(unique)-1] {
			unique = append(unique, value)
		}
	}
	return unique
}

func (g *declarationGraph) family(f *analysis.Family) any {
	if f == nil {
		return declarationObject{"unresolved": "family"}
	}
	if _, seen := g.definitions[f.Name]; seen {
		return declarationRef(f.Name)
	}
	node := declarationObject{"kind": "family", "parameters": parameters(f.Parameters())}
	g.definitions[f.Name] = node
	types := declarationObject{}
	for _, name := range f.Family.TypeNames() {
		types[name] = g.named(f, name)
	}
	node["types"] = types
	node["server"] = g.side(f, true)
	node["client"] = g.side(f, false)
	errors := []string{}
	if f.Protocol != nil {
		for _, e := range f.Protocol.Errors {
			errors = append(errors, e.Code)
		}
	}
	node["errors"] = sortedSet(errors)
	return declarationRef(f.Name)
}

func (g *declarationGraph) side(f *analysis.Family, server bool) any {
	direction := "client"
	if server {
		direction = "server"
	}
	path := f.Name + "/$" + direction
	if _, seen := g.definitions[path]; seen {
		return declarationRef(path)
	}
	node := declarationObject{"kind": "side", "direction": direction, "parameters": parameters(f.Parameters())}
	g.definitions[path] = node
	methods, events := declarationObject{}, declarationObject{}
	extends := []any{}
	scope := familyScope(f)
	var sides []model.Side
	if f.Protocol != nil {
		if server {
			sides = append(sides, f.Protocol.Server)
		} else {
			sides = append(sides, f.Protocol.Client)
		}
	}
	if f.Live != nil {
		if server {
			sides = append(sides, f.Live.Server)
		} else {
			sides = append(sides, f.Live.Client)
		}
	}
	for _, side := range sides {
		for _, base := range side.Extends {
			owner := f.Imported[base.Name]
			if owner == nil {
				extends = append(extends, declarationObject{"unresolved": base.Name})
				continue
			}
			ref := g.side(owner, server)
			if base.Applied() {
				ref = declarationObject{"apply": owner.Name + "/$" + direction, "arguments": g.arguments(f, owner, owner.Parameters(), base.With, scope, nil)}
			}
			extends = append(extends, ref)
		}
		for _, method := range side.Methods {
			methods[method.Name] = declarationObject{"request": g.expression(f, method.Request, scope, nil), "result": g.expression(f, method.Result, scope, nil), "errors": sortedSet(method.Errors)}
		}
		for _, event := range side.Events {
			events[event.Name] = g.expression(f, event.Type, scope, nil)
		}
		// CRUD is a declaration form even before the kernel expands it.
		if len(side.CRUD) > 0 {
			crud := declarationObject{}
			for entity, operations := range side.CRUD {
				crud[entity] = declarationObject{"type": g.named(f, entity), "operations": sortedSet(operations)}
			}
			node["crud"] = crud
		}
	}
	node["extends"], node["methods"], node["events"] = extends, methods, events
	return declarationRef(path)
}

func (g *declarationGraph) named(f *analysis.Family, name string) any {
	if f == nil {
		return declarationObject{"unresolved": name}
	}
	t := f.Types[name]
	if t == nil {
		return declarationObject{"unresolved": f.Name + "/" + name}
	}
	if f.IsCarried(name) {
		// These are abstract projections of the family's contract. A JSON
		// envelope layout or channel-handle representation is not identity.
		return declarationObject{"projection": name, "family": g.family(f)}
	}
	path := f.Name + "/" + name
	if t.Kind == model.KindAlias && len(t.Parameters) == 0 && !g.aliases[path] {
		g.aliases[path] = true
		defer delete(g.aliases, path)
		return g.expression(f, t.Alias, typeScope(f, t), nil)
	}
	if _, seen := g.definitions[path]; !seen {
		g.definitions[path] = declarationObject{} // recursion closes here
		g.definitions[path] = g.typeNode(f, t, typeScope(f, t), nil)
	}
	return declarationRef(path)
}

func (g *declarationGraph) typeNode(f *analysis.Family, t *model.Type, scope declarationScope, bindings map[string]any) any {
	node := declarationObject{"kind": t.Kind, "parameters": parameters(t.Parameters)}
	// Family parameters remain scoped nodes, never captured by a same-named
	// parameter belonging to another declaration.
	if captured := capturedParameters(f, t); len(captured) > 0 {
		node["captures"] = parameters(captured)
	}
	extends := []any{}
	for _, base := range t.Extends {
		extends = append(extends, g.expression(f, base.Expression(), scope, bindings))
	}
	if len(extends) > 0 {
		node["extends"] = extends
	}
	switch t.Kind {
	case model.KindRecord, model.KindEntity:
		node["open"] = t.Open
		fields := []any{}
		for _, field := range t.Fields {
			fn := declarationObject{"name": field.Name, "type": g.expression(f, field.Type, scope, bindings), "required": field.Required, "nullable": field.Nullable, "unique": field.Unique}
			if field.Min != nil {
				fn["min"] = canonicalDecimal(*field.Min)
			}
			if field.Max != nil {
				fn["max"] = canonicalDecimal(*field.Max)
			}
			if field.Length != nil {
				length := declarationObject{}
				if field.Length.Min != nil {
					length["min"] = strconv.Itoa(*field.Length.Min)
				}
				if field.Length.Max != nil {
					length["max"] = strconv.Itoa(*field.Length.Max)
				}
				fn["length"] = length
			}
			if field.Pattern != "" {
				fn["pattern"] = field.Pattern
			}
			fields = append(fields, fn)
		}
		node["fields"] = fields
		if t.Kind == model.KindEntity {
			node["key"] = t.Key
		}
	case model.KindEnum:
		node["values"] = sortedSet(t.Values)
	case model.KindUnion:
		node["tag"], node["value"] = t.Tag, t.ValueMember()
		variants := declarationObject{}
		for _, variant := range t.Variants {
			variants[variant.Tag] = g.expression(f, variant.Type, scope, bindings)
		}
		node["variants"] = variants
	case model.KindAlias:
		node["type"] = g.expression(f, t.Alias, scope, bindings)
	case model.KindCallable:
		node["request"] = g.expression(f, t.Request, scope, bindings)
		node["result"] = g.expression(f, t.Result, scope, bindings)
		node["errors"] = sortedSet(t.Errors)
	}
	return node
}

func capturedParameters(f *analysis.Family, t *model.Type) []model.Parameter {
	used := map[string]bool{}
	for _, use := range f.Generics().Types[t.Name] {
		used[use.Parameter] = true
	}
	// Callable captures are part of the declaration even on a generator
	// target which does not yet render parameterized callables.
	for _, expr := range []model.TypeExpr{t.Request, t.Result} {
		model.Walk(expr, func(e model.TypeExpr) bool {
			switch x := e.(type) {
			case model.Named:
				used[x.Name] = true
			case model.Drawn:
				used[x.Parameter] = true
			}
			return true
		})
	}
	for _, p := range t.Parameters {
		delete(used, p.Name)
	}
	out := []model.Parameter{}
	for _, p := range f.Parameters() {
		if used[p.Name] {
			out = append(out, p)
		}
	}
	return out
}

func (g *declarationGraph) expression(f *analysis.Family, e model.TypeExpr, scope declarationScope, bindings map[string]any) any {
	switch x := e.(type) {
	case nil:
		return emptyExpression()
	case model.Primitive:
		return declarationObject{"primitive": string(x)}
	case model.Named:
		if bound, ok := bindings[x.Name]; ok {
			return bound
		}
		if path := scope[x.Name]; path != "" {
			return parameterRef(path)
		}
		return g.named(f, x.Name)
	case model.Imported:
		return g.named(f.Imported[x.Family], x.Name)
	case model.Drawn:
		parameter := parameterRef(scope[x.Parameter])
		if bound, ok := bindings[x.Parameter]; ok {
			parameter = bound
		}
		return declarationObject{"draw": parameter, "name": x.Name}
	case model.Array:
		return declarationObject{"array": g.expression(f, x.Elem, scope, bindings)}
	case model.Map:
		return declarationObject{"map": g.expression(f, x.Elem, scope, bindings)}
	case model.Nullable:
		return declarationObject{"nullable": g.expression(f, x.Elem, scope, bindings)}
	case model.Literal:
		return declarationObject{"literal": x.Value}
	case model.Ref:
		owner := f
		if x.Family != "" && x.Family != f.Name {
			owner = f.Imported[x.Family]
		}
		return declarationObject{"entity": g.named(owner, x.Entity)}
	case model.Inline:
		return g.typeNode(f, x.Type, scope, bindings)
	case model.Apply:
		owner := f
		if x.Family != "" {
			owner = f.Imported[x.Family]
		}
		if owner == nil || owner.Types[x.Name] == nil {
			return declarationObject{"unresolved": x.Family + "/" + x.Name}
		}
		t := owner.Types[x.Name]
		params := append(capturedParameters(owner, t), t.Parameters...)
		args := g.arguments(f, owner, params, x.With, scope, bindings)
		path := owner.Name + "/" + x.Name
		if t.Kind == model.KindAlias && !g.aliases[path] {
			g.aliases[path] = true
			defer delete(g.aliases, path)
			bound := map[string]any{}
			for i, p := range params {
				bound[p.Name] = args[i]
			}
			return g.expression(owner, t.Alias, typeScope(owner, t), bound)
		}
		g.named(owner, x.Name)
		return declarationObject{"apply": path, "arguments": args}
	}
	panic("unhandled declaration expression")
}

func (g *declarationGraph) arguments(caller, owner *analysis.Family, params []model.Parameter, with map[string]model.Filler, scope declarationScope, bindings map[string]any) []any {
	args := []any{}
	for _, p := range params {
		filler, supplied := with[p.Name]
		if !supplied {
			if bound, ok := bindings[p.Name]; ok {
				args = append(args, bound)
			} else if path := scope[p.Name]; path != "" {
				args = append(args, parameterRef(path))
			} else {
				args = append(args, parameterRef(owner.Name+"/"+p.Name))
			}
		} else if filler.Family != "" {
			family := caller.Imported[filler.Family]
			if caller.Name == filler.Family {
				family = caller
			}
			args = append(args, g.family(family))
		} else {
			args = append(args, g.expression(caller, filler.Type, scope, bindings))
		}
	}
	return args
}

// canonicalDecimal retains all digits, including integers outside the
// exactly representable range of IEEE-754. Equivalent decimal spellings
// become a signed coefficient without trailing zeroes and an exponent.
func canonicalDecimal(number json.Number) string {
	s := string(number)
	negative := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	mantissa, exponent, hasExponent := strings.Cut(strings.ToLower(s), "e")
	power := new(big.Int)
	if hasExponent {
		if _, ok := power.SetString(exponent, 10); !ok {
			panic("invalid decimal exponent")
		}
	}
	whole, fraction, _ := strings.Cut(mantissa, ".")
	digits := strings.TrimLeft(whole+fraction, "0")
	if digits == "" {
		return "0"
	}
	power.Sub(power, big.NewInt(int64(len(fraction))))
	trimmed := strings.TrimRight(digits, "0")
	power.Add(power, big.NewInt(int64(len(digits)-len(trimmed))))
	if negative {
		trimmed = "-" + trimmed
	}
	if power.Sign() != 0 {
		trimmed += "e" + power.String()
	}
	return trimmed
}
