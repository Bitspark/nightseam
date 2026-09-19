package doc

import (
	"testing"

	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/pattern"
)

func TestPatternExamplesAreWitnessesInTheActualDialect(t *testing.T) {
	for _, source := range []string{`^[^@]+@[^@]+$`, `^https://[A-Za-z0-9.-]+/[^ ]*$`, `^[0-9]{4}$`, `^(cat|dog)$`, `^\s$`, `^😀$`, `^a\b$`, `^[^\d\s]+$`} {
		t.Run(source, func(t *testing.T) {
			value, ok := patternExample(source, nil)
			if !ok {
				t.Fatal("no example")
			}
			matcher, err := pattern.Compile(source)
			if err != nil || !matcher.MatchString(value) {
				t.Fatalf("%q is not admitted: %v", value, err)
			}
			if again, yes := patternExample(source, nil); !yes || again != value {
				t.Fatal("nondeterministic example")
			}
		})
	}
	lo, hi := 4, 4
	if value, ok := patternExample(`^a+$`, &model.Length{Min: &lo, Max: &hi}); !ok || value != "aaaa" {
		t.Fatalf("length-constrained example: %q %v", value, ok)
	}
	if _, ok := patternExample(`a^`, nil); ok {
		t.Fatal("impossible pattern received a witness")
	}
	if _, ok := patternExample(`^a{1000000000}$`, nil); ok {
		t.Fatal("unbounded synthesis")
	}
}
