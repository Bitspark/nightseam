package auth

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Bitspark/archon/sdk/go/login"
	"github.com/Bitspark/nightseam/auth/go/grant"
)

// The bounds of the bootstrap (docs/auth/connection.md): the sizes the
// server mints, the lifetimes it keeps, the pacing it applies and the work
// it does before anyone is authenticated.
const (
	idSize           = 16
	loginNonceSize   = 32
	loginExpiresIn   = 300
	answerRetention  = 300
	collectInterval  = 5
	issuerClockSkew  = 60
	maxValidFor      = 2592000
	maxPendingLogins = 4096
)

// LoginCode is what a bootstrap transition answers instead of its result:
// the RFC 8628 codes of Archon's transport. The empty code is success.
type LoginCode string

const (
	InvalidRequest       LoginCode = "invalid_request"
	InvalidGrant         LoginCode = "invalid_grant"
	ExpiredToken         LoginCode = "expired_token"
	AuthorizationPending LoginCode = "authorization_pending"
	SlowDown             LoginCode = "slow_down"
)

// Terms is the requested grant a login carries, rendered in the tagged
// grammar the person reads verbatim before signing: action:<a>,
// delegable:<a>, scope:<s>, each list strictly ascending by bytes, then
// depth:<n> at most once and last, absent being 0.
type Terms struct {
	Actions, Delegable, Scope []string
	Depth                     uint8
}

// tags are the terms' tags, in the one order the entries may come in.
var tags = []string{"action", "delegable", "scope", "depth"}

// ParseTerms reads a login request's scope entries as terms, refusing any
// list that is not in the canonical order — one request has one binding,
// and what the CLI shows is what is signed — a delegable outside the
// actions, a depth that is not a decimal in 0..=15 or that does not come
// once and last, and every entry outside the grant's rules.
func ParseTerms(entries []string) (Terms, error) {
	var t Terms
	rank, seenDepth := 0, false
	for i, entry := range entries {
		tag, value, found := strings.Cut(entry, ":")
		if !found {
			return Terms{}, fmt.Errorf("auth: term %d %q carries no tag", i, entry)
		}
		r := -1
		for j, known := range tags {
			if tag == known {
				r = j
			}
		}
		if r < 0 {
			return Terms{}, fmt.Errorf("auth: term %d has the unknown tag %q", i, tag)
		}
		if r < rank || (r == rank && r == 3 && seenDepth) {
			return Terms{}, fmt.Errorf("auth: term %d is out of the canonical order", i)
		}
		rank = r
		switch tag {
		case "action":
			t.Actions = append(t.Actions, value)
		case "delegable":
			t.Delegable = append(t.Delegable, value)
		case "scope":
			t.Scope = append(t.Scope, value)
		case "depth":
			n, err := strconv.ParseUint(value, 10, 8)
			if err != nil || strconv.FormatUint(n, 10) != value || n > 15 {
				return Terms{}, fmt.Errorf("auth: depth %q is not a decimal in 0..=15", value)
			}
			t.Depth = uint8(n)
			seenDepth = true
		}
	}
	// The lists are held to the grant's rules by the grant's own encoder:
	// terms are valid exactly when a body can carry them.
	if _, err := grant.Encode(grant.Grant{Domain: "terms", Scope: t.Scope, Actions: t.Actions, Delegable: t.Delegable, Depth: t.Depth}); err != nil {
		return Terms{}, err
	}
	return t, nil
}

// RenderTerms spells terms in the canonical order. The depth entry is
// rendered when withDepth is set, and always when the depth is not 0,
// since an absent depth is 0.
func RenderTerms(t Terms, withDepth bool) []string {
	entries := make([]string, 0, len(t.Actions)+len(t.Delegable)+len(t.Scope)+1)
	for _, a := range t.Actions {
		entries = append(entries, "action:"+a)
	}
	for _, d := range t.Delegable {
		entries = append(entries, "delegable:"+d)
	}
	for _, s := range t.Scope {
		entries = append(entries, "scope:"+s)
	}
	if withDepth || t.Depth != 0 {
		entries = append(entries, "depth:"+strconv.Itoa(int(t.Depth)))
	}
	return entries
}

// State is where a login record is: pending until answered, answered
// until collected, collected until its retention ends.
type State string

const (
	Pending   State = "pending"
	Answered  State = "answered"
	Collected State = "collected"
)

// Answer is what the principal posted and the browser collects: the
// principal, its login proof and the chain of grant envelopes it hands the
// browser key, root first.
type Answer struct {
	Principal  [32]byte
	Possession []byte
	Authority  [][]byte
}

// Record is one pending login as the store keeps it, with the version the
// store compares on every transition. ExpiresAt bounds a pending record
// and RetainedTo an answered or collected one; Polled and LastPoll are the
// reference time of collect pacing, which moves only on a verified,
// allowed poll and moves the version not at all.
type Record struct {
	ID         []byte
	Version    int
	State      State
	Nonce      []byte
	Browser    [32]byte
	Scope      []string // the terms as the browser gave them, in canonical order
	Terms      Terms
	ValidFor   uint32
	ExpiresAt  uint64
	RetainedTo uint64
	Polled     bool
	LastPoll   uint64
	Answer     Answer
}

