package grant

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unicode/utf8"
)

// Domain is the signing domain every grant envelope is sealed in. A
// signature made in any other domain, or raw, verifies as nothing here, and
// a grant signature can never be replayed as a possession proof or as any
// other envelope of the profile.
const Domain = "nightseam-grant/1"

// The bounds of docs/auth/grant.md, by which every piece of work here is
// linear in its input and refused before it grows.
const (
	version        byte = 0x01
	maxDepth            = 15
	maxChain            = 16
	maxEntries          = 64
	maxEntryBytes       = 1024
	maxBodyBytes        = 65535
	maxDomainBytes      = 255
)

// Code is a refusal's code: the closed set a program branches on. A refusal
// echoes no member of any grant; it is a code and a hop and nothing else.
type Code string

// The codes, in the order the evaluation can produce them.
const (
	Malformed          Code = "malformed"
	UnsupportedVersion Code = "unsupported_version"
	EnvelopeInvalid    Code = "envelope_invalid"
	ChainTooLong       Code = "chain_too_long"
	DomainMismatch     Code = "domain_mismatch"
	RootMismatch       Code = "root_mismatch"
	ParentMismatch     Code = "parent_mismatch"
	IssuerMismatch     Code = "issuer_mismatch"
	WidenedActions     Code = "widened_actions"
	WidenedScope       Code = "widened_scope"
	WidenedDepth       Code = "widened_depth"
	WidenedValidity    Code = "widened_validity"
	NotCovered         Code = "not_covered"
	TimeRequired       Code = "time_required"
	Expired            Code = "expired"
)

// Validity is a grant's explicit validity: unbounded, or finite with an
// absolute expiry in seconds since the Unix epoch, UTC. Nothing is an
// implicit infinity; an unbounded validity carries no expiry.
type Validity struct {
	Finite    bool
	ExpiresAt uint64
}

// Grant is the body of a grant: what a grant means. Who said it is the
// envelope's public key, which the body does not repeat.
type Grant struct {
	Domain    string    // the trust domain the scope lives in
	Subject   [32]byte  // the Ed25519 public key the grant is issued to
	Parent    *[32]byte // nil for a root grant; else the SHA-256 of the parent's whole envelope
	Scope     []string  // what the subject may reach, strictly ascending, at most 64
	Actions   []string  // what the subject may do there, the same rules
	Delegable []string  // which of those it may pass on: a subset of Actions
	Depth     uint8     // how many further hops it may pass them, 0..=15
	Validity  Validity
}

// Root is what a verifier is configured with: one public key and one trust
// domain.
type Root struct {
	Key    [32]byte
	Domain string
}

// Request is one concrete request a chain covers at every hop or covers
// nothing.
type Request struct {
	Domain, Action, Scope string
}

// Time is the decision time: absent, or a number of seconds since the Unix
// epoch, UTC. It is the caller's; nothing here reads a clock.
type Time struct {
	Present bool
	Now     uint64
}

// Verified is what a chain that holds yields: the leaf's subject, its
// depth, the chain's effective validity — the earliest finite expiry, or
// unbounded — and the hop count.
type Verified struct {
	Subject  [32]byte
	Depth    uint8
	Validity Validity
	Hops     int
}

// Refusal is a code and the index of the hop it arose at, and nothing
// else.
type Refusal struct {
	Code Code
	Hop  int
}

