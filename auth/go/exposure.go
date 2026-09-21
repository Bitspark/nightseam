package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/Bitspark/nightseam/auth/go/grant"
)

// Member is one thing an exposure can be asked to do — method:<name>,
// event:<name> or callable:<Type> — with the top-level names of its request
// payload, which are what a scope template may name.
type Member struct {
	Key    string
	Fields []string
}

// Surface is one declared side as an exposure serves it: the family and
// its declaration digest, so that a policy written for one revision cannot
// be bound to another, and its members. It is derived from the generated
// model, never written by hand.
type Surface struct {
	Family, Digest string
	Members        []Member
}

// Kind is a treatment's kind.
type Kind string

const (
	KindGuarded Kind = "guarded" // the caller's chain must grant Action over the scope the template renders, at this call, now
	KindPublic  Kind = "public"  // callable with no context at all: an explicit choice, never a default
	KindDenied  Kind = "denied"  // refused for everyone: a member the exposure does not offer
)

// Treatment is what one member costs: guarded with an action and a scope
// template, or public, or denied, the latter two carrying neither. A scope
// template is a grant scope entry with holes: {field} names a member of
// the request payload and {export}, on a callable only, the scope the
// owner recorded when it exported the reference.
type Treatment struct {
	Kind          Kind
	Action, Scope string
}

// Policy is a treatment per member, and nothing else: the consumer's,
// data, living beside the declaration and never in it.
type Policy struct {
	Family, Digest string
	Treatments     map[string]Treatment
}

// ConstructionError is why Bind constructed nothing: the code and every
// member it is about, so that one construction reports every gap.
type ConstructionError struct {
	Code    Code
	Members []string
}

func (e *ConstructionError) Error() string {
	if len(e.Members) == 0 {
		return string(e.Code)
	}
	return string(e.Code) + ": " + strings.Join(e.Members, ", ")
}

// Binding is a policy bound whole to a surface: the routes, the treatment
// of every member, and the export records of the references this exposure
// has handed out. Its methods are safe for concurrent use.
type Binding struct {
	routes  []string
	members map[string]*member
	mu      sync.Mutex
	exports map[string]export
}

// member is a declared member with its treatment held to it.
type member struct {
	callable  bool
	fields    map[string]bool
	treatment Treatment
	template  []part
}

// part is a literal or a hole of a parsed template.
type part struct {
	literal string
	hole    string
}

// export is what a reference is to this exposure: the callable member it
// was exported under and the scope the owner assigned.
type export struct {
	member, scope string
}

// Bind holds the policy to the surface whole, and constructs nothing on
// refusal: ContractMismatch when the policy names another family or
// digest; MemberUnbound when a declared member has no treatment;
// MemberUndeclared when a treatment names a member the surface does not
// declare; TemplateInvalid when a treatment is malformed — a hole naming no
// field of its member, {export} on a method or event, an action or scope
// on a public or denied treatment, a control character, an unknown kind or
// a guarded treatment without an action or a template. Omission never
// creates a usable partial exposure.
func Bind(s Surface, p Policy) (*Binding, *ConstructionError) {
	if s.Family != p.Family || s.Digest != p.Digest {
		return nil, &ConstructionError{Code: ContractMismatch}
	}
	b := &Binding{members: make(map[string]*member, len(s.Members)), exports: make(map[string]export)}
	var duplicates, unbound, undeclared, invalid []string
	for _, m := range s.Members {
		if _, seen := b.members[m.Key]; seen {
			duplicates = append(duplicates, m.Key)
			continue
		}
		fields := make(map[string]bool, len(m.Fields))
		for _, f := range m.Fields {
			fields[f] = true
		}
		b.members[m.Key] = &member{callable: strings.HasPrefix(m.Key, "callable:"), fields: fields}
		b.routes = append(b.routes, m.Key)
	}
	if len(duplicates) > 0 {
		sort.Strings(duplicates)
		return nil, &ConstructionError{Code: ContractMismatch, Members: duplicates}
	}
	for key := range p.Treatments {
		if _, declared := b.members[key]; !declared {
			undeclared = append(undeclared, key)
		}
	}
	for _, key := range b.routes {
		treatment, bound := p.Treatments[key]
		if !bound {
			unbound = append(unbound, key)
			continue
		}
		m := b.members[key]
		template, err := hold(treatment, m)
		if err != nil {
			invalid = append(invalid, key)
			continue
		}
		m.treatment, m.template = treatment, template
	}
	for _, gap := range []struct {
		code    Code
		members []string
	}{{MemberUnbound, unbound}, {MemberUndeclared, undeclared}, {TemplateInvalid, invalid}} {
		if len(gap.members) > 0 {
			sort.Strings(gap.members)
			return nil, &ConstructionError{Code: gap.code, Members: gap.members}
		}
	}
	return b, nil
}

