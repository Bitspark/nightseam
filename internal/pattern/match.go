package pattern

import (
	"math"
	"regexp"
	"slices"
)

// Regexp matches Nightseam patterns. The usual path is Go's engine; a
// counted expression beyond that engine's expansion limit uses its parsed
// expression directly, without allocating one copy per repetition.
type Regexp struct {
	native *regexp.Regexp
	tree   *node
}

func (r *Regexp) MatchString(value string) bool {
	if r.native != nil {
		return r.native.MatchString(value)
	}
	state := matchState{input: []rune(value), memo: map[matchKey][]int{}}
	for start := 0; start <= len(state.input); start++ {
		if len(state.ends(r.tree, start)) > 0 {
			return true
		}
	}
	return false
}

type node struct {
	kind            byte
	set             characters
	children        []*node
	min, max, width uint64
	unlimited       bool
}

func setNode(set characters) *node { return &node{kind: 'c', set: set.normalized(), width: 1} }

func joinNode(kind byte, children []*node) *node {
	if len(children) == 1 {
		return children[0]
	}
	n := &node{kind: kind, children: children}
	if kind == '|' {
		n.width = math.MaxUint64
	}
	for _, child := range children {
		if kind == '|' {
			n.width = min(n.width, child.width)
		} else {
			n.width = addWidth(n.width, child.width)
		}
	}
	return n
}

func addWidth(a, b uint64) uint64 {
	if a > math.MaxUint64-b {
		return math.MaxUint64
	}
	return a + b
}

func repeatNode(child *node, min, max uint64, unlimited bool) *node {
	width := child.width * min
	if min != 0 && child.width > math.MaxUint64/min {
		width = math.MaxUint64
	}
	return &node{kind: 'r', children: []*node{child}, min: min, max: max, unlimited: unlimited, width: width}
}

type matchKey struct {
	node  *node
	start int
}
type matchState struct {
	input []rune
	memo  map[matchKey][]int
}

func (s *matchState) ends(n *node, start int) []int {
	if n.width > uint64(len(s.input)-start) {
		return nil
	}
	key := matchKey{n, start}
	if result, ok := s.memo[key]; ok {
		return result
	}
	var result []int
	switch n.kind {
	case 'c':
		if start < len(s.input) {
			for _, span := range n.set {
				if s.input[start] >= span.lo && s.input[start] <= span.hi {
					result = []int{start + 1}
					break
				}
			}
		}
	case '^':
		if start == 0 {
			result = []int{start}
		}
	case '$':
		if start == len(s.input) {
			result = []int{start}
		}
	case 'b', 'B':
		word := func(at int) bool {
			if at < 0 || at >= len(s.input) {
				return false
			}
			c := s.input[at]
			return c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c == '_' || c >= 'a' && c <= 'z'
		}
		if (word(start-1) != word(start)) == (n.kind == 'b') {
			result = []int{start}
		}
	case 's':
		result = []int{start}
		for _, child := range n.children {
			result = s.advance(child, result)
			if len(result) == 0 {
				break
			}
		}
	case '|':
		for _, child := range n.children {
			result = append(result, s.ends(child, start)...)
		}
		result = uniquePositions(result)
	case 'r':
		current := []int{start}
		var count uint64
		for count < n.min && len(current) > 0 {
			next := s.advance(n.children[0], current)
			count++
			if slices.Equal(next, current) {
				count = n.min
				current = next
				break
			}
			current = next
		}
		if len(current) > 0 {
			result = append(result, current...)
			for n.unlimited || count < n.max {
				next := s.advance(n.children[0], current)
				if len(next) == 0 || slices.Equal(next, current) {
					break
				}
				result = append(result, next...)
				current = next
				count++
			}
			result = uniquePositions(result)
		}
	}
	s.memo[key] = result
	return result
}

// Every transition moves forward or stays put. Repeating an unchanged set
// is therefore a fixed point, including arbitrarily large nullable counts.
func (s *matchState) advance(child *node, positions []int) []int {
	var result []int
	for _, start := range positions {
		result = append(result, s.ends(child, start)...)
	}
	return uniquePositions(result)
}

func uniquePositions(positions []int) []int { slices.Sort(positions); return slices.Compact(positions) }
