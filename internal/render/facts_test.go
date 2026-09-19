package render

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/load"
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/model/modeltest"
)

func proof(t *testing.T) *analysis.Family {
	t.Helper()
	world, diagnostics := load.Checkout(os.DirFS("../../cmd/nightseam/testdata/families"), "api/contracts", []string{"go", "typescript"})
	if len(diagnostics) != 0 {
		t.Fatalf("load proof: %v", diagnostics)
	}
	return analysis.Resolve(analysis.World(world.Families), "proof")
}

func TestInlineDeclarationsAreRenderingTypes(t *testing.T) {
	r := Build(proof(t))
	if r.Source != "api/contracts/proof" {
		t.Fatalf("source directory = %q", r.Source)
	}
	for _, name := range []string{"OptionNone", "PartImage", "RichPartTable", "PartsRequest"} {
		if r.Type(name) == nil {
			t.Errorf("inline declaration %s has no rendering type", name)
		}
	}
}

func TestUnionIncludesItsInheritedVariants(t *testing.T) {
	r := Build(proof(t))
	var tags []string
	for _, variant := range r.Type("RichPart").Variants {
		tags = append(tags, variant.Tag)
	}
	if want := []string{"count", "image", "text", "table"}; !reflect.DeepEqual(tags, want) {
		t.Fatalf("RichPart variants = %v, want inherited variants then own: %v", tags, want)
	}
}

func TestSideIncludesItsBaseOperationsAndErrors(t *testing.T) {
	r := Build(proof(t))
	var methods []string
	for _, method := range r.Server.Methods {
		methods = append(methods, method.Name)
		if method.Name == "echo" && model.String(method.Request) != `"probe.Payload"` {
			t.Errorf("inherited request = %s, want its declaring family's type", model.String(method.Request))
		}
	}
	if want := []string{"echo", "no_args", "classify", "classify_rich", "parts", "relay"}; !reflect.DeepEqual(methods, want) {
		t.Errorf("methods = %v, want %v", methods, want)
	}
	if len(r.Errors) != 1 || r.Errors[0].Code != "denied" {
		t.Errorf("inherited errors = %v", r.Errors)
	}
}

func TestTypeApplicationsExposeEveryArgument(t *testing.T) {
	r := Build(proof(t))
	args := r.Arguments(model.Apply{Name: "Page", With: map[string]model.Filler{
		"T": {Type: model.Array{Elem: model.Nullable{Elem: model.Primitive("string")}}},
	}})
	if len(args) != 1 || args[0].Use.Parameter != "T" {
		t.Fatalf("Page's type argument disappeared: %+v", args)
	}
}

func TestFamilyAndTypeParameterUsesStayInTheirScopes(t *testing.T) {
	r := Build(proof(t))
	if want := []Use{{Parameter: "S", Type: "Envelope"}, {Parameter: "S", Type: "Handle"}, {Parameter: "Item"}}; !reflect.DeepEqual(r.Uses, want) {
		t.Errorf("family uses = %v, want %v", r.Uses, want)
	}
	if want := []Use{{Parameter: "T"}}; !reflect.DeepEqual(r.Type("Page").Uses, want) {
		t.Errorf("Page uses = %v, want %v", r.Type("Page").Uses, want)
	}
}

