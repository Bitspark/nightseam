package spi

import (
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
)

// Speller is a language target's view of an accepted family for documents.
// Its answers use the same names and emitters as the generated package.
type Speller interface {
	Spell(f *render.Family, e model.TypeExpr) string
	Declare(f *render.Family, t *model.Type) string
	Invoke(f *render.Family, side, op string) Invocation
}

// Invocation is consumer code for an operation. Call is an expression using
// client or remote, ctx, and params (or data for an event). Handle is the
// signature in the implementation Scaffold writes. A surface the target
// does not generate is empty. side is the declaration's server or client side.
type Invocation struct{ Call, Handle string }