// hold holds a treatment to its member: the kind, what it carries, and the
// template's holes against the member's fields.
func hold(t Treatment, m *member) ([]part, error) {
	switch t.Kind {
	case KindPublic, KindDenied:
		if t.Action != "" || t.Scope != "" {
			return nil, fmt.Errorf("a %s treatment carries neither an action nor a scope", t.Kind)
		}
		return nil, nil
	case KindGuarded:
	default:
		return nil, fmt.Errorf("unknown kind %q", t.Kind)
	}
	if err := entry(t.Action); err != nil {
		return nil, fmt.Errorf("action: %w", err)
	}
	if t.Scope == "" {
		return nil, errors.New("a guarded treatment carries a scope template")
	}
	template, err := parseTemplate(t.Scope)
	if err != nil {
		return nil, err
	}
	for _, p := range template {
		switch {
		case p.hole == "":
			if err := text(p.literal); err != nil {
				return nil, err
			}
		case p.hole == "export":
			if !m.callable {
				return nil, errors.New("{export} names an export record, which only a callable has")
			}
		case !m.fields[p.hole]:
			return nil, fmt.Errorf("{%s} names no field of the member", p.hole)
		}
	}
	return template, nil
}

// parseTemplate splits a template into literals and {holes}.
func parseTemplate(template string) ([]part, error) {
	var parts []part
	for template != "" {
		open := strings.IndexByte(template, '{')
		if open < 0 {
			if strings.IndexByte(template, '}') >= 0 {
				return nil, errors.New("a } closes no hole")
			}
			parts = append(parts, part{literal: template})
			break
		}
		if open > 0 {
			literal := template[:open]
			if strings.IndexByte(literal, '}') >= 0 {
				return nil, errors.New("a } closes no hole")
			}
			parts = append(parts, part{literal: literal})
		}
		close := strings.IndexByte(template[open:], '}')
		if close < 0 {
			return nil, errors.New("a { opens a hole that never closes")
		}
		hole := template[open+1 : open+close]
		if hole == "" || strings.IndexByte(hole, '{') >= 0 {
			return nil, fmt.Errorf("malformed hole {%s}", hole)
		}
		parts = append(parts, part{hole: hole})
		template = template[open+close+1:]
	}
	return parts, nil
}

