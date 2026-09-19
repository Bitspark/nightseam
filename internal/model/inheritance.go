package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Bitspark/nightseam/internal/diag"
)

// Inheritance names a base and the arguments explicitly bound at this
// edge. A nil With is a bare reference; an empty non-nil With is still an
// application. A type names Type or family.Type; a side names a family.
type Inheritance struct {
	Name string
	With map[string]Filler
	At   diag.Location
}

func (e Inheritance) Applied() bool { return e.With != nil }

// Expression is a type inheritance edge as the language's one reference
// or application expression. A side's family name is not a type expression.
func (e Inheritance) Expression() TypeExpr {
	family, name, qualified := strings.Cut(e.Name, ".")
	if !qualified {
		family, name = "", e.Name
	}
	if e.Applied() {
		return Apply{Family: family, Name: name, With: e.With}
	}
	if family != "" {
		return Imported{Family: family, Name: name}
	}
	return Named{Name: name}
}

func (e Inheritance) MarshalJSON() ([]byte, error) {
	if !e.Applied() {
		return json.Marshal(e.Name)
	}
	return json.Marshal(struct {
		Apply string            `json:"apply"`
		With  map[string]Filler `json:"with"`
	}{e.Name, e.With})
}

func decodeInheritance(raw json.RawMessage, at diag.Location) (Inheritance, error) {
	e := Inheritance{At: at}
	if err := json.Unmarshal(raw, &e.Name); err == nil && e.Name != "" {
		return e, nil
	}
	var wire struct {
		Apply string                     `json:"apply"`
		With  map[string]json.RawMessage `json:"with"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return e, fmt.Errorf("%s: an inherited base is a name or an application: %w", at, err)
	}
	if wire.Apply == "" || wire.With == nil {
		return e, fmt.Errorf("%s: an inherited application needs apply and with", at)
	}
	e.Name, e.With = wire.Apply, map[string]Filler{}
	for _, name := range sortedRaw(wire.With) {
		filler, err := decodeFiller(wire.With[name], at.Sub("with", name))
		if err != nil {
			return e, fmt.Errorf("%s: %w", at.Sub("with", name), err)
		}
		e.With[name] = filler
	}
	return e, nil
}

func decodeBases(raw []json.RawMessage, at diag.Location) ([]Inheritance, error) {
	var bases []Inheritance
	for i, entry := range raw {
		base, err := decodeInheritance(entry, at.Sub("extends", i))
		if err != nil {
			return nil, err
		}
		bases = append(bases, base)
	}
	return bases, nil
}

func rewriteBases(bases []Inheritance, fn func(TypeExpr) TypeExpr) []Inheritance {
	out := append([]Inheritance(nil), bases...)
	for i, base := range out {
		if base.With == nil {
			continue
		}
		out[i].With = map[string]Filler{}
		for name, filler := range base.With {
			filler.Type = Rewrite(filler.Type, fn)
			out[i].With[name] = filler
		}
	}
	return out
}
