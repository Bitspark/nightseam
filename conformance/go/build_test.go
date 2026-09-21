package conformance

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestBuildDeadlinesAreIndependent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, language := range []string{"first", "second", "generated"} {
			started := time.Now()
			var finished context.Context
			err := withBuildDeadline(func(ctx context.Context) error {
				finished = ctx
				deadline, ok := ctx.Deadline()
				if !ok || deadline.Sub(started) != 5*time.Minute {
					t.Fatalf("%s build deadline: %v, want five minutes from its own start", language, deadline)
				}
				select {
				case <-time.After(4 * time.Minute):
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
			if err != nil {
				t.Fatalf("%s build inherited an earlier deadline: %v", language, err)
			}
			if !errors.Is(finished.Err(), context.Canceled) {
				t.Fatalf("%s build context was not released: %v", language, finished.Err())
			}
		}
	})
}

func TestBuildDeadlineBoundsHungBuild(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := time.Now()
		err := withBuildDeadline(func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		})
		if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) != 5*time.Minute {
			t.Fatalf("hung build ended after %v with %v, want the five-minute deadline", time.Since(started), err)
		}
	})
}

func TestBuildDeadlinePreservesErrors(t *testing.T) {
	want := errors.New("build failed")
	if got := withBuildDeadline(func(context.Context) error { return want }); !errors.Is(got, want) {
		t.Fatalf("build failure: got %v, want %v", got, want)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	recipe := Recipe{Language: "broken", Build: []Command{{Argv: []string{executable, "-nightseam-no-such-test-flag"}}}}
	if err := withBuildDeadline(func(ctx context.Context) error { return recipe.RunBuild(ctx, Places{}, false) }); err == nil || !strings.Contains(err.Error(), "broken:") {
		t.Fatalf("failed build command was not reported: %v", err)
	}
	recipe.Toolchains = []string{"nightseam-no-such-toolchain-403"}
	if err := recipe.CheckToolchains(); err == nil || !strings.Contains(err.Error(), "needs nightseam-no-such-toolchain-403") {
		t.Fatalf("missing toolchain was not reported: %v", err)
	}
}
