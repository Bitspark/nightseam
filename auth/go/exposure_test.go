package auth_test

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/Bitspark/nightseam/auth/go"
)

// exposureTable is auth-exposure.json: the grant table's keys and
// envelopes plus a service key, named contexts, the two surfaces and the
// eight named policies.
type exposureTable struct {
	table
	Contexts map[string]struct {
		Subject string   `json:"subject"`
		Chain   []string `json:"chain"`
	} `json:"contexts"`
	Surfaces map[string]struct {
		Family  string `json:"family"`
		Digest  string `json:"digest"`
		Members []struct {
			Key    string   `json:"key"`
			Fields []string `json:"fields"`
		} `json:"members"`
	} `json:"surfaces"`
	Policies map[string]struct {
		Family     string `json:"family"`
		Digest     string `json:"digest"`
		Treatments map[string]struct {
			Kind   string `json:"kind"`
			Action string `json:"action"`
			Scope  string `json:"scope"`
		} `json:"treatments"`
	} `json:"policies"`
}

func (tb exposureTable) context(t *testing.T, name string) *auth.Context {
	t.Helper()
	if name == "" {
		return nil
	}
	c, ok := tb.Contexts[name]
	if !ok {
		t.Fatalf("no context %q", name)
	}
	return &auth.Context{Subject: tb.pubkey(t, c.Subject), Chain: tb.chain(t, c.Chain)}
}

func (tb exposureTable) policy(t *testing.T, name string) auth.Policy {
	t.Helper()
	p, ok := tb.Policies[name]
	if !ok {
		t.Fatalf("no policy %q", name)
	}
	policy := auth.Policy{Family: p.Family, Digest: p.Digest, Treatments: make(map[string]auth.Treatment, len(p.Treatments))}
	for key, treatment := range p.Treatments {
		policy.Treatments[key] = auth.Treatment{Kind: auth.Kind(treatment.Kind), Action: treatment.Action, Scope: treatment.Scope}
	}
	return policy
}

// surface is the surface a policy is bound to: the one of its family.
func (tb exposureTable) surface(t *testing.T, family string) auth.Surface {
	t.Helper()
	s, ok := tb.Surfaces[family]
	if !ok {
		t.Fatalf("no surface %q", family)
	}
	surface := auth.Surface{Family: s.Family, Digest: s.Digest}
	for _, m := range s.Members {
		surface.Members = append(surface.Members, auth.Member{Key: m.Key, Fields: m.Fields})
	}
	return surface
}

func (tb exposureTable) bind(t *testing.T, policy string) (*auth.Binding, *auth.ConstructionError) {
	t.Helper()
	p := tb.policy(t, policy)
	return auth.Bind(tb.surface(t, p.Family), p)
}

// jsonDecision is a decision as the table spells it: the action and scope
// only for a guarded member, the subject only where there is a context.
type jsonDecision struct {
	Action  *string `json:"action"`
	Kind    string  `json:"kind"`
	Member  string  `json:"member"`
	Scope   *string `json:"scope"`
	Subject *string `json:"subject"`
}

func (tb exposureTable) expectDecision(t *testing.T, want jsonDecision, got *auth.Decision, refusal *auth.Refusal) {
	t.Helper()
	if refusal != nil {
		t.Fatalf("refused %s, want a decision", describeRefusal(refusal))
	}
	if got == nil {
		t.Fatal("no decision and no refusal")
	}
	if string(got.Kind) != want.Kind || got.Member != want.Member {
		t.Fatalf("decided %s %s, want %s %s", got.Kind, got.Member, want.Kind, want.Member)
	}
	if (want.Action == nil) != (got.Action == "") || (want.Action != nil && *want.Action != got.Action) {
		t.Fatalf("decided action %q, want %v", got.Action, want.Action)
	}
	if (want.Scope == nil) != (got.Scope == "") || (want.Scope != nil && *want.Scope != got.Scope) {
		t.Fatalf("decided scope %q, want %v", got.Scope, want.Scope)
	}
	if (want.Subject == nil) != (got.Subject == nil) || (want.Subject != nil && *got.Subject != tb.pubkey(t, *want.Subject)) {
		t.Fatalf("decided subject %v, want %v", got.Subject, want.Subject)
	}
	if got.Kind == auth.KindGuarded && (got.Grant == nil || got.Grant.Subject != *got.Subject) {
		t.Fatalf("a guarded decision carries %v as what the grant verified", got.Grant)
	}
	if got.Kind != auth.KindGuarded && got.Grant != nil {
		t.Fatalf("a %s decision carries a verified grant", got.Kind)
	}
}