// Gone reports whether the record has reached its end at now: a pending
// record's expiry, an answered or collected record's retention.
func (r Record) Gone(now uint64) bool {
	switch r.State {
	case Pending:
		return now >= r.ExpiresAt
	case Answered, Collected:
		return now >= r.RetainedTo
	}
	return true
}

// request is the record as Archon's login scheme binds it.
func (r Record) request() *login.Request {
	return &login.Request{ID: r.ID, Nonce: r.Nonce, Browser: r.Browser[:], Scope: r.Scope, ValidFor: r.ValidFor}
}

// Store is the consumer's: any store that offers a versioned
// compare-and-set. Get answers the record under id. Put stores r under
// r.ID when the record there is at expectVersion — 0 being absent — and
// reports whether it did; a store two replicas share resolves the same
// login by it. Sweep drops every record that is Gone at now and answers
// the ids dropped. Count is the number of records held, swept or not, by
// which the service bounds its work before authentication.
type Store interface {
	Get(id []byte) (Record, bool)
	Put(r Record, expectVersion int) bool
	Sweep(now uint64) [][]byte
	Count() int
}

// Service is the bootstrap over a store: the root it admits chains under,
// the audience it recomputes every binding from — its own configuration,
// never a message's — and the store the records live in. Every transition
// is pure over its arguments and the store.
type Service struct {
	Root     grant.Root
	Audience string
	Store    Store
}

// Begin opens a login: id and nonce are the server's own entropy, 16 and
// 32 bytes; browser is the key that will act; scope is the terms, which
// must be in canonical order; validFor is seconds, 1..=2592000. The record
// is pending until now + 300. Refused InvalidRequest for a parameter
// outside those rules and for an id begun already and still live, and
// SlowDown once 4096 records are held.
func (s *Service) Begin(now uint64, id, nonce, browser []byte, scope []string, validFor uint32) (Record, LoginCode) {
	if s.Store == nil || len(id) != idSize || len(nonce) != loginNonceSize || len(browser) != 32 {
		return Record{}, InvalidRequest
	}
	terms, err := ParseTerms(scope)
	if err != nil {
		return Record{}, InvalidRequest
	}
	if validFor < 1 || validFor > maxValidFor {
		return Record{}, InvalidRequest
	}
	expect := 0
	if existing, ok := s.Store.Get(id); ok {
		if !existing.Gone(now) {
			return Record{}, InvalidRequest
		}
		expect = existing.Version
	}
	if s.Store.Count() >= maxPendingLogins {
		return Record{}, SlowDown
	}
	r := Record{
		ID:        append([]byte(nil), id...),
		Version:   expect + 1,
		State:     Pending,
		Nonce:     append([]byte(nil), nonce...),
		Scope:     append([]string(nil), scope...),
		Terms:     terms,
		ValidFor:  validFor,
		ExpiresAt: now + loginExpiresIn,
	}
	copy(r.Browser[:], browser)
	if !s.Store.Put(r, expect) {
		return Record{}, InvalidRequest
	}
	return r, ""
}

// Read answers a pending record; an unknown, expired, answered or
// collected one is ExpiredToken.
func (s *Service) Read(now uint64, id []byte) (Record, LoginCode) {
	if s.Store == nil {
		return Record{}, ExpiredToken
	}
	r, ok := s.Store.Get(id)
	if !ok || r.Gone(now) || r.State != Pending {
		return Record{}, ExpiredToken
	}
	return r, ""
}

// Answer stores the principal's answer to a pending login after holding
// it to the admission law — the login proof under principal for the stored
// request and the service's own audience; the chain under grant.Inspect at
// now; its leaf the browser key; a same-principal chain covering every
// requested action at every requested scope, or a delegated leaf issued by
// the principal, no wider than the request and bounded by it — and refused
// answers leave the request pending. InvalidRequest is a record that is
// not pending, a version that is not the one read (the caller re-reads),
// or a lost compare-and-set; ExpiredToken an unknown or expired record;
// InvalidGrant a refused proof or, with the grant code and hop, a refused
// chain. The server never signs: credential admission cannot mint a grant.
func (s *Service) Answer(now uint64, id []byte, principal [32]byte, proof []byte, authority [][]byte, expectVersion int) (LoginCode, *grant.Refusal) {
	if s.Store == nil {
		return ExpiredToken, nil
	}
	r, ok := s.Store.Get(id)
	if !ok || r.Gone(now) {
		return ExpiredToken, nil
	}
	if r.State != Pending || r.Version != expectVersion {
		return InvalidRequest, nil
	}
	if !login.Verify(principal[:], s.Audience, r.request(), proof) {
		return InvalidGrant, nil
	}
	if refusal := admit(s.Root, r, principal, authority, now); refusal != nil {
		if refusal.Hop < 0 {
			return InvalidGrant, nil
		}
		return InvalidGrant, refusal
	}
	kept := make([][]byte, 0, len(authority))
	for _, env := range authority {
		kept = append(kept, append([]byte(nil), env...))
	}
	r.State = Answered
	r.Answer = Answer{Principal: principal, Possession: append([]byte(nil), proof...), Authority: kept}
	r.RetainedTo = now + answerRetention
	r.Version = expectVersion + 1
	if !s.Store.Put(r, expectVersion) {
		return InvalidRequest, nil
	}
	return "", nil
}

