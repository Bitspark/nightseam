package diag

import "testing"

// TestLocationsAndLines: a sub-location escapes its segments, a diagnostic
// prints as one line naming family, file, pointer, message and code, and a
// set sorts by where it is.
func TestLocationsAndLines(t *testing.T) {
	at := Location{File: "model.json"}.Sub("types", "a/b", "fields", 2)
	if at.String() != "model.json#/types/a~1b/fields/2" {
		t.Fatalf("location is %s", at)
	}
	d := New("probe", at, "unresolved_type", "Type X is not declared.")
	if d.String() != "probe/model.json#/types/a~1b/fields/2: Type X is not declared. [unresolved_type]" {
		t.Fatalf("line is %s", d)
	}
	var l List
	l.Family = "probe"
	l.Add(Location{File: "session.json", Pointer: "/decides/0"}, "b", "second")
	l.Addf(Location{File: "model.json", Pointer: "/types/A"}, "a", "%s", "first")
	l.Add(Location{File: "model.json", Pointer: "/types/A"}, "a", "also first")
	Sort(l.Diagnostics)
	if l.Diagnostics[0].Message != "also first" || l.Diagnostics[1].Message != "first" || l.Diagnostics[2].File != "session.json" {
		t.Fatalf("sorted as %v", l.Diagnostics)
	}
}
