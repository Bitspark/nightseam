// Package pattern shares declaration syntax checks with runtime descriptors.
package pattern

import (
	"fmt"
	"regexp"
	"strings"
)

// Dialect is the one sentence that says what a `pattern` is written in.
const Dialect = "A pattern is written in Nightseam's own regular-expression language: ECMAScript syntax without lookaround and without backreferences, which is what every planned runtime's engine can express and mean the same by."

// spelling is one spelling outside the dialect: the text that opens it, and
// which engine it belongs to, so that a diagnostic says why it is refused.
type spelling struct {
	opens string
	says  string
}

// outside are the spellings the dialect refuses, in both directions: the
// ones only an RE2 engine reads (Go's `regexp`, Rust's `regex`), and the
// ones only an ECMAScript engine reads (a browser's, Node's). A pattern
// that uses either means two things in two runtimes and is refused where
// it is written rather than where it is run.
var outside = []spelling{
	{`(?P<`, "a named group of RE2, which ECMAScript does not read"},
	{`(?P=`, "a backreference to a named group of RE2, which ECMAScript does not read"},
	{`(?<=`, "a lookbehind of ECMAScript, which the dialect has no lookaround in"},
	{`(?<!`, "a lookbehind of ECMAScript, which the dialect has no lookaround in"},
	{`(?<`, "a named group of ECMAScript, which RE2 does not read"},
	{`(?=`, "a lookahead of ECMAScript, which the dialect has no lookaround in"},
	{`(?!`, "a lookahead of ECMAScript, which the dialect has no lookaround in"},
	{`[[:`, "a POSIX class of RE2, which ECMAScript reads as an ordinary character class and quietly means something else by"},
	{`\k<`, "a backreference of ECMAScript, which the dialect has none of"},
	{`\p{`, "a Unicode class, which ECMAScript reads only under a flag the dialect does not carry"},
	{`\P{`, "a Unicode class, which ECMAScript reads only under a flag the dialect does not carry"},
	{`\A`, "an anchor of RE2, which ECMAScript does not read; write ^"},
	{`\z`, "an anchor of RE2, which ECMAScript does not read; write $"},
	{`\Z`, "an anchor of RE2, which ECMAScript does not read; write $"},
	{`\Q`, "a literal span of RE2, which ECMAScript does not read"},
	{`\C`, "the any-byte of RE2, which ECMAScript does not read"},
}

// inlineFlags opens an inline flag group, (?i), (?s:…) and their kin: RE2
// reads them and ECMAScript throws on them.
var inlineFlags = regexp.MustCompile(`^\(\?[imsUu-]+[):]`)

// backreference is \1 … \9, which ECMAScript reads and RE2 does not.
var backreference = regexp.MustCompile(`^\\[1-9]`)

// Check holds a field's pattern to the dialect, in both directions. It
// reports what is outside it, or that it is not a regular expression at
// all.
func Check(pattern string) error {
	for i := 0; i < len(pattern); i++ {
		rest := pattern[i:]
		if pattern[i] == '\\' {
			if backreference.MatchString(rest) {
				return fmt.Errorf("%q is a backreference, which the dialect has none of", rest[:2])
			}
			if found, ok := opens(rest); ok {
				return fmt.Errorf("%q is %s", found.opens, found.says)
			}
			i++ // what is escaped is a literal, whatever it is
			continue
		}
		if pattern[i] == '(' && inlineFlags.MatchString(rest) {
			return fmt.Errorf("%q sets flags inline, which RE2 reads and ECMAScript throws on", inlineFlags.FindString(rest))
		}
		if found, ok := opens(rest); ok {
			return fmt.Errorf("%q is %s", found.opens, found.says)
		}
	}
	if _, err := regexp.Compile(pattern); err != nil {
		return fmt.Errorf("it is not a regular expression: %v", err)
	}
	return nil
}

func opens(rest string) (spelling, bool) {
	for _, s := range outside {
		if strings.HasPrefix(rest, s.opens) {
			return s, true
		}
	}
	return spelling{}, false
}
