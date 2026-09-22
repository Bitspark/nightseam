package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"time"
)

// Outcome is how one scenario went between two testees: passed, skipped
// with the reason, or failed at a step with everything a reader needs.
type Outcome struct {
	Skipped string
	// SkippedBy names the participant missing a capability or operation.
	// Empty means the reason concerns the pairing rather than one participant.
	SkippedBy string
	Failed    *Failure
}

// Failure is where a scenario parted from what happened.
type Failure struct {
	Step    int
	Op      string
	On      string
	Request map[string]any
	Answer  string
	Reason  string
	StderrA string
	StderrB string
}

func (f *Failure) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "step %d, %s on %s: %s\n  request: %s\n  answer:  %s", f.Step, f.Op, f.On, f.Reason, render(f.Request), f.Answer)
	if f.StderrA != "" {
		fmt.Fprintf(&b, "\n  a said on stderr:\n%s", indent(f.StderrA))
	}
	if f.StderrB != "" {
		fmt.Fprintf(&b, "\n  b said on stderr:\n%s", indent(f.StderrB))
	}
	return b.String()
}

func indent(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = "    " + l
	}
	return strings.Join(lines, "\n")
}

// Run drives one scenario over two testees, a and b, each reset first. A
// need either testee lacks skips it; an op a testee answers unsupported
// skips it; anything else that parts from the scenario fails it.
func Run(ctx context.Context, a, b *Testee, s Scenario) Outcome {
	// The runner's own steps become the testees' by what each can do, and
	// each side is then held to what its own steps ask: a language that
	// lacks a feature skips the scenario only where it would use it.
	steps, skip := expand(s, map[string]Hello{"a": a.Hello, "b": b.Hello})
	if skip != "" {
		return Outcome{Skipped: skip}
	}
	for side, needs := range SideNeeds(steps) {
		t := a
		if side == "b" {
			t = b
		}
		for _, need := range needs {
			if !t.Hello.Has(need) {
				return Outcome{Skipped: fmt.Sprintf("the %s testee, on side %s, lacks %s", t.Language, side, need), SkippedBy: t.Language}
			}
		}
	}
	s.Steps = steps
	for _, t := range []*Testee{a, b} {
		if err := t.Reset(ctx); err != nil {
			return Outcome{Failed: &Failure{Reason: err.Error(), StderrA: a.Stderr(), StderrB: b.Stderr()}}
		}
	}
	bindings := Bindings{}
	if s.Row != nil {
		bindings[s.RowAs] = s.Row
	}
	for i, step := range s.Steps {
		testee := a
		if step.On == "b" {
			testee = b
		}
		fail := func(request map[string]any, answer string, reason string) Outcome {
			return Outcome{Failed: &Failure{Step: i, Op: step.Op, On: step.On, Request: request, Answer: answer, Reason: reason, StderrA: a.Stderr(), StderrB: b.Stderr()}}
		}
		substituted, err := substitute(step.Args, bindings)
		if err != nil {
			return fail(step.Args, "", "the arguments refer to something unbound: "+err.Error())
		}
		args, _ := substituted.(map[string]any)
		if args == nil {
			args = map[string]any{}
		}
		answer, err := request(ctx, testee, step, args)
		if err != nil {
			return fail(args, "", err.Error())
		}
		// A step that repeats until its expectations match is a wait on the
		// far side of the wire — an observer told of a frame the local answer
		// preceded — and is asked again, briefly apart, until they hold or
		// the repeats run out; the last answer is then what is reported.
		if step.Repeat != nil && step.Repeat.Until == "match" {
			for n := 1; n < step.Repeat.Max && !holds(step, answer, bindings); n++ {
				select {
				case <-ctx.Done():
					return fail(args, renderAnswer(answer), ctx.Err().Error())
				case <-time.After(50 * time.Millisecond):
				}
				if answer, err = testee.Request(ctx, step.Op, args, 15*time.Second); err != nil {
					return fail(args, "", err.Error())
				}
			}
		}
		rendered := renderAnswer(answer)
		if answer.Error != nil {
			if answer.Error.Code == "unsupported" {
				return Outcome{Skipped: fmt.Sprintf("the %s testee does not support %s: %s", testee.Language, step.Op, answer.Error.Message), SkippedBy: testee.Language}
			}
			if step.ExpectError == nil {
				return fail(args, rendered, "the op failed: "+answer.Error.Error())
			}
			if err := match(step.ExpectError, answer.Error.Members, bindings); err != nil {
				return fail(args, rendered, "the error is not the one expected: "+strings.TrimPrefix(err.Error(), "the answer"))
			}
		} else {
			// A step with both expect and expect_error holds whichever kind
			// the answer is to the one written for it; with expect_error alone
			// the op must fail.
			if step.ExpectError != nil && !step.HasExpect {
				return fail(args, rendered, "expected an error "+render(step.ExpectError)+", the op succeeded")
			}
			if step.HasExpect {
				if err := match(step.Expect, answer.OK, bindings); err != nil {
					return fail(args, rendered, err.Error()+"\n  expected: "+render(step.Expect))
				}
			}
			if err := bind(step.Bind, answer.OK, bindings); err != nil {
				return fail(args, rendered, err.Error())
			}
		}
		if step.Assert != nil {
			if needle, ok := step.Assert["absent"].(string); ok && strings.Contains(rendered, needle) {
				return fail(args, rendered, fmt.Sprintf("%q appears in the answer and must not", needle))
			}
			if needle, ok := step.Assert["present"].(string); ok && !strings.Contains(rendered, needle) {
				return fail(args, rendered, fmt.Sprintf("%q does not appear in the answer and must", needle))
			}
		}
	}
	return Outcome{}
}

