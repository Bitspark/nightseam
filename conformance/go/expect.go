package conformance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// An expectation is a JSON value a scenario writes beside a step, held
// against the answer the step got. It is compared as JSON values — a number
// is a number however it was spelled — member by member, with a few
// placeholders in string position that stand for a shape rather than a
// value, and that bind and refer to what a run minted: an id, a URL, a
// handle, a trace.
//
//	$any            anything, present
//	$string $int $number $bool  a value of that kind
//	$odd $even      an integer of that parity
//	$absent         the member must not be present at all
//	$bind:name      anything; bound to name for later steps
//	$name           whatever name was bound to
//	$not:name       anything but what name was bound to
//	$regex:pattern  a string the pattern matches
//	$row.member     a member of the table row a foreach bound
//
// An array expectation is ordered and exact, or one of two objects:
// {"$contains": [...]} holds an ordered subsequence, {"$sequence_by": "id"}
// beside it holds order per id and lets ids interleave — what an observer
// told from several goroutines can promise.

// Bindings are what a scenario has bound so far, by name.
type Bindings map[string]any

// match holds actual to expected under the bindings, binding as it goes,
// and returns where they part.
func match(expected, actual any, b Bindings) error {
	return matchAt("", expected, actual, b)
}

func matchAt(path string, expected, actual any, b Bindings) error {
	switch e := expected.(type) {
	case string:
		if strings.HasPrefix(e, "$") {
			return matchPlaceholder(path, e, actual, b)
		}
		s, ok := actual.(string)
		if !ok || s != e {
			return fmt.Errorf("%s: expected %q, got %s", at(path), e, render(actual))
		}
		return nil
	case map[string]any:
		if contains, ok := e["$contains"]; ok {
			return matchContains(path, contains, e["$sequence_by"], actual, b)
		}
		object, ok := actual.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: expected an object, got %s", at(path), render(actual))
		}
		keys := make([]string, 0, len(e))
		for key := range e {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			want := e[key]
			got, present := object[key]
			if want == "$absent" {
				if present {
					return fmt.Errorf("%s: expected no %s, got %s", at(path), key, render(got))
				}
				continue
			}
			if !present {
				return fmt.Errorf("%s: expected %s, which is absent", at(path), key)
			}
			if err := matchAt(path+"."+key, want, got, b); err != nil {
				return err
			}
		}
		return nil
	case []any:
		list, ok := actual.([]any)
		if !ok {
			return fmt.Errorf("%s: expected an array, got %s", at(path), render(actual))
		}
		if len(list) != len(e) {
			return fmt.Errorf("%s: expected %d elements, got %d: %s", at(path), len(e), len(list), render(actual))
		}
		for i := range e {
			if err := matchAt(fmt.Sprintf("%s[%d]", path, i), e[i], list[i], b); err != nil {
				return err
			}
		}
		return nil
	case nil:
		if actual != nil {
			return fmt.Errorf("%s: expected null, got %s", at(path), render(actual))
		}
		return nil
	case bool:
		v, ok := actual.(bool)
		if !ok || v != e {
			return fmt.Errorf("%s: expected %v, got %s", at(path), e, render(actual))
		}
		return nil
	case json.Number:
		v, ok := actual.(json.Number)
		if !ok || !sameNumber(e, v) {
			return fmt.Errorf("%s: expected %s, got %s", at(path), e, render(actual))
		}
		return nil
	}
	return fmt.Errorf("%s: an expectation of %T is not one the runner knows", at(path), expected)
}