func TestBuildingFactsDoesNotRewriteTheDeclaration(t *testing.T) {
	f := proof(t)
	before, err := json.Marshal(f.Family)
	if err != nil {
		t.Fatal(err)
	}
	first, second := Build(f), Build(f)
	after, err := json.Marshal(f.Family)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || first.Wire != second.Wire {
		t.Fatal("building render facts mutated the declaration")
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal([]byte(first.Wire), &wire); err != nil {
		t.Fatal(err)
	}
	if _, materialized := wire["PartImage"]; materialized {
		t.Fatal("the rendering name changed the original wire descriptor")
	}
}

func TestVariantPayloadFactsIncludeTheirWireForm(t *testing.T) {
	r := Build(proof(t))
	forms := map[string]VariantForm{}
	for _, variant := range r.Type("RichPart").Variants {
		forms[variant.Tag] = variant.Form
		if variant.Origin.Family != "proof" || variant.At.File != "model.json" {
			t.Fatalf("variant lost its provenance: %+v", variant)
		}
	}
	want := map[string]VariantForm{"count": VariantValue, "image": VariantValue, "text": VariantValue, "table": VariantValue}
	if !reflect.DeepEqual(forms, want) {
		t.Fatalf("forms = %v, want %v", forms, want)
	}
	if r.Type("Option").Variants[1].Form != VariantValue {
		t.Fatal("a type parameter did not keep the adjacent payload carrier")
	}
}

func TestVariantPayloadFactsKeepOneCarrierForEveryPayloadKind(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"x": {"model.json": `{"nightseam":2,"types":{
			"Payload":{"kind":"record","fields":[{"name":"text","type":"string"}]},
			"MaybePayload":{"kind":"alias","type":{"nullable":"Payload"}},
			"Part":{"kind":"union","tag":"kind","variants":{
				"map":{"map":"integer"},"record":{"nullable":"Payload"},"alias":"MaybePayload",
				"scalar":{"nullable":"integer"},"list":{"array":"Payload"},"json":"json"
			}}
		}}`},
	}))
	r := Build(analysis.Resolve(world, "x"))
	want := map[string]VariantForm{"map": VariantValue, "record": VariantValue, "alias": VariantValue, "scalar": VariantValue, "list": VariantValue, "json": VariantValue}
	for _, variant := range r.Type("Part").Variants {
		if variant.Form != want[variant.Tag] {
			t.Errorf("%s payload form = %s, want %s", variant.Tag, variant.Form, want[variant.Tag])
		}
		if (variant.Tag == "record" || variant.Tag == "alias") && (variant.Payload != nil || len(variant.Fields) != 0) {
			t.Errorf("%s mistook a nullable record for a directly reusable record", variant.Tag)
		}
	}
}

func TestExplicitApplicationsSubstituteNestedTypesAndFamilyDraws(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"payload": {"model.json": `{"nightseam":2,"types":{"Item":{"kind":"record","fields":[{"name":"text","type":"string"}]}}}`, "protocol.json": modeltest.Protocol("")},
		"boxes": {"model.json": `{"nightseam":2}`, "protocol.json": modeltest.Protocol(`"types":{
			"Box":{"kind":"record","parameters":[{"name":"T"},{"name":"S","of":"protocol"}],"fields":[{"name":"items","type":{"array":{"nullable":"T"}}},{"name":"message","type":"S.Envelope"}]},
			"Result":{"kind":"union","parameters":[{"name":"T"}],"tag":"kind","variants":{"ok":"T","error":"string"}}
		}`)},
		"consumer": {"model.json": `{"nightseam":2,"imports":["boxes","payload"]}`, "protocol.json": modeltest.Protocol("")},
	}))
	r := Build(analysis.Resolve(world, "consumer"))
	box, ok := r.Apply(model.Apply{Family: "boxes", Name: "Box", With: map[string]model.Filler{
		"T": {Type: model.Imported{Family: "payload", Name: "Item"}}, "S": {Family: "payload"},
	}}, nil)
	if !ok {
		t.Fatal("Box application did not resolve")
	}
	if got := model.String(box.Fields[0].Type); got != `{"array":{"nullable":"payload.Item"}}` {
		t.Fatalf("nested filler = %s", got)
	}
	if got := model.String(box.Fields[1].Type); got != `"payload.Envelope"` {
		t.Fatalf("family draw = %s", got)
	}
	if len(box.Uses) != 0 {
		t.Fatalf("concrete application stayed generic: %v", box.Uses)
	}
	result, ok := r.Apply(model.Apply{Family: "boxes", Name: "Result", With: map[string]model.Filler{
		"T": {Type: model.Imported{Family: "payload", Name: "Item"}},
	}}, nil)
	if !ok || result.Variants[1].Form != VariantValue || len(result.Variants[1].Fields) != 1 || result.Variants[1].Fields[0].Name != "text" {
		t.Fatalf("applied object variant = %+v", result)
	}
	if source := r.other("boxes").Type("Box"); model.String(source.Fields[0].Type) != `{"array":{"nullable":"T"}}` {
		t.Fatal("application rewrote the generic declaration")
	}
}

