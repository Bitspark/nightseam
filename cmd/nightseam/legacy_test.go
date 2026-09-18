package main

import (
	legacygolang "github.com/Bitspark/nightseam/internal/legacy/languages/golang"
	legacytypescript "github.com/Bitspark/nightseam/internal/legacy/languages/typescript"
	legacyspi "github.com/Bitspark/nightseam/internal/legacy/spi"
)

// languages is the v1 composition root, kept for the tests that render
// with the legacy generator until it is deleted: the v1 generation of the
// slow fixtures, and the equivalence of what v2 renders with what v1 did.
func languages(module, scope string) []legacyspi.Language {
	return []legacyspi.Language{
		legacygolang.New(legacygolang.Options{Module: module}),
		legacytypescript.New(legacytypescript.Options{Scope: scope}),
	}
}
