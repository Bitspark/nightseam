package runtime

import "testing"

func TestDrawnTypeConstraints(t *testing.T) {
	schema := MustSchema(`{"parameters":[{"name":"S","of":"live"}],"types":{"Job":{"kind":"record","fields":[]},"Choice":{"kind":"enum","values":["one"]},"Alias":{"kind":"alias","type":"Job"},"Call":{"kind":"callable","request":"integer"},"Generic":{"kind":"record","parameters":[{"name":"T"}],"fields":[]},"Captured":{"kind":"record","fields":[{"name":"job","type":"S.Job"}]}}}`, "", nil)
	for _, test := range []struct {
		name          string
		object, valid bool
	}{
		{"Job", false, true}, {"Job", true, true}, {"Choice", false, true}, {"Choice", true, false},
		{"Alias", false, false}, {"Call", false, false}, {"Generic", false, false}, {"Captured", false, false}, {"Missing", false, false},
	} {
		err := ValidateDrawnType(TypeBinding{Schema: schema, Type: test.name}, test.name, test.object)
		if (err == nil) != test.valid {
			t.Errorf("%s object=%v: %v", test.name, test.object, err)
		}
	}
	if err := ValidateDrawnType(TypeBinding{Schema: schema, Type: "Job"}, "Alias", false); err == nil {
		t.Fatal("a normalized recipe hid the provider's alias member")
	}
}
