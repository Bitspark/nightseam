// Package naming derives identifiers from the names a contract declares,
// by one convention every target shares: a name's alphanumeric words,
// spelled in upper or lower camel case, with the initialisms a language
// keeps in capitals. A target file overrides the convention where it must;
// nothing here is target-specific.
package naming

import (
	"regexp"
	"strings"
)

var wordPattern = regexp.MustCompile(`[A-Za-z0-9]+`)

// initialisms are the words spelled in capitals in upper camel case: ID,
// URL, not Id, Url.
var initialisms = map[string]bool{"id": true, "api": true, "url": true, "http": true, "json": true, "uuid": true}

// words splits a declared name into its alphanumeric words: work_item_id
// and frame.relayed alike.
func words(name string) []string { return wordPattern.FindAllString(name, -1) }

// UpperCamel joins a name's words with each capitalised, initialisms in
// full: work_item_id → WorkItemID, frame.relayed → FrameRelayed.
func UpperCamel(name string) string {
	var out strings.Builder
	for _, word := range words(name) {
		out.WriteString(capitalise(word))
	}
	return out.String()
}

// LowerCamel joins a name's words with the first in lower case and the
// rest capitalised, initialisms like any word, as TypeScript spells them:
// work_item_id → workItemId, not_found → notFound.
func LowerCamel(name string) string {
	var out strings.Builder
	for i, word := range words(name) {
		word = strings.ToLower(word)
		if i > 0 {
			word = strings.ToUpper(word[:1]) + word[1:]
		}
		out.WriteString(word)
	}
	return out.String()
}

func capitalise(word string) string {
	if initialisms[strings.ToLower(word)] {
		return strings.ToUpper(word)
	}
	return strings.ToUpper(word[:1]) + word[1:]
}

// Derived is the name a shape written inline is generated under: the upper
// camel of the path to it, joined. A path is the declaration the shape sits
// in and then each step that names something — a field's name, a union's
// variant tag, an operation and the role the shape plays in it — while the
// steps that name nothing, `array`, `map`, `nullable` and an application's
// slot, are passed over, so that wrapping a shape in a list does not rename
// it. One rule, so that every language spells a derived name one way:
//
//	Derived("EchoRequest", "note")   → EchoRequestNote
//	Derived("echo", "request")       → EchoRequest
//	Derived("frame.relayed", "event") → FrameRelayedEvent
//	Derived("Part", "text", "body")  → PartTextBody
func Derived(path ...string) string {
	var out strings.Builder
	for _, step := range path {
		out.WriteString(UpperCamel(step))
	}
	return out.String()
}