// Encode is the one canonical encoding of a body: version-tagged, fixed
// order, length-prefixed, big-endian. It refuses a body outside the rules
// of the packet — a domain or entry that is empty, too long, not UTF-8 or
// carrying a control character; a list out of strictly ascending byte
// order, with a duplicate, or of more than 64 entries; a delegable outside
// the actions; a depth above 15; an unbounded validity carrying an expiry;
// a body over 65535 bytes.
func Encode(g Grant) ([]byte, error) {
	if err := check(g); err != nil {
		return nil, err
	}
	body := make([]byte, 0, 2+len(g.Domain)+32+33+8+2)
	body = append(body, version)
	body = append(body, byte(len(g.Domain)))
	body = append(body, g.Domain...)
	body = append(body, g.Subject[:]...)
	if g.Parent == nil {
		body = append(body, 0x00)
	} else {
		body = append(body, 0x01)
		body = append(body, g.Parent[:]...)
	}
	body = appendList(body, g.Scope)
	body = appendList(body, g.Actions)
	body = appendList(body, g.Delegable)
	body = append(body, g.Depth)
	if g.Validity.Finite {
		body = append(body, 0x01)
		body = binary.BigEndian.AppendUint64(body, g.Validity.ExpiresAt)
	} else {
		body = append(body, 0x00)
	}
	if len(body) > maxBodyBytes {
		return nil, fmt.Errorf("grant: body is %d bytes, over the bound of %d", len(body), maxBodyBytes)
	}
	return body, nil
}

func appendList(body []byte, entries []string) []byte {
	body = binary.BigEndian.AppendUint16(body, uint16(len(entries)))
	for _, entry := range entries {
		body = binary.BigEndian.AppendUint16(body, uint16(len(entry)))
		body = append(body, entry...)
	}
	return body
}

// Decode is total: it reads a body and answers a grant or a code, never a
// panic. A first byte other than the version is UnsupportedVersion; an
// empty body, a body over 65535 bytes, a truncated field, a trailing byte,
// an unknown parent or validity kind, a list out of order or over its
// bounds, and a delegable outside the actions are Malformed.
func Decode(body []byte) (Grant, Code) {
	if len(body) == 0 {
		return Grant{}, Malformed
	}
	if body[0] != version {
		return Grant{}, UnsupportedVersion
	}
	if len(body) > maxBodyBytes {
		return Grant{}, Malformed
	}
	r := reader{rest: body[1:]}
	var g Grant
	domainLen, ok := r.u8()
	if !ok {
		return Grant{}, Malformed
	}
	domain, ok := r.take(int(domainLen))
	if !ok {
		return Grant{}, Malformed
	}
	g.Domain = string(domain)
	subject, ok := r.take(32)
	if !ok {
		return Grant{}, Malformed
	}
	copy(g.Subject[:], subject)
	parentKind, ok := r.u8()
	if !ok {
		return Grant{}, Malformed
	}
	switch parentKind {
	case 0x00:
	case 0x01:
		parent, ok := r.take(32)
		if !ok {
			return Grant{}, Malformed
		}
		g.Parent = new([32]byte)
		copy(g.Parent[:], parent)
	default:
		return Grant{}, Malformed
	}
	if g.Scope, ok = r.list(); !ok {
		return Grant{}, Malformed
	}
	if g.Actions, ok = r.list(); !ok {
		return Grant{}, Malformed
	}
	if g.Delegable, ok = r.list(); !ok {
		return Grant{}, Malformed
	}
	if g.Depth, ok = r.u8(); !ok {
		return Grant{}, Malformed
	}
	validityKind, ok := r.u8()
	if !ok {
		return Grant{}, Malformed
	}
	switch validityKind {
	case 0x00:
	case 0x01:
		expiresAt, ok := r.take(8)
		if !ok {
			return Grant{}, Malformed
		}
		g.Validity = Validity{Finite: true, ExpiresAt: binary.BigEndian.Uint64(expiresAt)}
	default:
		return Grant{}, Malformed
	}
	if len(r.rest) != 0 {
		return Grant{}, Malformed
	}
	if check(g) != nil {
		return Grant{}, Malformed
	}
	return g, ""
}

// reader consumes a body front to back; every read reports whether the
// bytes were there.
type reader struct {
	rest []byte
}

func (r *reader) u8() (byte, bool) {
	if len(r.rest) < 1 {
		return 0, false
	}
	b := r.rest[0]
	r.rest = r.rest[1:]
	return b, true
}

func (r *reader) u16() (uint16, bool) {
	if len(r.rest) < 2 {
		return 0, false
	}
	n := binary.BigEndian.Uint16(r.rest)
	r.rest = r.rest[2:]
	return n, true
}

func (r *reader) take(n int) ([]byte, bool) {
	if len(r.rest) < n {
		return nil, false
	}
	b := r.rest[:n]
	r.rest = r.rest[n:]
	return b, true
}

