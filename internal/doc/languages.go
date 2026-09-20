package doc

import (
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
	"github.com/Bitspark/nightseam/internal/spi"
)

// Language is what a language target says about a documented declaration.
// A type has a Name and Declare; an operation has the available Invoke
// surfaces. Writers use the map's target name as its code-fence language.
type Language struct {
	Name, Declare string
	Invoke        spi.Invocation
}

func addLanguages(d *Family, f *render.Family, spellers map[string]spi.Speller) {
	if len(spellers) == 0 {
		return
	}
	for _, group := range [][]*Type{d.Types, d.Carried} {
		for _, typ := range group {
			typ.Languages = make(map[string]Language, len(spellers))
			resolved := f.Type(typ.Name)
			expression := model.TypeExpr(model.Named{Name: typ.Name})
			if resolved.Inline {
				expression = model.Inline{Type: resolved.Declaration}
			}
			for name, speller := range spellers {
				typ.Languages[name] = Language{Name: speller.Spell(f, expression), Declare: speller.Declare(f, resolved.Declaration)}
			}
		}
	}
	for _, side := range []struct {
		name  string
		value *Side
	}{{"server", &d.Server}, {"client", &d.Client}} {
		for i := range side.value.Methods {
			method := &side.value.Methods[i]
			method.Languages = invocationLanguages(f, side.name, method.Name, spellers)
		}
		for i := range side.value.Events {
			event := &side.value.Events[i]
			event.Languages = invocationLanguages(f, side.name, event.Name, spellers)
		}
	}
}

func invocationLanguages(f *render.Family, side, op string, spellers map[string]spi.Speller) map[string]Language {
	languages := make(map[string]Language, len(spellers))
	for name, speller := range spellers {
		languages[name] = Language{Invoke: speller.Invoke(f, side, op)}
	}
	return languages
}