// admit is the law over a verified answer: steps 2 to 6. A refusal with a
// hop below zero is one the grant has no code for — a chain that does not
// end at the browser key.
func admit(root grant.Root, r Record, principal [32]byte, authority [][]byte, now uint64) *grant.Refusal {
	at := grant.Time{Present: true, Now: now}
	held, refusal := grant.Inspect(root, authority, at)
	if refusal != nil {
		return refusal
	}
	if held.Subject != r.Browser {
		return &grant.Refusal{Hop: -1}
	}
	if r.Browser == principal {
		for _, action := range r.Terms.Actions {
			for _, scope := range r.Terms.Scope {
				if _, refusal := grant.Verify(root, authority, grant.Request{Domain: root.Domain, Action: action, Scope: scope}, at); refusal != nil {
					return refusal
				}
			}
		}
		return nil
	}
	hop := len(authority) - 1
	issuer, leaf, code := grant.Open(authority[hop])
	if code != "" {
		return &grant.Refusal{Code: code, Hop: hop}
	}
	if issuer != principal {
		return &grant.Refusal{Code: grant.IssuerMismatch, Hop: hop}
	}
	if !within(leaf.Actions, r.Terms.Actions) || !within(leaf.Delegable, r.Terms.Delegable) {
		return &grant.Refusal{Code: grant.WidenedActions, Hop: hop}
	}
	if !coveredBy(leaf.Scope, r.Terms.Scope) {
		return &grant.Refusal{Code: grant.WidenedScope, Hop: hop}
	}
	if leaf.Depth > r.Terms.Depth {
		return &grant.Refusal{Code: grant.WidenedDepth, Hop: hop}
	}
	if !leaf.Validity.Finite || leaf.Validity.ExpiresAt > now+uint64(r.ValidFor)+issuerClockSkew {
		return &grant.Refusal{Code: grant.WidenedValidity, Hop: hop}
	}
	return nil
}

// within reports whether every entry of inner is an entry of outer.
func within(inner, outer []string) bool {
	for _, entry := range inner {
		found := false
		for _, o := range outer {
			if o == entry {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// coveredBy reports whether every scope entry of inner is covered by one of
// outer: an entry covers itself and everything under it.
func coveredBy(inner, outer []string) bool {
	for _, entry := range inner {
		found := false
		for _, o := range outer {
			if entry == o || (len(entry) > len(o) && entry[:len(o)] == o && entry[len(o)] == '/') {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// Collect hands the browser the answer. The collect proof by the browser
// key is verified first (InvalidGrant otherwise); then a poll sooner than 5
// seconds after the last verified poll is SlowDown without moving the
// reference time. A verified collect on a pending record is
// AuthorizationPending; on an answered record it moves it to collected and
// answers; on a collected record it answers the same answer again — the
// recovery of a lost response by an authenticated retry — until the
// retention ends, after which the record is gone and a collect is
// ExpiredToken. Two collectors make one transition and receive the same
// bytes.
func (s *Service) Collect(now uint64, id, proof []byte) (Answer, LoginCode) {
	if s.Store == nil {
		return Answer{}, ExpiredToken
	}
	r, ok := s.Store.Get(id)
	if !ok || r.Gone(now) {
		return Answer{}, ExpiredToken
	}
	if !login.VerifyCollect(s.Audience, r.request(), proof) {
		return Answer{}, InvalidGrant
	}
	if r.Polled && now < r.LastPoll+collectInterval {
		return Answer{}, SlowDown
	}
	r.Polled, r.LastPoll = true, now
	switch r.State {
	case Pending:
		s.Store.Put(r, r.Version)
		return Answer{}, AuthorizationPending
	case Answered:
		expect := r.Version
		r.State = Collected
		r.Version = expect + 1
		if !s.Store.Put(r, expect) {
			again, ok := s.Store.Get(id)
			if !ok || again.State != Collected {
				return Answer{}, InvalidRequest
			}
			return again.Answer, ""
		}
		return r.Answer, ""
	case Collected:
		s.Store.Put(r, r.Version)
		return r.Answer, ""
	}
	return Answer{}, ExpiredToken
}

// Sweep drops every record that is gone at now and answers the ids
// dropped.
func (s *Service) Sweep(now uint64) [][]byte {
	if s.Store == nil {
		return nil
	}
	return s.Store.Sweep(now)
}