// text refuses a control character (U+0000–U+001F, U+007F) and text that
// is not UTF-8.
func text(s string) error {
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

// entry holds an action or a scope entry to the grant's rule for one, by
// the grant's own encoder.
func entry(s string) error {
	_, err := grant.Encode(grant.Grant{Domain: "entry", Actions: []string{s}})
	return err
}

// Routes are exactly the declared members, each once, in the surface's
// order: every route to the implementation goes through the binding or
// not at all.
func (b *Binding) Routes() []string {
	return append([]string(nil), b.routes...)
}

// Render fills a scope template from the request, syntactically, before
// any authority is consulted: a hole is filled from the payload member it
// names — a string, or an integer spelled in decimal — and the value must
// be a scope segment, with no separator, no control character and not
// empty, so that a caller cannot traverse to a sibling or a parent by the
// text it sends; {export} is filled with the scope recorded at export. A
// missing or non-scalar field is an error like the rest. The rendered
// scope is what the grant's coverage rule then decides on.
func Render(template string, payload map[string]any, export string) (string, error) {
	parts, err := parseTemplate(template)
	if err != nil {
		return "", err
	}
	return render(parts, payload, export)
}

func render(parts []part, payload map[string]any, export string) (string, error) {
	var b strings.Builder
	for _, p := range parts {
		switch {
		case p.hole == "":
			b.WriteString(p.literal)
		case p.hole == "export":
			if export == "" {
				return "", errors.New("auth: no export record")
			}
			if err := text(export); err != nil {
				return "", fmt.Errorf("auth: export scope %w", err)
			}
			b.WriteString(export)
		default:
			value, ok := payload[p.hole]
			if !ok {
				return "", fmt.Errorf("auth: the payload has no %q", p.hole)
			}
			segment, err := scalar(value)
			if err != nil {
				return "", fmt.Errorf("auth: %q: %w", p.hole, err)
			}
			b.WriteString(segment)
		}
	}
	if b.Len() == 0 {
		return "", errors.New("auth: the template renders an empty scope")
	}
	if err := text(b.String()); err != nil {
		return "", fmt.Errorf("auth: the rendered scope %w", err)
	}
	return b.String(), nil
}

// scalar spells a payload value as a scope segment: a string as itself, an
// integer in decimal; anything else is refused, and so is a value that is
// empty, carries a separator or a control character.
func scalar(value any) (string, error) {
	var segment string
	switch v := value.(type) {
	case string:
		segment = v
	case json.Number:
		if _, err := strconv.ParseInt(v.String(), 10, 64); err != nil {
			return "", errors.New("not an integer")
		}
		segment = v.String()
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || v != math.Trunc(v) || math.Abs(v) >= 1<<53 {
			return "", errors.New("not an integer")
		}
		segment = strconv.FormatInt(int64(v), 10)
	case int:
		segment = strconv.Itoa(v)
	case int8:
		segment = strconv.FormatInt(int64(v), 10)
	case int16:
		segment = strconv.FormatInt(int64(v), 10)
	case int32:
		segment = strconv.FormatInt(int64(v), 10)
	case int64:
		segment = strconv.FormatInt(v, 10)
	case uint:
		segment = strconv.FormatUint(uint64(v), 10)
	case uint8:
		segment = strconv.FormatUint(uint64(v), 10)
	case uint16:
		segment = strconv.FormatUint(uint64(v), 10)
	case uint32:
		segment = strconv.FormatUint(uint64(v), 10)
	case uint64:
		segment = strconv.FormatUint(v, 10)
	default:
		return "", errors.New("not a scalar")
	}
	if segment == "" {
		return "", errors.New("empty")
	}
	if strings.IndexByte(segment, '/') >= 0 {
		return "", errors.New("carries a separator")
	}
	if err := text(segment); err != nil {
		return "", err
	}
	return segment, nil
}

// Decision is what dispatch yields when a call may start: the member, its
// kind, the subject — nil for a public member called with no context —
// and for a guarded member the action, the rendered scope and what the
// grant verified.
type Decision struct {
	Member        string
	Kind          Kind
	Subject       *[32]byte
	Action, Scope string
	Grant         *grant.Verified
}

// Decide is the decision at dispatch for a call to member with payload on
// a connection with context ctx at now: a member not in the surface is
// UnknownMember; denied is MemberDenied; public is admitted, with ctx's
// subject if there is one; guarded with no context is Unauthenticated,
// never a downgrade, then the scope is rendered (SelectorInvalid), then
// Call decides — the chain held, its leaf the context's subject
// (SubjectMismatch), the request covered at every hop (Denied with the
// grant code and hop). export is the scope recorded at export for a
// callable's {export}; Invoke supplies it from the record.
func (b *Binding) Decide(root grant.Root, member string, payload map[string]any, export string, ctx *Context, now grant.Time) (*Decision, *Refusal) {
	m, ok := b.members[member]
	if !ok {
		return nil, &Refusal{Code: UnknownMember}
	}
	switch m.treatment.Kind {
	case KindDenied:
		return nil, &Refusal{Code: MemberDenied}
	case KindPublic:
		d := &Decision{Member: member, Kind: KindPublic}
		if ctx != nil {
			subject := ctx.Subject
			d.Subject = &subject
		}
		return d, nil
	}
	if ctx == nil {
		return nil, &Refusal{Code: Unauthenticated}
	}
	scope, err := render(m.template, payload, export)
	if err != nil {
		return nil, &Refusal{Code: SelectorInvalid}
	}
	verified, refusal := Call(root, ctx, grant.Request{Domain: root.Domain, Action: m.treatment.Action, Scope: scope}, now)
	if refusal != nil {
		return nil, refusal
	}
	subject := ctx.Subject
	return &Decision{Member: member, Kind: KindGuarded, Subject: &subject, Action: m.treatment.Action, Scope: scope, Grant: &verified}, nil
}

// Condition is the owner's own predicate over the resolved target — the
// project exists, is not archived, is in a state that permits the action
// — evaluated inside the owner's transaction. A refusal is the owner's own
// reason.
type Condition func(scope string) (ok bool, reason string)

// Effect is the decision at the effect: the same decision re-made at the
// owner's boundary, at the effect's own time — for a guarded member, Call
// again under the same context, so that authority that expired between
// dispatch and effect refuses the effect (Denied, expired) with the
// connection still open and nothing revoked — and then the owner's own
// condition, refused with the owner's code under owner: and no effect.
func (b *Binding) Effect(root grant.Root, d *Decision, ctx *Context, now grant.Time, cond Condition) *Refusal {
	if d == nil {
		return &Refusal{Code: UnknownMember}
	}
	if _, ok := b.members[d.Member]; !ok {
		return &Refusal{Code: UnknownMember}
	}
	switch d.Kind {
	case KindPublic:
	case KindGuarded:
		if ctx == nil {
			return &Refusal{Code: Unauthenticated}
		}
		if d.Subject == nil || *d.Subject != ctx.Subject {
			return &Refusal{Code: SubjectMismatch}
		}
		if _, refusal := Call(root, ctx, grant.Request{Domain: root.Domain, Action: d.Action, Scope: d.Scope}, now); refusal != nil {
			return refusal
		}
	default:
		return &Refusal{Code: MemberDenied}
	}
	if cond != nil {
		if ok, reason := cond(d.Scope); !ok {
			return &Refusal{Code: Code(OwnerPrefix + reason)}
		}
	}
	return nil
}

// Export records what a reference is to this exposure: the callable member
// it is invoked under and the scope the owner assigns, a grant scope
// entry. A reference exported again under the same member and scope is
// the same record; under another it is an error, as is a member that is
// not a declared callable of this surface.
func (b *Binding) Export(ref, member, scope string) error {
	if ref == "" {
		return errors.New("auth: no reference")
	}
	m, ok := b.members[member]
	if !ok {
		return fmt.Errorf("auth: %q is not a declared member", member)
	}
	if !m.callable {
		return fmt.Errorf("auth: %q is not a callable", member)
	}
	if err := entry(scope); err != nil {
		return fmt.Errorf("auth: scope: %w", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if existing, ok := b.exports[ref]; ok && existing != (export{member, scope}) {
		return fmt.Errorf("auth: %q is already exported under %s at %s", ref, existing.member, existing.scope)
	}
	b.exports[ref] = export{member, scope}
	return nil
}

// Invoke is the decision for an invocation of an exported reference,
// against its export record, under the invoking connection's context, at
// the invocation's time; a reference this exposure did not export is
// ReferenceUnknown to it.
func (b *Binding) Invoke(root grant.Root, ref string, payload map[string]any, ctx *Context, now grant.Time) (*Decision, *Refusal) {
	b.mu.Lock()
	record, ok := b.exports[ref]
	b.mu.Unlock()
	if !ok {
		return nil, &Refusal{Code: ReferenceUnknown}
	}
	return b.Decide(root, record.member, payload, record.scope, ctx, now)
}

// Recipient is one connection an event is disclosed toward, by name, with
// its context.
type Recipient struct {
	Name string
	Ctx  *Context
}

// Delivery is one recipient's outcome: delivered where Refused is nil,
// dropped where it is not — and a dropped recipient is not told.
type Delivery struct {
	Recipient string
	Refused   *Refusal
}

// Emit decides the emission of event — a member event:<name> — with data
// per recipient, at emission time, against that recipient's context, in
// the recipients' order.
func (b *Binding) Emit(root grant.Root, event string, data map[string]any, to []Recipient, now grant.Time) []Delivery {
	deliveries := make([]Delivery, 0, len(to))
	for _, r := range to {
		var refused *Refusal
		if !strings.HasPrefix(event, "event:") {
			refused = &Refusal{Code: UnknownMember}
		} else {
			_, refused = b.Decide(root, event, data, "", r.Ctx, now)
		}
		deliveries = append(deliveries, Delivery{Recipient: r.Name, Refused: refused})
	}
	return deliveries
}
