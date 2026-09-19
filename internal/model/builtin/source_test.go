package builtin

import "testing"

func TestBuiltinSourceIsDistinctFromACheckoutPath(t *testing.T) {
	for _, name := range Names() {
		f, _ := Family(name)
		if f.Source != Prefix+name {
			t.Errorf("%s source = %q", name, f.Source)
		}
	}
}
