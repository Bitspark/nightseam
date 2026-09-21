package conformance

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The descendant owns the same output pipe as its parent and keeps doing work
// until killed. Its finite fallback bounds this regression even before the fix.
func TestBuildProcessHelper(t *testing.T) {
	mode := os.Getenv("NIGHTSEAM_BUILD_PROCESS_HELPER")
	if mode == "" {
		return
	}
	dir := os.Getenv("NIGHTSEAM_BUILD_PROCESS_DIRECTORY")
	if mode == "child" {
		_ = os.WriteFile(filepath.Join(dir, "child"), []byte(strconv.Itoa(os.Getpid())), 0o600)
		until := time.Now().Add(6 * time.Second)
		for n := 0; time.Now().Before(until); n++ {
			_ = os.WriteFile(filepath.Join(dir, "work"), []byte(strconv.Itoa(n)), 0o600)
			time.Sleep(10 * time.Millisecond)
		}
		os.Exit(0)
	}
	if mode == "parent" || mode == "failed-parent" || mode == "successful-parent" {
		executable, _ := os.Executable()
		child := exec.Command(executable, "-test.run=^TestBuildProcessHelper$")
		child.Env = append(os.Environ(), "NIGHTSEAM_BUILD_PROCESS_HELPER=child")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(9)
		}
		for {
			if _, err := os.Stat(filepath.Join(dir, "work")); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		fmt.Println("build parent started its child")
		if mode == "failed-parent" {
			fmt.Fprintln(os.Stderr, "build refused")
			os.Exit(7)
		}
		if mode == "successful-parent" {
			os.Exit(0)
		}
		time.Sleep(8 * time.Second)
		os.Exit(0)
	}
	if mode == "success" {
		cwd, _ := os.Getwd()
		if cwd != dir || os.Args[len(os.Args)-1] != "space and λ" {
			fmt.Fprintf(os.Stderr, "lost directory/argument: %q %q", cwd, os.Args)
			os.Exit(8)
		}
		_ = os.WriteFile(filepath.Join(dir, "success"), []byte("built"), 0o600)
		os.Exit(0)
	}
	os.Exit(10)
}

func processRecipe(t *testing.T, mode string) (Recipe, string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Cleanup(func() {
		// Baseline failures must not strand their fixture. Only this helper's
		// recorded PID is eligible, and the child has its own short fallback.
		data, err := os.ReadFile(filepath.Join(dir, "child"))
		if err == nil {
			pid, _ := strconv.Atoi(string(data))
			if process, err := os.FindProcess(pid); err == nil {
				_ = process.Kill()
				_ = process.Release()
			}
		}
	})
	return Recipe{Language: "process-test", Build: []Command{{
		Argv: []string{executable, "-test.run=^TestBuildProcessHelper$", "--", "space and λ"},
		Cwd:  dir,
		Env:  map[string]string{"NIGHTSEAM_BUILD_PROCESS_HELPER": mode, "NIGHTSEAM_BUILD_PROCESS_DIRECTORY": dir},
	}}}, dir
}

func TestBuildProcessesEndTogether(t *testing.T) {
	for _, mode := range []string{"cancel", "deadline", "failed-parent", "successful-parent"} {
		t.Run(mode, func(t *testing.T) {
			helper := mode
			if mode == "cancel" || mode == "deadline" {
				helper = "parent"
			}
			recipe, dir := processRecipe(t, helper)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			finished := make(chan error, 1)
			started := time.Now()
			go func() { finished <- recipe.RunBuild(ctx, Places{}, false) }()
			for {
				if _, err := os.Stat(filepath.Join(dir, "work")); err == nil {
					break
				}
				select {
				case err := <-finished:
					t.Fatalf("build stopped before starting its descendant: %v", err)
				case <-ctx.Done():
					t.Fatal("helper did not start its descendant within two seconds")
				case <-time.After(10 * time.Millisecond):
				}
			}
			if mode == "cancel" {
				cancel()
			}
			if mode == "deadline" {
				<-ctx.Done()
			}
			select {
			case err := <-finished:
				if mode == "successful-parent" && err != nil {
					t.Fatalf("successful parent: %v", err)
				}
				if mode != "successful-parent" && err == nil {
					t.Fatal("failed or cancelled build succeeded")
				}
				if mode == "failed-parent" && (!strings.Contains(err.Error(), "build refused") || !strings.Contains(err.Error(), "exit status 7")) {
					t.Fatalf("lost failure status/output: %v", err)
				}
			case <-time.After(1500 * time.Millisecond):
				t.Errorf("recipe still waiting for descendant output after %s", time.Since(started))
				cancel()
				<-finished
			}
			// Killing the parent or merely closing the read pipe is insufficient:
			// its descendant must stop doing work as well.
			time.Sleep(100 * time.Millisecond)
			before, _ := os.ReadFile(filepath.Join(dir, "work"))
			time.Sleep(150 * time.Millisecond)
			after, _ := os.ReadFile(filepath.Join(dir, "work"))
			if string(before) != string(after) {
				t.Fatal("build descendant is still working after RunBuild returned")
			}
		})
	}
}

func TestBuildProcessSuccess(t *testing.T) {
	recipe, dir := processRecipe(t, "success")
	if err := recipe.RunBuild(context.Background(), Places{}, false); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "success")); err != nil || string(data) != "built" {
		t.Fatalf("success result: %q, %v", data, err)
	}
}