func matchPlaceholder(path, placeholder string, actual any, b Bindings) error {
	name, argument, _ := strings.Cut(placeholder[1:], ":")
	switch name {
	case "any":
		return nil
	case "absent":
		return fmt.Errorf("%s: $absent stands only as an object member's expectation", at(path))
	case "string":
		if _, ok := actual.(string); !ok {
			return fmt.Errorf("%s: expected a string, got %s", at(path), render(actual))
		}
	case "int":
		n, ok := actual.(json.Number)
		if !ok {
			return fmt.Errorf("%s: expected an integer, got %s", at(path), render(actual))
		}
		if _, err := n.Int64(); err != nil {
			return fmt.Errorf("%s: expected an integer, got %s", at(path), n)
		}
	case "number":
		if _, ok := actual.(json.Number); !ok {
			return fmt.Errorf("%s: expected a number, got %s", at(path), render(actual))
		}
	case "odd", "even":
		n, ok := actual.(json.Number)
		i, err := n.Int64()
		if !ok || err != nil {
			return fmt.Errorf("%s: expected an integer, got %s", at(path), render(actual))
		}
		if (i%2 != 0) != (name == "odd") {
			return fmt.Errorf("%s: expected an %s integer, got %d", at(path), name, i)
		}
	case "bool":
		if _, ok := actual.(bool); !ok {
			return fmt.Errorf("%s: expected a boolean, got %s", at(path), render(actual))
		}
	case "bind":
		if argument == "" {
			return fmt.Errorf("%s: $bind names nothing", at(path))
		}
		b[argument] = actual
	case "not":
		bound, ok := b[argument]
		if !ok {
			return fmt.Errorf("%s: $not:%s refers to nothing bound", at(path), argument)
		}
		if equal(bound, actual) {
			return fmt.Errorf("%s: expected anything but %s", at(path), render(bound))
		}
	case "regex":
		s, ok := actual.(string)
		if !ok {
			return fmt.Errorf("%s: expected a string matching %s, got %s", at(path), argument, render(actual))
		}
		pattern, err := regexp.Compile(argument)
		if err != nil {
			return fmt.Errorf("%s: $regex:%s: %v", at(path), argument, err)
		}
		if !pattern.MatchString(s) {
			return fmt.Errorf("%s: expected a string matching %s, got %q", at(path), argument, s)
		}
	default:
		// $name, or $row.member: what a step or a foreach bound.
		bound, ok := resolve(placeholder[1:], b)
		if !ok {
			return fmt.Errorf("%s: %s refers to nothing bound", at(path), placeholder)
		}
		if !equal(bound, actual) {
			return fmt.Errorf("%s: expected %s (bound as %s), got %s", at(path), render(bound), placeholder, render(actual))
		}
	}
	return nil
}

// matchContains holds an ordered subsequence: every expected element is
// found in the actual list, each after the previous match. With sequenceBy
// the order is held per value of that member, so that what came from
// several goroutines may interleave and still be held in order each.
func matchContains(path string, contains, sequenceBy, actual any, b Bindings) error {
	wanted, ok := contains.([]any)
	if !ok {
		return fmt.Errorf("%s: $contains takes an array", at(path))
	}
	list, ok := actual.([]any)
	if !ok {
		return fmt.Errorf("%s: expected an array, got %s", at(path), render(actual))
	}
	key, _ := sequenceBy.(string)
	cursor := map[string]int{}
	for i, want := range wanted {
		lane := ""
		if key != "" {
			if object, ok := want.(map[string]any); ok {
				if v, ok := object[key]; ok {
					lane = render(v)
				}
			}
		}
		found := false
		for j := cursor[lane]; j < len(list); j++ {
			if key != "" && lane != "" {
				object, ok := list[j].(map[string]any)
				if !ok || render(object[key]) != lane {
					continue
				}
			}
			if matchAt(fmt.Sprintf("%s[%d]", path, i), want, list[j], b) == nil {
				cursor[lane] = j + 1
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%s: element %d of $contains, %s, is not found in order in %s", at(path), i, render(want), render(actual))
		}
	}
	return nil
}

// resolve looks a placeholder's name up in the bindings, descending into
// members on dots: row.frame is member frame of what row was bound to.
func resolve(name string, b Bindings) (any, bool) {
	head, rest, nested := strings.Cut(name, ".")
	value, ok := b[head]
	if !ok {
		return nil, false
	}
	for nested {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		head, rest, nested = strings.Cut(rest, ".")
		if value, ok = object[head]; !ok {
			return nil, false
		}
	}
	return value, true
}

// substitute replaces every placeholder in a step's arguments with what it
// refers to, so that a handle bound by one step is the on of the next; a
// value that is not a reference is kept as it is.
func substitute(value any, b Bindings) (any, error) {
	switch v := value.(type) {
	case string:
		if !strings.HasPrefix(v, "$") || strings.HasPrefix(v, "$$") {
			return strings.TrimPrefix(v, "$"), nil
		}
		bound, ok := resolve(v[1:], b)
		if !ok {
			return nil, fmt.Errorf("%s refers to nothing bound", v)
		}
		return bound, nil
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, member := range v {
			replaced, err := substitute(member, b)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			out[key] = replaced
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, member := range v {
			replaced, err := substitute(member, b)
			if err != nil {
				return nil, err
			}
			out[i] = replaced
		}
		return out, nil
	}
	return value, nil
}

func sameNumber(a, b json.Number) bool {
	if a == b {
		return true
	}
	x, errX := a.Float64()
	y, errY := b.Float64()
	return errX == nil && errY == nil && x == y
}

// equal compares two decoded JSON values as values.
func equal(a, b any) bool {
	switch x := a.(type) {
	case json.Number:
		y, ok := b.(json.Number)
		return ok && sameNumber(x, y)
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for key, v := range x {
			w, ok := y[key]
			if !ok || !equal(v, w) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !equal(x[i], y[i]) {
				return false
			}
		}
		return true
	}
	return a == b
}

// decode reads JSON as the runner compares it: numbers kept as spelled.
func decode(data []byte) (any, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func render(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}

func at(path string) string {
	if path == "" {
		return "the answer"
	}
	return "the answer" + path
}
