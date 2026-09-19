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
func Compile(source string) (*regexp.Regexp, error) {
	p := parser{source: source}
	translated, err := p.disjunction(false)
	if err != nil {
		return nil, fmt.Errorf("it is not a Nightseam Unicode regular expression: %w", err)
	}
	compiled, err := regexp.Compile(translated)
	if parseError, ok := err.(*syntax.Error); ok && parseError.Code == syntax.ErrInvalidRepeatSize {
		// ECMAScript has no RE2 1000-repetition ceiling. Expand counted
		// repeats only when that implementation limit prevents compilation.
		p = parser{source: source, expandCounts: true}
		translated, err = p.disjunction(false)
		if err == nil {
			compiled, err = regexp.Compile(translated)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("it is not a regular expression: %w", err)
	}
	return compiled, nil
}

type parser struct {
	source       string
	pos          int
	expandCounts bool
}

func (p *parser) fail(message string) error { return fmt.Errorf("%s at byte %d", message, p.pos) }

func (p *parser) disjunction(group bool) (string, error) {
	var out strings.Builder
	for p.pos < len(p.source) {
		if p.source[p.pos] == ')' {
			if !group {
				return "", p.fail("unmatched closing parenthesis")
			}
			p.pos++
			return out.String(), nil
		}
		if p.source[p.pos] == '|' {
			out.WriteByte('|')
			p.pos++
			continue
		}
		atom, assertion, err := p.atom()
		if err != nil {
			return "", err
		}
		if p.pos == len(p.source) {
			out.WriteString(atom)
			continue
		}
		start := p.pos
		min, max := -1, -1
		switch p.source[p.pos] {
		case '*', '+', '?':
			p.pos++
		case '{':
			p.pos++
			lower := p.pos
			if !p.digits() {
				return "", p.fail("a repetition requires a lower bound")
			}
			min, err = strconv.Atoi(p.source[lower:p.pos])
			if err != nil {
				return "", p.fail("repetition exceeds engine capacity")
			}
			max = min
			if p.pos < len(p.source) && p.source[p.pos] == ',' {
				p.pos++
				upper := p.pos
				max = -1
				if p.digits() {
					max, err = strconv.Atoi(p.source[upper:p.pos])
					if err != nil {
						return "", p.fail("repetition exceeds engine capacity")
					}
				}
			}
			if p.pos == len(p.source) || p.source[p.pos] != '}' {
				return "", p.fail("invalid repetition")
			}
			p.pos++
			if max >= 0 && max < min {
				return "", p.fail("repetition bounds are reversed")
			}
		default:
			out.WriteString(atom)
			continue
		}
		if assertion {
			return "", p.fail("an assertion cannot be repeated")
		}
		lazy := p.pos < len(p.source) && p.source[p.pos] == '?'
		if lazy {
			p.pos++
		}
		if min < 0 {
			out.WriteString(atom)
			out.WriteString(p.source[start:p.pos])
			continue
		}
		if p.expandCounts {
			copies := min
			if max >= 0 {
				copies = max
			}
			if len(atom) > 0 && copies > (1<<20)/len(atom) {
				return "", p.fail("repetition exceeds engine capacity")
			}
			out.WriteString(strings.Repeat(atom, min))
			if max < 0 {
				out.WriteString(atom + "*")
			} else {
				out.WriteString(strings.Repeat("(?:"+atom+")?", max-min))
			}
		} else {
			out.WriteString(atom)
			if max == min {
				fmt.Fprintf(&out, "{%d}", min)
			} else if max < 0 {
				fmt.Fprintf(&out, "{%d,}", min)
			} else {
				fmt.Fprintf(&out, "{%d,%d}", min, max)
			}
			if lazy {
				out.WriteByte('?')
			}
		}
	}
	if group {
		return "", p.fail("unclosed group")
	}
	return out.String(), nil
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
		return string(c), true, nil
	case '.':
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
		return set.pattern(), false, err
	case '\\':
		set, assertion, err := p.escape(false)
		if assertion != "" {
			return assertion, true, err
		}
		return set.pattern(), false, err
	case '*', '+', '?', '{', '}', ']':
		return "", false, p.fail("unescaped syntax character")
	default:
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