// list reads u16(count) ‖ (u16(len) ‖ entry)*, refusing a count or an entry
// over its bound before reading it, so the work is bounded by the packet
// and not by the input.
func (r *reader) list() ([]string, bool) {
	count, ok := r.u16()
	if !ok || count > maxEntries {
		return nil, false
	}
	if count == 0 {
		return nil, true
	}
	entries := make([]string, 0, count)
	for i := 0; i < int(count); i++ {
		n, ok := r.u16()
		if !ok || n > maxEntryBytes {
			return nil, false
		}
		entry, ok := r.take(int(n))
		if !ok {
			return nil, false
		}
		entries = append(entries, string(entry))
	}
	return entries, true
}

// check holds a grant to the rules of the body; Encode refuses what it
// refuses and Decode calls it Malformed.
func check(g Grant) error {
	if err := text(g.Domain, maxDomainBytes); err != nil {
		return fmt.Errorf("grant: domain: %w", err)
	}
	for _, list := range []struct {
		name    string
		entries []string
	}{{"scope", g.Scope}, {"actions", g.Actions}, {"delegable", g.Delegable}} {
		if err := checkList(list.entries); err != nil {
			return fmt.Errorf("grant: %s: %w", list.name, err)
		}
	}
	if !subset(g.Delegable, g.Actions) {
		return errors.New("grant: delegable is not a subset of actions")
	}
	if g.Depth > maxDepth {
		return fmt.Errorf("grant: depth %d is above the bound of %d", g.Depth, maxDepth)
	}
	if !g.Validity.Finite && g.Validity.ExpiresAt != 0 {
		return errors.New("grant: an unbounded validity carries no expiry")
	}
	return nil
}

// checkList holds a list to its rules: at most 64 entries, each 1..=1024
// bytes of UTF-8 with no control character, in strictly ascending byte
// order — canonical and duplicate-free by construction.
func checkList(entries []string) error {
	if len(entries) > maxEntries {
		return fmt.Errorf("%d entries, over the bound of %d", len(entries), maxEntries)
	}
	for i, entry := range entries {
		if err := text(entry, maxEntryBytes); err != nil {
			return fmt.Errorf("entry %d: %w", i, err)
		}
		if i > 0 && entries[i-1] >= entry {
			return fmt.Errorf("entry %d is not strictly after entry %d", i, i-1)
		}
	}
	return nil
}

// text holds a domain or an entry to the shared rule: 1..=max bytes of
// valid UTF-8 with no control character (U+0000–U+001F, U+007F).
func text(s string, max int) error {
	if len(s) == 0 || len(s) > max {
		return fmt.Errorf("%d bytes, want 1..=%d", len(s), max)
	}
	if !utf8.ValidString(s) {
		return errors.New("not valid UTF-8")
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("carries the control character U+%04X", r)
		}
	}
	return nil
}

// subset reports whether every entry of inner is an entry of outer; both
// are sorted, so one walk decides it.
func subset(inner, outer []string) bool {
	j := 0
	for _, entry := range inner {
		for j < len(outer) && outer[j] < entry {
			j++
		}
		if j == len(outer) || outer[j] != entry {
			return false
		}
	}
	return true
}

// contains reports whether entries, sorted, holds entry.
func contains(entries []string, entry string) bool {
	for _, e := range entries {
		if e == entry {
			return true
		}
		if e > entry {
			return false
		}
	}
	return false
}

// covered reports whether one of the entries covers scope: an entry is a
// path, and a/b covers a/b and everything under a/b/ — never a/bc.
func covered(scope string, entries []string) bool {
	for _, entry := range entries {
		if scope == entry {
			return true
		}
		if len(scope) > len(entry) && scope[:len(entry)] == entry && scope[len(entry)] == '/' {
			return true
		}
	}
	return false
}

// allCovered reports whether every entry of inner is covered by an entry of
// outer.
func allCovered(inner, outer []string) bool {
	for _, entry := range inner {
		if !covered(entry, outer) {
			return false
		}
	}
	return true
}
