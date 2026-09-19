package runtime

// Of is what an entry point of a package generic in a family holds its type
// arguments to. Every record and enum a family's protocol package declares
// returns the package's Tag from Of, and an entry point that takes a
// parameter S drawn at several types constrains each to Of[STag]: they are
// then types of one family, or the call does not compile, whether the
// caller spells them or the compiler infers them from the handler. Go has
// no associated types; this is the pairing they would have given.
type Of[T any] interface{ Of() T }

// Opaque is the tag of Raw: no family at all.
type Opaque struct{}

// Raw fills a slot with JSON passed through unexamined, which is what a
// relay wants: it carries the bytes and satisfies Of, so a generic package
// instantiated with it compiles, and validates nothing of what it carries.
type Raw []byte

// Of is the tag of Raw: Opaque, no family at all.
func (Raw) Of() Opaque { return Opaque{} }

func (r Raw) MarshalJSON() ([]byte, error) {
	if len(r) == 0 {
		return []byte("null"), nil
	}
	return r, nil
}

func (r *Raw) UnmarshalJSON(data []byte) error {
	*r = append((*r)[:0], data...)
	return nil
}