func TestApplicationsBindParametersCapturedThroughNamedTypes(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"boxes": {"model.json": `{"nightseam":2}`, "protocol.json": modeltest.Protocol(`"parameters":[{"name":"Item"}],"types":{
			"Payload":{"kind":"record","fields":[{"name":"value","type":"Item"}]},
			"Box":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"value","type":"T"},{"name":"payload","type":"Payload"}]},
			"Container":{"kind":"record","fields":[{"name":"payload","type":"Payload"},{"name":"box","type":{"apply":"Box","with":{"T":"integer"}}}]}
		}`)},
		"consumer": {"model.json": `{"nightseam":2,"imports":["boxes"]}`, "protocol.json": modeltest.Protocol("")},
	}))
	r := Build(analysis.Resolve(world, "consumer"))
	container, ok := r.Apply(model.Apply{Family: "boxes", Name: "Container", With: map[string]model.Filler{"Item": {Type: model.Primitive("string")}}}, nil)
	if !ok {
		t.Fatal("Container application failed")
	}
	for _, field := range container.Fields {
		application, ok := field.Type.(model.Apply)
		if !ok {
			t.Errorf("%s did not retain its captured argument: %s", field.Name, model.String(field.Type))
			continue
		}
		if got := model.String(application.With["Item"].Type); got != `"string"` {
			t.Errorf("%s's captured Item = %s", field.Name, got)
		}
		resolved, ok := r.Apply(application, nil)
		if !ok || len(resolved.Uses) != 0 {
			t.Errorf("%s stayed unbound: %+v", field.Name, resolved)
		}
	}
	if original := r.other("boxes").Type("Container"); model.String(original.Fields[0].Type) != `"Payload"` {
		t.Fatal("application mutated its source reference")
	}
}

func TestApplicationsResolveTheSourcesImplicitImportedFamilyBinding(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"payload":  {"model.json": `{"nightseam":2}`, "protocol.json": modeltest.Protocol("")},
		"foreign":  {"model.json": `{"nightseam":2}`, "protocol.json": modeltest.Protocol(`"parameters":[{"name":"F","of":"protocol"}],"types":{"Payload":{"kind":"record","fields":[{"name":"message","type":"F.Envelope"}]}}`)},
		"boxes":    {"model.json": `{"nightseam":2,"imports":["foreign"]}`, "protocol.json": modeltest.Protocol(`"parameters":[{"name":"S","of":"protocol"}],"types":{"Container":{"kind":"record","fields":[{"name":"payload","type":"foreign.Payload"}]}}`)},
		"consumer": {"model.json": `{"nightseam":2,"imports":["boxes","payload"]}`, "protocol.json": modeltest.Protocol("")},
	}))
	r := Build(analysis.Resolve(world, "consumer"))
	container, ok := r.Apply(model.Apply{Family: "boxes", Name: "Container", With: map[string]model.Filler{"S": {Family: "payload"}}}, nil)
	if !ok {
		t.Fatal("Container application failed")
	}
	application, ok := container.Fields[0].Type.(model.Apply)
	if !ok || application.Family != "foreign" || application.With["F"].Family != "payload" {
		t.Fatalf("implicit source binding was lost: %s", model.String(container.Fields[0].Type))
	}
	payload, ok := r.Apply(application, nil)
	if !ok || model.String(payload.Fields[0].Type) != `"payload.Envelope"` || len(payload.Uses) != 0 {
		t.Fatalf("nested concrete application = %+v", payload)
	}
}

func TestAppliedInlineKeepsItsNameAndResolvedCapture(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"boxes":    {"model.json": `{"nightseam":2,"types":{"Box":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"child","type":{"kind":"record","fields":[{"name":"value","type":"T"}]}}]}}}`},
		"consumer": {"model.json": `{"nightseam":2,"imports":["boxes"]}`},
	}))
	r := Build(analysis.Resolve(world, "consumer"))
	box, ok := r.Apply(model.Apply{Family: "boxes", Name: "Box", With: map[string]model.Filler{"T": {Type: model.Primitive("string")}}}, nil)
	if !ok {
		t.Fatal("application failed")
	}
	inline := r.InlineType(box.Fields[0].Type.(model.Inline))
	if inline == nil || inline.Name != "BoxChild" || inline.Origin.Family != "boxes" {
		t.Fatalf("inline identity = %+v", inline)
	}
	if got := model.String(inline.Fields[0].Type); got != `"string"` {
		t.Fatalf("captured argument = %s", got)
	}
	if len(inline.Uses) != 0 {
		t.Fatalf("bound inline has free uses: %v", inline.Uses)
	}
	if len(inline.Arguments) != 1 || model.String(inline.Arguments[0].Type) != `"string"` {
		t.Fatalf("inline type arguments = %+v", inline.Arguments)
	}
	if got := model.String(inline.Declaration.Fields[0].Type); got != `"T"` {
		t.Fatalf("original inline declaration changed: %s", got)
	}
}