// holds reports whether a step's expect and assert are satisfied by an
// answer — what a repeat until match waits for. Nothing is bound here: a
// `$bind` in the expect writes as it matches, so the match is made against
// a copy, and the run binds once, from the answer that held.
func holds(step Step, answer Answer, bound Bindings) bool {
	b := maps.Clone(bound)
	if answer.Error != nil {
		return step.ExpectError != nil && match(step.ExpectError, answer.Error.Members, b) == nil
	}
	if step.HasExpect && match(step.Expect, answer.OK, b) != nil {
		return false
	}
	if step.Assert != nil {
		rendered := renderAnswer(answer)
		if needle, ok := step.Assert["absent"].(string); ok && strings.Contains(rendered, needle) {
			return false
		}
		if needle, ok := step.Assert["present"].(string); ok && !strings.Contains(rendered, needle) {
			return false
		}
	}
	return true
}

// request sends a step's op, again while a repeat says so, with a deadline
// past whatever the op itself was told to wait.
func request(ctx context.Context, t *Testee, step Step, args map[string]any) (Answer, error) {
	within := 15 * time.Second
	if w, ok := args["within_ms"].(json.Number); ok {
		if ms, err := w.Int64(); err == nil {
			within = time.Duration(ms)*time.Millisecond + 10*time.Second
		}
	}
	if step.Repeat == nil || step.Repeat.Until == "match" {
		return t.Request(ctx, step.Op, args, within)
	}
	var answer Answer
	for n := 0; n < step.Repeat.Max; n++ {
		var err error
		answer, err = t.Request(ctx, step.Op, args, within)
		if err != nil {
			return answer, err
		}
		if (answer.Error != nil) == (step.Repeat.Until == "error") {
			break
		}
	}
	return answer, nil
}

// bind records what a step said to keep: a name for the answer's handle,
// or the answer itself when it has none; or a name per member.
func bind(spec any, ok any, b Bindings) error {
	switch v := spec.(type) {
	case nil:
		return nil
	case string:
		if object, isObject := ok.(map[string]any); isObject {
			if handle, has := object["handle"]; has {
				b[v] = handle
				return nil
			}
		}
		b[v] = ok
	case map[string]any:
		object, isObject := ok.(map[string]any)
		if !isObject {
			return fmt.Errorf("bind names members of an answer that is %s", render(ok))
		}
		for member, name := range v {
			value, has := object[member]
			if !has {
				return fmt.Errorf("bind names %s, which the answer lacks: %s", member, render(ok))
			}
			b[name.(string)] = value
		}
	}
	return nil
}

func renderAnswer(a Answer) string {
	if a.Error != nil {
		return "error " + render(a.Error.Members)
	}
	return render(a.OK)
}
