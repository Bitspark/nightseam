package runtime

import (
	"encoding/json"
	"fmt"
	"testing"
)

type codecArgument string

func (v *codecArgument) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	if text != "fixed" {
		return fmt.Errorf("expected fixed")
	}
	*v = codecArgument(text)
	return nil
}

var argumentTestSchema = MustSchema(`{"types":{
 "Box":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"item","type":"T","required":true}]},
 "Bound":{"kind":"record","fields":[{"name":"item","type":"string","required":true}]},
 "Tag":{"kind":"alias","type":{"literal":"fixed"}}
}}`, "", nil)

type declaredArgument string

var foreignArgumentSchema = MustSchema(`{"types":{"Tag":{"kind":"alias","type":{"literal":"foreign"}}}}`, "", nil)

type foreignArgument string

func (foreignArgument) WireType() TypeBinding {
	return TypeBinding{Schema: foreignArgumentSchema, Type: "Tag"}
}

var treeArgumentSchema = MustSchema(`{"types":{"Tree":{"kind":"record","parameters":[{"name":"T"}],"fields":[
 {"name":"value","type":"T","required":true},
 {"name":"children","type":{"array":{"apply":"Tree","with":{"T":"T"}}},"required":true}
]}}}`, "", nil)

type treeArgument[T any] struct {
	Value    T
	Children []treeArgument[T]
}

func (treeArgument[T]) WireType() TypeBinding {
	return TypeBinding{Schema: treeArgumentSchema.Bind(map[string]any{"T": TypeArgument[T]()}, nil), Type: "Tree"}
}

func (declaredArgument) WireType() TypeBinding {
	return TypeBinding{Schema: argumentTestSchema, Type: "Tag"}
}

func checkArgument[T any](raw string) error {
	return argumentTestSchema.Bind(map[string]any{"T": TypeArgument[T]()}, nil).ValidateRaw("Box", []byte(raw))
}

func TestAutomaticTypeArguments(t *testing.T) {
	for _, raw := range []string{`{"item":"hello"}`, `{"item":null}`, `{"item":1}`, `{}`, `{"item":"hello","other":1}`} {
		generic := checkArgument[string](raw)
		bound := argumentTestSchema.ValidateRaw("Bound", []byte(raw))
		if (generic == nil) != (bound == nil) {
			t.Errorf("%s: generic %v, bound %v", raw, generic, bound)
		}
		if generic != nil && bound != nil && generic.Error() != bound.Error() {
			t.Errorf("%s: generic %v, bound %v", raw, generic, bound)
		}
	}
	cases := []struct {
		name  string
		check func(string) error
		raw   string
		valid bool
	}{
		{"map", checkArgument[map[string]string], `{"item":{"a":"x"}}`, true},
		{"nil map", checkArgument[map[string]string], `{"item":null}`, false},
		{"map value", checkArgument[map[string]string], `{"item":{"a":null}}`, false},
		{"nullable map value", checkArgument[map[string]*string], `{"item":{"a":null}}`, true},
		{"array", checkArgument[[]string], `{"item":["x"]}`, true},
		{"nil array", checkArgument[[]string], `{"item":null}`, false},
		{"array element", checkArgument[[]string], `{"item":[null]}`, false},
		{"nullable array element", checkArgument[[]*string], `{"item":[null,"x"]}`, true},
		{"nullable", checkArgument[Nullable[string]], `{"item":null}`, true},
		{"nullable value", checkArgument[Nullable[string]], `{"item":"x"}`, true},
		{"nullable refusal", checkArgument[Nullable[string]], `{"item":1}`, false},
		{"named literal", checkArgument[declaredArgument], `{"item":"fixed"}`, true},
		{"named literal refusal", checkArgument[declaredArgument], `{"item":"wrong"}`, false},
		{"foreign literal", checkArgument[foreignArgument], `{"item":"foreign"}`, true},
		{"foreign ownership", checkArgument[foreignArgument], `{"item":"fixed"}`, false},
		{"recursive generic", checkArgument[treeArgument[declaredArgument]], `{"item":{"value":"fixed","children":[{"value":"fixed","children":[]}]}}`, true},
		{"recursive generic refusal", checkArgument[treeArgument[declaredArgument]], `{"item":{"value":"fixed","children":[{"value":"wrong","children":[]}]}}`, false},
		{"codec literal", checkArgument[codecArgument], `{"item":"fixed"}`, true},
		{"codec literal refusal", checkArgument[codecArgument], `{"item":"wrong"}`, false},
		{"nested named literal", checkArgument[map[string][]*declaredArgument], `{"item":{"a":[null,"fixed"]}}`, true},
		{"nested named refusal", checkArgument[map[string][]*declaredArgument], `{"item":{"a":["wrong"]}}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.check(c.raw); (err == nil) != c.valid {
				t.Fatalf("valid=%v: %v", c.valid, err)
			}
		})
	}
}

func TestDrawnTypeArgumentsForwardAsFamilyBindings(t *testing.T) {
	other := MustSchema(`{"types":{"Frame":{"kind":"record","parameters":[{"name":"S","of":"protocol"}],"fields":[{"name":"item","type":"S.Value","required":true}]}}}`, "", nil)
	outer := MustSchema(`{"parameters":[{"name":"Root","of":"protocol"}],"types":{
 "Forward":{"kind":"alias","parameters":[{"name":"Family","of":"protocol"}],"type":{"apply":"other.Frame","with":{"S":"Family"}}},
 "Implicit":{"kind":"alias","type":"other.Frame"}
}}`, "", map[string]*Schema{"other": other})
	for _, c := range []struct{ name, draw string }{{"Forward", "Family.Value"}, {"Implicit", "Root.Value"}} {
		bound := outer.Bind(map[string]any{c.draw: TypeArgument[string]()}, nil)
		if err := bound.ValidateRaw(c.name, []byte(`{"item":"hello"}`)); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		if err := bound.ValidateRaw(c.name, []byte(`{"item":1}`)); err == nil {
			t.Errorf("%s accepted wrong type", c.name)
		}
	}
}

func TestChainedDrawnTypeBindingsRetainEveryMember(t *testing.T) {
	inner := MustSchema(`{"types":{"Pair":{"kind":"record","parameters":[{"name":"S","of":"protocol"}],"fields":[
 {"name":"first","type":"S.First","required":true},{"name":"second","type":"S.Second","required":true}
]}}}`, "", nil)
	outer := MustSchema(`{"types":{"Forward":{"kind":"alias","parameters":[{"name":"Family","of":"protocol"}],"type":{"apply":"inner.Pair","with":{"S":"Family"}}}}}`, "", map[string]*Schema{"inner": inner})
	first := outer.Bind(map[string]any{"Family.First": TypeArgument[foreignArgument]()}, nil)
	bound := first.Bind(map[string]any{"Family.Second": TypeArgument[string]()}, nil)
	if err := bound.ValidateRaw("Forward", []byte(`{"first":"foreign","second":"hello"}`)); err != nil {
		t.Fatal(err)
	}
	if err := bound.ValidateRaw("Forward", []byte(`{"first":"wrong","second":"hello"}`)); err == nil {
		t.Fatal("chained binding lost the first member's literal constraint")
	}
	if err := first.ValidateExpressionRaw("Family.Second", []byte(`"hello"`)); err == nil {
		t.Fatal("binding mutated its receiver")
	}
}