func TestNestedInlineShapesRetainTheirNamesThroughApplications(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"x": {"model.json": `{"nightseam":2,"types":{"State":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"items","type":{"map":{"array":{"nullable":{"kind":"union","tag":"kind","variants":{"detail":{"kind":"record","fields":[{"name":"value","type":"T"},{"name":"mode","type":{"kind":"enum","values":["automatic","manual"]}}]}}}}}}}]}}}`},
	}))
	r := Build(analysis.Resolve(world, "x"))
	for _, name := range []string{"StateItems", "StateItemsDetail", "StateItemsDetailMode"} {
		if shape := r.Type(name); shape == nil || !shape.Inline {
			t.Fatalf("nested shape %s is missing", name)
		}
	}
	state, ok := r.Apply(model.Apply{Name: "State", With: map[string]model.Filler{"T": {Type: model.Primitive("string")}}}, nil)
	if !ok {
		t.Fatal("State application failed")
	}
	expression := state.Fields[0].Type.(model.Map).Elem.(model.Array).Elem.(model.Nullable).Elem.(model.Inline)
	items := r.InlineType(expression)
	detail := r.InlineType(items.Variants[0].Type.(model.Inline))
	mode := r.InlineType(detail.Fields[1].Type.(model.Inline))
	if items.Name != "StateItems" || detail.Name != "StateItemsDetail" || mode.Name != "StateItemsDetailMode" {
		t.Fatalf("nested application names = %s, %s, %s", items.Name, detail.Name, mode.Name)
	}
	if model.String(detail.Fields[0].Type) != `"string"` || len(items.Uses)+len(detail.Uses)+len(mode.Uses) != 0 {
		t.Fatal("nested application did not substitute its lexical capture")
	}
	if mode.Declaration != r.Type(mode.Name).Declaration || len(mode.Values) != 2 {
		t.Fatal("nested enum lost its declaration")
	}
}

func TestSharedInheritedDeclarationAppearsOnce(t *testing.T) {
	f := analysis.Resolve(analysis.World(modeltest.World(map[string]map[string]string{"x": {
		"model.json": `{"nightseam":2,"types":{
			"Base":{"kind":"union","tag":"kind","variants":{"base":"string"}},
			"Left":{"kind":"union","tag":"kind","extends":["Base"],"variants":{"left":"string"}},
			"Right":{"kind":"union","tag":"kind","extends":["Base"],"variants":{"right":"string"}},
			"Diamond":{"kind":"union","tag":"kind","extends":["Left","Right"],"variants":{"own":"string"}}
		}}`,
	}})), "x")
	r := Build(f)
	var tags []string
	for _, variant := range r.Type("Diamond").Variants {
		tags = append(tags, variant.Tag)
	}
	if want := []string{"base", "left", "right", "own"}; !reflect.DeepEqual(tags, want) {
		t.Fatalf("diamond = %v, want %v", tags, want)
	}
}

func TestInheritedSurfaceAddsItsTransitiveReferences(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"values":   {"model.json": `{"nightseam":2,"types":{"Payload":{"kind":"record","fields":[{"name":"text","type":"string"}]}}}`},
		"base":     {"model.json": `{"nightseam":2,"imports":["values"]}`, "protocol.json": modeltest.Protocol(`"server":{"methods":{"echo":{"request":"values.Payload","result":"values.Payload"}}}`)},
		"middle":   {"model.json": `{"nightseam":2,"imports":["base"]}`, "protocol.json": modeltest.Protocol(`"server":{"extends":["base"]}`)},
		"consumer": {"model.json": `{"nightseam":2,"imports":["middle"]}`, "protocol.json": modeltest.Protocol(`"server":{"extends":["middle"]}`)},
	}))
	r := Build(analysis.Resolve(world, "consumer"))
	if want := []string{"middle", "values"}; !reflect.DeepEqual(r.References, want) {
		t.Errorf("imports = %v, want %v", r.References, want)
	}
	if len(r.Server.Methods) != 1 || r.Server.Methods[0].Origin.Family != "base" {
		t.Fatalf("method provenance = %+v", r.Server.Methods)
	}
	if r.other("values") == nil {
		t.Fatal("transitive reference cannot resolve")
	}
}