// TestExposureTable reproduces every case of auth-exposure.json: the count
// of cases run is the count of cases in the file; construction, rendering
// and the scripts are held step by step, the decision at the effect
// included.
func TestExposureTable(t *testing.T) {
	var tb exposureTable
	loadJSON(t, "auth-exposure.json", &tb)
	const want = 35
	if len(tb.Cases) != want {
		t.Fatalf("%d cases in the table, the packet states %d", len(tb.Cases), want)
	}
	ran := 0
	for _, raw := range tb.Cases {
		family, id := head(t, raw)
		if t.Run(id, func(t *testing.T) {
			switch family {
			case "exposure_bind":
				tb.runBind(t, raw)
			case "exposure_template":
				runTemplate(t, raw)
			case "exposure":
				tb.runScript(t, raw)
			default:
				t.Fatalf("unknown family %q", family)
			}
		}) {
			ran++
		}
	}
	if ran != want {
		t.Fatalf("%d of %d cases reproduced", ran, want)
	}
}

func (tb exposureTable) runBind(t *testing.T, raw json.RawMessage) {
	var c struct {
		Policy  string   `json:"policy"`
		Routes  []string `json:"routes"`
		Refused *struct {
			Code    string   `json:"code"`
			Members []string `json:"members"`
		} `json:"refused"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	binding, refused := tb.bind(t, c.Policy)
	if c.Refused != nil {
		if refused == nil {
			t.Fatalf("bound, want refused %s", c.Refused.Code)
		}
		if binding != nil {
			t.Fatal("a refused construction handed out a binding")
		}
		members := append([]string(nil), refused.Members...)
		sort.Strings(members)
		wantMembers := append([]string(nil), c.Refused.Members...)
		sort.Strings(wantMembers)
		if string(refused.Code) != c.Refused.Code || !sameStrings(members, wantMembers) {
			t.Fatalf("refused %s %v, want %s %v", refused.Code, refused.Members, c.Refused.Code, c.Refused.Members)
		}
		return
	}
	if refused != nil {
		t.Fatalf("refused %s %v", refused.Code, refused.Members)
	}
	routes := binding.Routes()
	sort.Strings(routes)
	wantRoutes := append([]string(nil), c.Routes...)
	sort.Strings(wantRoutes)
	if !sameStrings(routes, wantRoutes) {
		t.Fatalf("routes %v, want %v", routes, wantRoutes)
	}
}

func runTemplate(t *testing.T, raw json.RawMessage) {
	var c struct {
		Template string         `json:"template"`
		Payload  map[string]any `json:"payload"`
		Export   string         `json:"export"`
		Scope    string         `json:"scope"`
		Refused  bool           `json:"refused"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	got, err := auth.Render(c.Template, c.Payload, c.Export)
	if c.Refused {
		if err == nil {
			t.Fatalf("rendered %q, want refused", got)
		}
		return
	}
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if got != c.Scope {
		t.Fatalf("rendered %q, want %q", got, c.Scope)
	}
}

// scriptStep is one step of an exposure script; which fields a step
// carries depends on its op.
type scriptStep struct {
	Op         string            `json:"op"`
	Policy     string            `json:"policy"`
	Connection string            `json:"connection"`
	Member     string            `json:"member"`
	Route      string            `json:"route"`
	Payload    map[string]any    `json:"payload"`
	Meta       map[string]any    `json:"meta"`
	Now        json.RawMessage   `json:"now"`
	EffectAt   json.RawMessage   `json:"effect_at"`
	State      map[string]string `json:"state"`
	Ref        string            `json:"ref"`
	Scope      string            `json:"scope"`
	Recipients []string          `json:"recipients"`
	Result     json.RawMessage   `json:"result"`
	Refused    *jsonRefusal      `json:"refused"`
	Effect     json.RawMessage   `json:"effect"`
}

func (tb exposureTable) runScript(t *testing.T, raw json.RawMessage) {
	var c struct {
		Steps []scriptStep `json:"steps"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	root := tb.root(t)
	var binding *auth.Binding
	for i, step := range c.Steps {
		switch step.Op {
		case "bind":
			var refused *auth.ConstructionError
			binding, refused = tb.bind(t, step.Policy)
			if refused != nil || string(step.Result) != `"bound"` {
				t.Fatalf("step %d: bind %s: %v, want %s", i, step.Policy, refused, step.Result)
			}
		case "call", "invoke":
			if binding == nil {
				t.Fatalf("step %d: no binding", i)
			}
			ctx := tb.context(t, step.Connection)
			var decision *auth.Decision
			var refusal *auth.Refusal
			if step.Op == "call" {
				// The route and the meta are what the table pins the decision
				// as independent of: neither reaches Decide.
				decision, refusal = binding.Decide(root, step.Member, step.Payload, "", ctx, now(step.Now))
			} else {
				decision, refusal = binding.Invoke(root, step.Ref, step.Payload, ctx, now(step.Now))
			}
			if step.Refused != nil {
				expectRefusal(t, *step.Refused, refusal)
				if decision != nil {
					t.Fatalf("step %d: a refusal came with a decision", i)
				}
				continue
			}
			var want jsonDecision
			if err := json.Unmarshal(step.Result, &want); err != nil {
				t.Fatalf("step %d: %v", i, err)
			}
			tb.expectDecision(t, want, decision, refusal)
			if len(step.Effect) == 0 {
				continue
			}
			var condition auth.Condition
			if step.State != nil {
				state := step.State
				condition = func(scope string) (bool, string) {
					if reason, ok := state[scope]; ok {
						return false, reason
					}
					return true, ""
				}
			}
			effect := binding.Effect(root, decision, ctx, now(step.EffectAt), condition)
			if string(step.Effect) == `"applied"` {
				if effect != nil {
					t.Fatalf("step %d: the effect is refused %s, want applied", i, describeRefusal(effect))
				}
				continue
			}
			var want2 struct {
				Refused jsonRefusal `json:"refused"`
			}
			if err := json.Unmarshal(step.Effect, &want2); err != nil {
				t.Fatalf("step %d: %v", i, err)
			}
			expectRefusal(t, want2.Refused, effect)
		case "export":
			if binding == nil {
				t.Fatalf("step %d: no binding", i)
			}
			if err := binding.Export(step.Ref, step.Member, step.Scope); err != nil || string(step.Result) != `"exported"` {
				t.Fatalf("step %d: export: %v, want %s", i, err, step.Result)
			}
		case "emit":
			if binding == nil {
				t.Fatalf("step %d: no binding", i)
			}
			recipients := make([]auth.Recipient, 0, len(step.Recipients))
			for _, name := range step.Recipients {
				recipients = append(recipients, auth.Recipient{Name: name, Ctx: tb.context(t, name)})
			}
			deliveries := binding.Emit(root, step.Member, step.Payload, recipients, now(step.Now))
			var want map[string]json.RawMessage
			if err := json.Unmarshal(step.Result, &want); err != nil {
				t.Fatalf("step %d: %v", i, err)
			}
			if len(deliveries) != len(want) || len(deliveries) != len(recipients) {
				t.Fatalf("step %d: %d deliveries for %d recipients, want %d", i, len(deliveries), len(recipients), len(want))
			}
			for j, delivery := range deliveries {
				if delivery.Recipient != recipients[j].Name {
					t.Fatalf("step %d: delivery %d is %s's, want %s's", i, j, delivery.Recipient, recipients[j].Name)
				}
				outcome, ok := want[delivery.Recipient]
				if !ok {
					t.Fatalf("step %d: no outcome for %s", i, delivery.Recipient)
				}
				if string(outcome) == `"delivered"` {
					if delivery.Refused != nil {
						t.Fatalf("step %d: %s dropped %s, want delivered", i, delivery.Recipient, describeRefusal(delivery.Refused))
					}
					continue
				}
				var dropped struct {
					Dropped jsonRefusal `json:"dropped"`
				}
				if err := json.Unmarshal(outcome, &dropped); err != nil {
					t.Fatalf("step %d: %v", i, err)
				}
				expectRefusal(t, dropped.Dropped, delivery.Refused)
			}
		default:
			t.Fatalf("step %d: unknown op %q", i, step.Op)
		}
	}
}
