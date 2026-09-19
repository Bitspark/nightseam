package pattern

import (
	"fmt"
	"regexp"
	"regexp/syntax"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Compile translates Nightseam's regular subset of ECMAScript Unicode
// patterns to Go's code-point engine. Parsing first excludes Go-only
// escapes, permissive identity escapes and invalid class range endpoints.
func Compile(source string) (*Regexp, error) {
	p := parser{source: source}
	translated, err := p.disjunction(false)
	if err != nil {
		return nil, fmt.Errorf("it is not a Nightseam Unicode regular expression: %w", err)
	}
	compiled, err := regexp.Compile(translated)
	if parseError, ok := err.(*syntax.Error); ok && (parseError.Code == syntax.ErrInvalidRepeatSize || parseError.Code == syntax.ErrLarge) {
		// ECMAScript has no RE2 counted-repetition ceiling. The parsed
		// expression matches directly when native expansion is too large.
		return &Regexp{tree: p.tree}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("it is not a regular expression: %w", err)
	}
	return &Regexp{native: compiled, tree: p.tree}, nil
}

type parser struct {
	source string
	pos    int
	tree   *node
}

func (p *parser) fail(message string) error { return fmt.Errorf("%s at byte %d", message, p.pos) }

func (p *parser) disjunction(group bool) (string, error) {
	var out strings.Builder
	var branches, sequence []*node
	finish := func() { p.tree = joinNode('|', append(branches, joinNode('s', sequence))) }
	for p.pos < len(p.source) {
		if p.source[p.pos] == ')' {
			if !group {
				return "", p.fail("unmatched closing parenthesis")
			}
			p.pos++
			finish()
			return out.String(), nil
		}
		if p.source[p.pos] == '|' {
			out.WriteByte('|')
			p.pos++
			branches = append(branches, joinNode('s', sequence))
			sequence = nil
			continue
		}
		atom, assertion, err := p.atom()
		if err != nil {
			return "", err
		}
		atomTree := p.tree
		if p.pos == len(p.source) {
			out.WriteString(atom)
			sequence = append(sequence, atomTree)
			continue
		}
		start := p.pos
		var minimum, maximum uint64
		var lowerText, upperText string
		unlimited, counted := false, false
		switch p.source[p.pos] {
		case '*', '+', '?':
			unlimited = p.source[p.pos] != '?'
			maximum = 1
			if p.source[p.pos] == '+' {
				minimum = 1
			}
			p.pos++
		case '{':
			counted = true
			p.pos++
			lower := p.pos
			if !p.digits() {
				return "", p.fail("a repetition requires a lower bound")
			}
			lowerText = decimal(p.source[lower:p.pos])
			upperText = lowerText
			if p.pos < len(p.source) && p.source[p.pos] == ',' {
				p.pos++
				upper := p.pos
				unlimited = true
				if p.digits() {
					upperText = decimal(p.source[upper:p.pos])
					unlimited = false
				}
			}
			if p.pos == len(p.source) || p.source[p.pos] != '}' {
				return "", p.fail("invalid repetition")
			}
			p.pos++
			if !unlimited && (len(upperText) < len(lowerText) || len(upperText) == len(lowerText) && upperText < lowerText) {
				return "", p.fail("repetition bounds are reversed")
			}
			// Saturating bounds retain the answer for any Go string without
			// allocating or iterating once per declared repetition.
			minimum, _ = strconv.ParseUint(lowerText, 10, 64)
			maximum, _ = strconv.ParseUint(upperText, 10, 64)
		default:
			out.WriteString(atom)
			sequence = append(sequence, atomTree)
			continue
		}
		if assertion {
			return "", p.fail("an assertion cannot be repeated")
		}
		lazy := p.pos < len(p.source) && p.source[p.pos] == '?'
		if lazy {
			p.pos++
		}
		sequence = append(sequence, repeatNode(atomTree, minimum, maximum, unlimited))
		out.WriteString(atom)
		if !counted {
			out.WriteString(p.source[start:p.pos])
			continue
		}
		if unlimited {
			fmt.Fprintf(&out, "{%s,}", lowerText)
		} else if upperText == lowerText {
			fmt.Fprintf(&out, "{%s}", lowerText)
		} else {
			fmt.Fprintf(&out, "{%s,%s}", lowerText, upperText)
		}
		if lazy {
			out.WriteByte('?')
		}
	}
	if group {
		return "", p.fail("unclosed group")
	}
	finish()
	return out.String(), nil
}

func decimal(value string) string {
	if value = strings.TrimLeft(value, "0"); value == "" {
		return "0"
	}
	return value
}

func (p *parser) digits() bool {
	start := p.pos
	for p.pos < len(p.source) && p.source[p.pos] >= '0' && p.source[p.pos] <= '9' {
		p.pos++
	}
	return p.pos > start
}

func (p *parser) atom() (string, bool, error) {
	rest := p.source[p.pos:]
	if found, ok := opens(rest); ok {
		return "", false, p.fail(fmt.Sprintf("%q is %s", found.opens, found.says))
	}
	c, size := utf8.DecodeRuneInString(rest)
	p.pos += size
	switch c {
	case '^', '$':
		p.tree = &node{kind: byte(c)}
		return string(c), true, nil
	case '.':
		p.tree = setNode(characters{{10, 10}, {13, 13}, {0x2028, 0x2029}}.complement())
		return `[^\x{a}\x{d}\x{2028}\x{2029}]`, false, nil
	case '(':
		if p.pos < len(p.source) && p.source[p.pos] == '?' {
			if !strings.HasPrefix(p.source[p.pos:], "?:") {
				return "", false, p.fail("unsupported group prefix")
			}
			p.pos += 2
		}
		inner, err := p.disjunction(true)
		return "(?:" + inner + ")", false, err
	case '[':
		set, err := p.class()
		p.tree = setNode(set)
		return set.pattern(), false, err
	case '\\':
		set, assertion, err := p.escape(false)
		if assertion != "" {
			p.tree = &node{kind: assertion[1]}
			return assertion, true, err
		}
		p.tree = setNode(set)
		return set.pattern(), false, err
	case '*', '+', '?', '{', '}', ']':
		return "", false, p.fail("unescaped syntax character")
	default:
		p.tree = setNode(character(c))
		return character(c).pattern(), false, nil
	}
}

type interval struct{ lo, hi rune }
type characters []interval

func character(r rune) characters { return characters{{r, r}} }

var digits = characters{{'0', '9'}}
var words = characters{{'0', '9'}, {'A', 'Z'}, {'_', '_'}, {'a', 'z'}}

// ECMAScript WhiteSpace and LineTerminator, without case-folding flags.
var whitespace = characters{
	{0x9, 0xd}, {0x20, 0x20}, {0xa0, 0xa0}, {0x1680, 0x1680},
	{0x2000, 0x200a}, {0x2028, 0x2029}, {0x202f, 0x202f},
	{0x205f, 0x205f}, {0x3000, 0x3000}, {0xfeff, 0xfeff},
}

func (set characters) normalized() characters {
	set = append(characters(nil), set...)
	sort.Slice(set, func(i, j int) bool { return set[i].lo < set[j].lo })
	var result characters
	for _, span := range set {
		if len(result) > 0 && span.lo <= result[len(result)-1].hi+1 {
			if span.hi > result[len(result)-1].hi {
				result[len(result)-1].hi = span.hi
			}
		} else {
			result = append(result, span)
		}
	}
	return result
}

func (set characters) complement() characters {
	var result characters
	next := rune(0)
	for _, span := range set.normalized() {
		if next < span.lo {
			result = append(result, interval{next, span.lo - 1})
		}
		next = span.hi + 1
	}
	if next <= utf8.MaxRune {
		result = append(result, interval{next, utf8.MaxRune})
	}
	return result
}

func (set characters) pattern() string {
	// A surrogate is not a scalar in a Go string. Remove that unmatchable
	// interval before handing the set to regexp: its literal-prefix
	// optimization otherwise encodes an isolated surrogate as U+FFFD.
	var scalars characters
	for _, span := range set {
		if span.hi < 0xd800 || span.lo > 0xdfff {
			scalars = append(scalars, span)
			continue
		}
		if span.lo < 0xd800 {
			scalars = append(scalars, interval{span.lo, 0xd7ff})
		}
		if span.hi > 0xdfff {
			scalars = append(scalars, interval{0xe000, span.hi})
		}
	}
	set = scalars
	if len(set) == 0 {
		return `[^\x{0}-\x{10ffff}]`
	}
	var out strings.Builder
	out.WriteByte('[')
	for _, span := range set.normalized() {
		fmt.Fprintf(&out, `\x{%x}`, span.lo)
		if span.hi != span.lo {
			fmt.Fprintf(&out, `-\x{%x}`, span.hi)
		}
	}
	out.WriteByte(']')
	return out.String()
}

func (set characters) single() (rune, bool) {
	if len(set) == 1 && set[0].lo == set[0].hi {
		return set[0].lo, true
	}
	return 0, false
}

func (p *parser) class() (characters, error) {
	negated := p.pos < len(p.source) && p.source[p.pos] == '^'
	if negated {
		p.pos++
	}
	var result characters
	for p.pos < len(p.source) && p.source[p.pos] != ']' {
		left, err := p.classAtom()
		if err != nil {
			return nil, err
		}
		if p.pos+1 < len(p.source) && p.source[p.pos] == '-' && p.source[p.pos+1] != ']' {
			p.pos++
			right, err := p.classAtom()
			if err != nil {
				return nil, err
			}
			lo, leftSingle := left.single()
			hi, rightSingle := right.single()
			if !leftSingle || !rightSingle || lo > hi {
				return nil, p.fail("invalid character-class range")
			}
			result = append(result, interval{lo, hi})
		} else {
			result = append(result, left...)
		}
	}
	if p.pos == len(p.source) {
		return nil, p.fail("unclosed character class")
	}
	p.pos++
	if negated {
		result = result.complement()
	}
	return result, nil
}

func (p *parser) classAtom() (characters, error) {
	c, size := utf8.DecodeRuneInString(p.source[p.pos:])
	p.pos += size
	if c != '\\' {
		return character(c), nil
	}
	set, _, err := p.escape(true)
	return set, err
}

func (p *parser) escape(class bool) (characters, string, error) {
	if p.pos == len(p.source) {
		return nil, "", p.fail("trailing escape")
	}
	if found, ok := opens(p.source[p.pos-1:]); ok {
		return nil, "", p.fail(fmt.Sprintf("%q is %s", found.opens, found.says))
	}
	c := p.source[p.pos]
	p.pos++
	switch c {
	case 'd':
		return digits, "", nil
	case 'D':
		return digits.complement(), "", nil
	case 'w':
		return words, "", nil
	case 'W':
		return words.complement(), "", nil
	case 's':
		return whitespace, "", nil
	case 'S':
		return whitespace.complement(), "", nil
	case 'b':
		if class {
			return character(8), "", nil
		}
		return nil, `\b`, nil
	case 'B':
		if !class {
			return nil, `\B`, nil
		}
	case 'f':
		return character('\f'), "", nil
	case 'n':
		return character('\n'), "", nil
	case 'r':
		return character('\r'), "", nil
	case 't':
		return character('\t'), "", nil
	case 'v':
		return character(11), "", nil
	case '0':
		if p.pos < len(p.source) && p.source[p.pos] >= '0' && p.source[p.pos] <= '9' {
			return nil, "", p.fail("legacy octal escape")
		}
		return character(0), "", nil
	case 'c':
		if p.pos < len(p.source) {
			letter := p.source[p.pos]
			if letter >= 'A' && letter <= 'Z' || letter >= 'a' && letter <= 'z' {
				p.pos++
				return character(rune(letter & 31)), "", nil
			}
		}
		return nil, "", p.fail("a control escape requires an ASCII letter")
	case 'x':
		value, err := p.hex(2)
		return character(value), "", err
	case 'u':
		if p.pos < len(p.source) && p.source[p.pos] == '{' {
			p.pos++
			start := p.pos
			for p.pos < len(p.source) && isHex(p.source[p.pos]) {
				p.pos++
			}
			if p.pos == start || p.pos == len(p.source) || p.source[p.pos] != '}' {
				return nil, "", p.fail("invalid Unicode escape")
			}
			value, err := strconv.ParseUint(p.source[start:p.pos], 16, 32)
			p.pos++
			if err != nil || value > utf8.MaxRune {
				return nil, "", p.fail("Unicode escape out of range")
			}
			return character(rune(value)), "", nil
		}
		value, err := p.hex(4)
		if err != nil {
			return nil, "", err
		}
		if value >= 0xd800 && value <= 0xdbff && strings.HasPrefix(p.source[p.pos:], `\u`) {
			end := p.pos
			p.pos += 2
			trail, err := p.hex(4)
			if err == nil && trail >= 0xdc00 && trail <= 0xdfff {
				value = utf16.DecodeRune(value, trail)
			} else {
				p.pos = end
			}
		}
		return character(value), "", nil
	default:
		if strings.ContainsRune(`^$\.*+?()[]{}|/`, rune(c)) || class && c == '-' {
			return character(rune(c)), "", nil
		}
	}
	return nil, "", p.fail("escape is outside Unicode pattern syntax")
}

func isHex(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

func (p *parser) hex(count int) (rune, error) {
	start := p.pos
	for i := 0; i < count; i++ {
		if p.pos == len(p.source) || !isHex(p.source[p.pos]) {
			return 0, p.fail("invalid hexadecimal escape")
		}
		p.pos++
	}
	value, _ := strconv.ParseUint(p.source[start:p.pos], 16, 32)
	return rune(value), nil
}