func TestInheritedEntityReferencesRetainTheirDeclarations(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"base":     {"model.json": `{"nightseam":2,"types":{"Item":{"kind":"entity","key":"id","fields":[{"name":"id","type":"string"}]}}}`, "protocol.json": modeltest.Protocol(`"server":{"methods":{"select":{"result":{"ref":"Item"}}},"events":{"selected":{"type":{"ref":"Item"}}}}`)},
		"middle":   {"model.json": `{"nightseam":2,"imports":["base"]}`, "protocol.json": modeltest.Protocol(`"server":{"extends":["base"]}`)},
		"consumer": {"model.json": `{"nightseam":2,"imports":["middle"]}`, "protocol.json": modeltest.Protocol(`"server":{"extends":["middle"]}`)},
	}))
	r := Build(analysis.Resolve(world, "consumer"))
	m, e := r.Server.Methods[0], r.Server.Events[0]
	if m.Origin.Family != "base" || e.Origin.Family != "base" {
		t.Fatalf("reference owners = %v, %v", m.Origin, e.Origin)
	}
	if m.Declaration != &world["base"].Protocol.Server.Methods[0] || e.Declaration != &world["base"].Protocol.Server.Events[0] {
		t.Fatal("inherited operations lost their original declaration identity")
	}
	for _, expression := range []model.TypeExpr{m.Declaration.Result, e.Declaration.Type} {
		if reference, ok := expression.(model.Ref); !ok || reference.Entity != "Item" {
			t.Fatalf("declared reference = %s", model.String(expression))
		}
	}
	for _, expression := range []model.TypeExpr{m.Result, e.Type} {
		if model.String(expression) != `"string"` {
			t.Fatalf("resolved reference key = %s", model.String(expression))
		}
	}
}

func TestSessionGovernanceFollowsTheInheritedSides(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"base":  {"model.json": `{"nightseam":2,"types":{"State":{"kind":"record","fields":[{"name":"id","type":"string"}]}}}`, "protocol.json": modeltest.Protocol(`"server":{"methods":{"write":{"result":"string"}},"events":{"started":{"type":"State"}}},"client":{"methods":{"answer":{"result":"string"}}}`), "session.json": `{"decides":["write"],"asks":["answer"],"conversation":{"event":"started","path":"id"}}`},
		"child": {"model.json": `{"nightseam":2,"imports":["base"]}`, "protocol.json": modeltest.Protocol(`"server":{"extends":["base"]},"client":{"extends":["base"]}`), "session.json": `{}`},
		"plain": {"model.json": `{"nightseam":2,"imports":["base"]}`, "protocol.json": modeltest.Protocol(`"server":{"extends":["base"]}`)},
	}))
	r := Build(analysis.Resolve(world, "child"))
	if !reflect.DeepEqual(r.Session.Decides, []string{"write"}) || !reflect.DeepEqual(r.Session.Asks, []string{"answer"}) || r.Session.Conversation == nil || r.Session.Conversation.Event != "started" {
		t.Fatalf("inherited governance = %+v", r.Session)
	}
	if len(world["child"].Session.Decides) != 0 || world["child"].Session.Conversation != nil {
		t.Fatal("governance mutated the declaration")
	}
	if Build(analysis.Resolve(world, "plain")).Session != nil {
		t.Fatal("inheriting a side invented a session tier")
	}
}

func TestDecidingMethodsFollowEitherInheritedSide(t *testing.T) {
	world := analysis.World(modeltest.World(map[string]map[string]string{
		"base":   {"model.json": `{"nightseam":2}`, "protocol.json": modeltest.Protocol(`"server":{"methods":{"write":{"result":"string"}}},"client":{"methods":{"answer":{"result":"string"}}}`), "session.json": `{"decides":["write","answer"]}`},
		"server": {"model.json": `{"nightseam":2,"imports":["base"]}`, "protocol.json": modeltest.Protocol(`"server":{"extends":["base"]}`), "session.json": `{}`},
		"client": {"model.json": `{"nightseam":2,"imports":["base"]}`, "protocol.json": modeltest.Protocol(`"client":{"extends":["base"]}`), "session.json": `{}`},
		"both":   {"model.json": `{"nightseam":2,"imports":["base"]}`, "protocol.json": modeltest.Protocol(`"server":{"extends":["base"]},"client":{"extends":["base"]}`), "session.json": `{}`},
	}))
	for name, want := range map[string][]string{"base": {"write", "answer"}, "server": {"write"}, "client": {"answer"}, "both": {"write", "answer"}} {
		r := Build(analysis.Resolve(world, name))
		if !reflect.DeepEqual(r.Session.Decides, want) {
			t.Errorf("%s deciding methods = %v, want %v", name, r.Session.Decides, want)
		}
	}
}
