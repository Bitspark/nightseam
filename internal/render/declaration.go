package render

import (
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/model"
)

// CanonicalDeclaration is the language- and backend-independent graph of
// the family's declaration. Its exact bytes are specified in
// docs/declaration/declaration-identity.md. It is not the validator schema.
func CanonicalDeclaration(f *analysis.Family) string {
	g := newDeclarationGraph()
	g.computeCaptures(f)
	return g.bytes(g.family(f))
}

// CanonicalExpression selects a type or application and only its reachable
// declaration content, using the same graph and encoding as a family.
func CanonicalExpression(f *analysis.Family, e model.TypeExpr) string {
	g := newDeclarationGraph()
	g.computeCaptures(f)
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
	captureUses map[declarationType]map[string]bool
	families    map[string]*analysis.Family
}

type declarationType struct {
	family      *analysis.Family
	declaration *model.Type
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

func (g *declarationGraph) document(root any, scopeRoot bool) declarationObject {
	if node, ok := root.(map[string]any); ok && scopeRoot {
		if graph, ok := node["graph"].(map[string]any); ok {
			return graph
		}
		clone := declarationObject{}
		for key, value := range node {
			clone[key] = value
		}
		wrap := func(value any) any { return declarationObject{"graph": g.document(value, true)} }
		for _, key := range []string{"array", "map", "nullable", "entity", "draw", "family", "type", "request", "result"} {
			if value, ok := node[key]; ok {
				clone[key] = wrap(value)
			}
		}
		for _, key := range []string{"arguments", "extends"} {
			if values, ok := node[key].([]any); ok {
				scoped := []any{}
				for _, value := range values {
					scoped = append(scoped, wrap(value))
				}
				clone[key] = scoped
			}
		}
		if fields, ok := node["fields"].([]any); ok {
			scoped := []any{}
			for _, field := range fields {
				original := field.(map[string]any)
				copy := declarationObject{}
				for key, value := range original {
					copy[key] = value
				}
				copy["type"] = wrap(original["type"])
				scoped = append(scoped, copy)
			}
			clone["fields"] = scoped
		}
		if variants, ok := node["variants"].(map[string]any); ok {
			scoped := declarationObject{}
			for name, value := range variants {
				scoped[name] = wrap(value)
			}
			clone["variants"] = scoped
		}
		root = clone
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
	if g.families == nil {
		g.families = map[string]*analysis.Family{}
	}
	g.families[f.Name] = f
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
	if captured := g.capturedParameters(f, t); len(captured) > 0 {
		free := []model.Parameter{}
		for _, p := range captured {
			if _, bound := bindings[p.Name]; !bound {
				free = append(free, p)
			}
		}
		if len(free) > 0 {
			node["captures"] = parameters(free)
		}
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

func (g *declarationGraph) capturedParameters(f *analysis.Family, t *model.Type) []model.Parameter {
	if g.captureUses == nil {
		g.computeCaptures(f)
	}
	used := g.captureUses[declarationType{f, t}]
	if used == nil {
		used = g.typeCaptures(f, t)
	} // inline declaration
	out := []model.Parameter{}
	for _, p := range f.Parameters() {
		_, shadowed := t.Parameter(p.Name)
		if used[p.Name] && !shadowed {
			out = append(out, p)
		}
	}
	return out
}

// Captures are a declaration fact, including callable signatures. The
// monotone finite fixed point propagates free family parameters through
// recursive local/imported types without expanding any application.
func (g *declarationGraph) computeCaptures(root *analysis.Family) {
	g.captureUses = map[declarationType]map[string]bool{}
	if g.families == nil {
		g.families = map[string]*analysis.Family{}
	}
	visited := map[*analysis.Family]bool{}
	var collect func(*analysis.Family)
	collect = func(f *analysis.Family) {
		if f == nil || visited[f] {
			return
		}
		visited[f] = true
		g.families[f.Name] = f
		for _, t := range f.Types {
			g.captureUses[declarationType{f, t}] = map[string]bool{}
		}
		for _, other := range f.Imported {
			collect(other)
		}
	}
	collect(root)
	for changed := true; changed; {
		changed = false
		for key, used := range g.captureUses {
			for name := range g.typeCaptures(key.family, key.declaration) {
				if !used[name] {
					used[name] = true
					changed = true
				}
			}
		}
	}
}

func (g *declarationGraph) typeCaptures(f *analysis.Family, t *model.Type) map[string]bool {
	used := map[string]bool{}
	shadow := map[string]bool{}
	own := map[string]bool{}
	for _, p := range t.Parameters {
		own[p.Name] = true
	}
	var expression func(model.TypeExpr, map[string]bool)
	var application func(*analysis.Family, *model.Type, map[string]model.Filler, map[string]bool)
	add := func(name string, shadow map[string]bool) {
		if !shadow[name] && (f.HasParameter(name) || own[name]) {
			used[name] = true
		}
	}
	application = func(owner *analysis.Family, target *model.Type, with map[string]model.Filler, shadow map[string]bool) {
		if owner == nil || target == nil {
			return
		}
		params := []model.Parameter{}
		for _, p := range owner.Parameters() {
			_, shadowed := target.Parameter(p.Name)
			if !shadowed && g.captureUses[declarationType{owner, target}][p.Name] {
				params = append(params, p)
			}
		}
		for _, p := range target.Parameters {
			if target.Kind != model.KindAlias || g.captureUses[declarationType{owner, target}][p.Name] {
				params = append(params, p)
			}
		}
		for _, p := range params {
			filler, ok := with[p.Name]
			if ok {
				if filler.Type != nil {
					expression(filler.Type, shadow)
				}
				continue
			}
			if owner == f {
				add(p.Name, shadow)
			} else if p.IsFamily() && len(f.FamilyParameters()) == 1 {
				add(f.FamilyParameters()[0].Name, shadow)
			}
		}
	}
	expression = func(e model.TypeExpr, shadow map[string]bool) {
		switch x := e.(type) {
		case model.Named:
			if shadow[x.Name] {
				return
			}
			if f.HasParameter(x.Name) || own[x.Name] {
				add(x.Name, shadow)
			} else if target := f.Types[x.Name]; target != nil {
				application(f, target, nil, shadow)
			}
		case model.Imported:
			if owner := f.Imported[x.Family]; owner != nil {
				application(owner, owner.Types[x.Name], nil, shadow)
			}
		case model.Drawn:
			add(x.Parameter, shadow)
		case model.Array:
			expression(x.Elem, shadow)
		case model.Map:
			expression(x.Elem, shadow)
		case model.Nullable:
			expression(x.Elem, shadow)
		case model.Apply:
			owner := f
			if x.Family != "" {
				owner = f.Imported[x.Family]
			}
			if owner != nil {
				application(owner, owner.Types[x.Name], x.With, shadow)
			}
		case model.Ref:
			owner := f
			if x.Family != "" && x.Family != f.Name {
				owner = f.Imported[x.Family]
			}
			if owner != nil {
				application(owner, owner.Types[x.Entity], nil, shadow)
			}
		case model.Inline:
			inner := map[string]bool{}
			for name := range shadow {
				inner[name] = true
			}
			for _, p := range x.Type.Parameters {
				inner[p.Name] = true
			}
			x.Type.WalkExpressions(func(child model.TypeExpr, _ diag.Location) { expression(child, inner) })
		}
	}
	t.WalkExpressions(func(e model.TypeExpr, _ diag.Location) { expression(e, shadow) })
	return used
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
		if target := f.Types[x.Name]; target != nil && len(bindings) > 0 {
			params := append(g.capturedParameters(f, target), target.Parameters...)
			for _, p := range params {
				if _, bound := bindings[p.Name]; bound {
					return g.application(f, target, params, g.arguments(f, f, params, nil, scope, bindings))
				}
			}
		}
		return g.named(f, x.Name)
	case model.Imported:
		owner := f.Imported[x.Family]
		if owner != nil && owner.Types[x.Name] != nil && len(g.capturedParameters(owner, owner.Types[x.Name])) > 0 {
			return g.expression(f, model.Apply{Family: x.Family, Name: x.Name}, scope, bindings)
		}
		return g.named(owner, x.Name)
	case model.Drawn:
		parameter := parameterRef(scope[x.Parameter])
		if bound, ok := bindings[x.Parameter]; ok {
			parameter = bound
			if node, ok := bound.(map[string]any); ok {
				if path, ok := node["ref"].(string); ok && g.families[path] != nil {
					return g.named(g.families[path], x.Name)
				}
			}
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
		params := append(g.capturedParameters(owner, t), t.Parameters...)
		args := g.arguments(f, owner, params, x.With, scope, bindings)
		return g.application(owner, t, params, args)
	}
	panic("unhandled declaration expression")
}

func (g *declarationGraph) application(owner *analysis.Family, t *model.Type, params []model.Parameter, args []any) any {
	path := owner.Name + "/" + t.Name
	if t.Kind == model.KindAlias && !g.aliases[path] {
		g.aliases[path] = true
		defer delete(g.aliases, path)
		bound := map[string]any{}
		for i, p := range params {
			bound[p.Name] = args[i]
		}
		return g.expression(owner, t.Alias, typeScope(owner, t), bound)
	}
	g.named(owner, t.Name)
	return declarationObject{"apply": path, "arguments": args}
}

func (g *declarationGraph) arguments(caller, owner *analysis.Family, params []model.Parameter, with map[string]model.Filler, scope declarationScope, bindings map[string]any) []any {
	args := []any{}
	for _, p := range params {
		filler, supplied := with[p.Name]
		if !supplied {
			// A bare imported generic type has the language's one implicit
			// family-parameter binding when the caller has exactly one.
			name := p.Name
			if caller != owner && p.IsFamily() && len(caller.FamilyParameters()) == 1 {
				name = caller.FamilyParameters()[0].Name
			}
			if bound, ok := bindings[name]; ok {
				args = append(args, bound)
			} else if path := scope[name]; path != "" {
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
