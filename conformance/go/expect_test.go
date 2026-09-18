package conformance

import (
	"strings"
	"testing"
)

func value(t *testing.T, text string) any {
	t.Helper()
	v, err := decode([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// TestExpectationsHold: every placeholder and matcher of the expectation
// language holds what it should and refuses what it should, and a
// binding made by one match serves the next.
func TestExpectationsHold(t *testing.T) {
	cases := []struct {
		name, expected, actual string
		holds                  bool
	}{
		{"literal object", `{"a": 1, "b": "x"}`, `{"a": 1, "b": "x", "c": true}`, true},
		{"a number however spelled", `{"n": 1000}`, `{"n": 1e3}`, true},
		{"a differing member", `{"a": 1}`, `{"a": 2}`, false},
		{"a missing member", `{"a": 1}`, `{}`, false},
		{"$absent", `{"a": "$absent"}`, `{"b": 1}`, true},
		{"$absent refused", `{"a": "$absent"}`, `{"a": null}`, false},
		{"$any", `{"a": "$any"}`, `{"a": [1, 2]}`, true},
		{"$string", `{"a": "$string"}`, `{"a": "s"}`, true},
		{"$string refused", `{"a": "$string"}`, `{"a": 1}`, false},
		{"$int", `{"a": "$int"}`, `{"a": 7}`, true},
		{"$int refused", `{"a": "$int"}`, `{"a": 7.5}`, false},
		{"$number", `{"a": "$number"}`, `{"a": 7.5}`, true},
		{"$bool", `{"a": "$bool"}`, `{"a": false}`, true},
		{"$regex", `{"a": "$regex:^c:[0-9]+$"}`, `{"a": "c:12"}`, true},
		{"$regex refused", `{"a": "$regex:^c:[0-9]+$"}`, `{"a": "s:12"}`, false},
		{"array exact", `[1, "two", null]`, `[1, "two", null]`, true},
		{"array length", `[1]`, `[1, 2]`, false},
		{"$contains in order", `{"$contains": [{"k": "a"}, {"k": "c"}]}`, `[{"k": "a"}, {"k": "b"}, {"k": "c"}]`, true},
		{"$contains out of order", `{"$contains": [{"k": "c"}, {"k": "a"}]}`, `[{"k": "a"}, {"k": "b"}, {"k": "c"}]`, false},
		{"$sequence_by lanes", `{"$contains": [{"id": "1", "s": 1}, {"id": "2", "s": 1}, {"id": "1", "s": 2}], "$sequence_by": "id"}`, `[{"id": "2", "s": 1}, {"id": "1", "s": 1}, {"id": "1", "s": 2}]`, true},
		{"$sequence_by holds each lane", `{"$contains": [{"id": "1", "s": 2}, {"id": "1", "s": 1}], "$sequence_by": "id"}`, `[{"id": "1", "s": 1}, {"id": "1", "s": 2}]`, false},
		{"null", `{"a": null}`, `{"a": null}`, true},
		{"null refused", `{"a": null}`, `{"a": 0}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := match(value(t, c.expected), value(t, c.actual), Bindings{})
			if (err == nil) != c.holds {
				t.Fatalf("expected %s against %s: holds=%v, err=%v", c.expected, c.actual, c.holds, err)
			}
		})
	}
}

func TestBindingsCarryAcrossMatches(t *testing.T) {
	b := Bindings{}
	if err := match(value(t, `{"id": "$bind:first", "trace": {"trace_id": "$bind:t"}}`), value(t, `{"id": "c:1", "trace": {"trace_id": "abc"}}`), b); err != nil {
		t.Fatal(err)
	}
	if err := match(value(t, `{"id": "$first", "parent": "$t"}`), value(t, `{"id": "c:1", "parent": "abc"}`), b); err != nil {
		t.Fatal(err)
	}
	if err := match(value(t, `{"id": "$first"}`), value(t, `{"id": "c:2"}`), b); err == nil {
		t.Fatal("a reference matched something else than what was bound")
	}
	if err := match(value(t, `{"id": "$not:first"}`), value(t, `{"id": "c:2"}`), b); err != nil {
		t.Fatal(err)
	}
	if err := match(value(t, `{"id": "$not:first"}`), value(t, `{"id": "c:1"}`), b); err == nil {
		t.Fatal("$not matched what was bound")
	}
	if err := match(value(t, `{"id": "$nobody"}`), value(t, `{"id": "c:1"}`), b); err == nil || !strings.Contains(err.Error(), "nothing bound") {
		t.Fatalf("an unbound reference was not refused: %v", err)
	}
}

func TestSubstitutionFillsArguments(t *testing.T) {
	b := Bindings{"url": "ws://x", "row": map[string]any{"frame": `{"a":1}`, "n": value(t, `1e3`)}}
	out, err := substitute(value(t, `{"url": "$url", "text": "$row.frame", "n": "$row.n", "literal": "$$dollar", "nested": {"on": "$url"}}`), b)
	if err != nil {
		t.Fatal(err)
	}
	if got := render(out); got != `{"literal":"$dollar","n":1e3,"nested":{"on":"ws://x"},"text":"{\"a\":1}","url":"ws://x"}` {
		t.Fatalf("substituted into %s", got)
	}
	if _, err := substitute(value(t, `{"on": "$nothing"}`), b); err == nil {
		t.Fatal("an unbound reference in arguments was not refused")
	}
}

func TestErrorsNameWhereTheyPart(t *testing.T) {
	err := match(value(t, `{"events": [{"type": "a"}, {"type": "b", "n": 1}]}`), value(t, `{"events": [{"type": "a"}, {"type": "b", "n": 2}]}`), Bindings{})
	if err == nil || !strings.HasPrefix(err.Error(), "the answer.events[1].n: expected 1, got 2") {
		t.Fatalf("the error does not name the member: %v", err)
	}
}
