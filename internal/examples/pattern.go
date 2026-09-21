package examples

import (
	"regexp/syntax"
	"strings"
	"unicode/utf8"

	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/pattern"
)

// patternExample searches a bounded set of deterministic witnesses. The Go
// syntax tree is only a source of candidates, never the pattern's authority:
// every candidate must match the actual Nightseam Unicode dialect. A dialect
// spelling Go cannot parse, or an exhausted search, produces no witness.
func patternExample(source string, length *model.Length) (string, bool) {
	const limit = 256
	minimum, maximum := 0, 4096
	if length != nil {
		if length.Min != nil {
			minimum = *length.Min
		}
		if length.Max != nil {
			maximum = min(maximum, *length.Max)
		}
	}
	if minimum > maximum || minimum < 0 || maximum < 0 {
		return "", false
	}
	matcher, err := pattern.Compile(source)
	if err != nil {
		return "", false
	}
	tree, err := syntax.Parse(source, syntax.Perl)
	if err != nil {
		return "", false
	}
	product := func(left, right []string) []string {
		var out []string
		for _, a := range left {
			for _, b := range right {
				if utf8.RuneCountInString(a)+utf8.RuneCountInString(b) <= maximum {
					out = append(out, a+b)
					if len(out) == limit {
						return out
					}
				}
			}
		}
		return out
	}
	var candidates func(*syntax.Regexp) []string
	candidates = func(r *syntax.Regexp) []string {
		switch r.Op {
		case syntax.OpNoMatch:
			return nil
		case syntax.OpEmptyMatch, syntax.OpBeginLine, syntax.OpEndLine, syntax.OpBeginText, syntax.OpEndText, syntax.OpWordBoundary, syntax.OpNoWordBoundary:
			return []string{""}
		case syntax.OpLiteral:
			if len(r.Rune) > maximum {
				return nil
			}
			return []string{string(r.Rune)}
		case syntax.OpAnyChar, syntax.OpAnyCharNotNL:
			return []string{"a", "0", " "}
		case syntax.OpCharClass:
			var out []string
			seen := map[rune]bool{}
			add := func(c rune) {
				if seen[c] || c >= 0xd800 && c <= 0xdfff {
					return
				}
				for i := 0; i < len(r.Rune); i += 2 {
					if c >= r.Rune[i] && c <= r.Rune[i+1] {
						seen[c] = true
						out = append(out, string(c))
						return
					}
				}
			}
			for _, c := range "aA0_ -@😀" {
				add(c)
			}
			for i := 0; i < len(r.Rune) && len(out) < limit; i += 2 {
				add(r.Rune[i])
			}
			return out
		case syntax.OpCapture:
			return candidates(r.Sub[0])
		case syntax.OpConcat:
			out := []string{""}
			for _, sub := range r.Sub {
				out = product(out, candidates(sub))
			}
			return out
		case syntax.OpAlternate:
			var out []string
			for _, sub := range r.Sub {
				out = append(out, candidates(sub)...)
				if len(out) >= limit {
					return out[:limit]
				}
			}
			return out
		case syntax.OpStar, syntax.OpPlus, syntax.OpQuest, syntax.OpRepeat:
			lo, hi := r.Min, r.Max
			switch r.Op {
			case syntax.OpStar:
				lo, hi = 0, -1
			case syntax.OpPlus:
				lo, hi = 1, -1
			case syntax.OpQuest:
				lo, hi = 0, 1
			}
			if lo > maximum {
				return nil
			}
			// The shortest branch is tried first; longer repeats cover a
			// field's minimum length without unbounded Cartesian expansion.
			if hi < 0 {
				hi = maximum
			}
			hi = min(hi, maximum)
			unit := candidates(r.Sub[0])
			var out []string
			for _, s := range unit {
				for count := lo; count <= hi && len(out) < limit; count++ {
					if len(s) == 0 && count > lo || utf8.RuneCountInString(s)*count > maximum {
						break
					}
					out = append(out, strings.Repeat(s, count))
				}
				if len(out) == limit {
					break
				}
			}
			return out
		}
		return nil
	}
	for _, candidate := range candidates(tree) {
		options := []string{candidate}
		if missing := minimum - utf8.RuneCountInString(candidate); missing > 0 {
			padding := strings.Repeat("a", missing)
			options = append(options, candidate+padding, padding+candidate)
		}
		for _, s := range options {
			n := utf8.RuneCountInString(s)
			if n >= minimum && n <= maximum && matcher.MatchString(s) {
				return s, true
			}
		}
	}
	return "", false
}
