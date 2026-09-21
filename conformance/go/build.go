package conformance

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// withBuildDeadline gives one recipe's commands their own bounded window.
// Call it after preparation so rendering and earlier languages cannot spend
// this build's time. Both runtime and generated testees use the same bound.
func withBuildDeadline(build func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return build(ctx)
}

// buildTestee records build failures before applying the tier gate. An absent
// testee skips its scenarios; unrelated pairings still run, including when
// this build has already made the required or nightly job fail.
func (s *Suite) buildTestee(t *testing.T, recipe Recipe, places Places, generated bool) bool {
	t.Helper()
	err := withBuildDeadline(func(ctx context.Context) error { return recipe.RunBuild(ctx, places, generated) })
	if err == nil {
		return true
	}
	kind := "runtime"
	if generated {
		kind = "generated"
	}
	reason := buildDiagnostic(err)
	if s.unavailable == nil {
		s.unavailable = map[string]map[string]string{}
	}
	if s.unavailable[recipe.Language] == nil {
		s.unavailable[recipe.Language] = map[string]string{}
	}
	s.unavailable[recipe.Language][kind] = reason
	s.Matrix.recordAbsent(recipe.Language, kind, reason)
	tier := s.Profiles.Tiers[fmt.Sprint(s.Profiles.Languages[recipe.Language].Tier)]
	if os.Getenv("NIGHTSEAM_MATRIX") != "" || tier.OnFailure == "stop" {
		t.Errorf("the %s %s testee is absent — build failed:\n%s", recipe.Language, kind, reason)
	} else {
		t.Logf("the %s %s testee is absent — build failed (provisional):\n%s", recipe.Language, kind, reason)
	}
	return false
}

func (s *Suite) buildUnavailable(language string, generated bool) string {
	for _, kind := range []string{"runtime", "generated"} {
		if kind == "generated" && !generated {
			continue
		}
		if s.unavailable[language][kind] != "" {
			return fmt.Sprintf("the %s %s testee is absent — build failed; see the matrix build reason", language, kind)
		}
	}
	return ""
}

// Retain the command and exit status, plus the last twenty output lines. A
// compiler emitting one enormous line must not inflate the matrix or summary.
func buildDiagnostic(err error) string {
	first, output, _ := strings.Cut(strings.TrimSpace(err.Error()), "\n")
	if len(first) > 1024 {
		first = strings.ToValidUTF8(first[:512], "") + "…" + strings.ToValidUTF8(first[len(first)-512:], "")
	}
	lines := strings.Split(output, "\n")
	if len(lines) > 20 {
		lines = append([]string{"…"}, lines[len(lines)-20:]...)
	}
	tail := strings.Join(lines, "\n")
	if len(tail) > 6144 {
		tail = "…" + strings.ToValidUTF8(tail[len(tail)-6144:], "")
	}
	return strings.TrimSpace(first + "\n" + tail)
}
