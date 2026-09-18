package conformance

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Recipe is a language's testee.json: how its testee is built and run, and
// its generated-code testee beside it. Placeholders in any string —
// {self}, {checkout}, {rendered}, {out}, {exe} — are filled when it runs.
type Recipe struct {
	Language   string    `json:"language"`
	Toolchains []string  `json:"toolchains"`
	Build      []Command `json:"build"`
	Run        Command   `json:"run"`
	Generated  *struct {
		Rendered string    `json:"rendered"`
		Build    []Command `json:"build"`
		Run      Command   `json:"run"`
	} `json:"generated"`
	// Dir is where testee.json lies: {self}.
	Dir string `json:"-"`
}

// Command is one process to run: its argv, where, and what environment
// beyond the runner's.
type Command struct {
	Argv []string          `json:"argv"`
	Cwd  string            `json:"cwd"`
	Env  map[string]string `json:"env"`
}

// Recipes finds every language that joined the suite: each
// conformance/<lang>/testee.json, by language name.
func Recipes(root string) (map[string]Recipe, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	recipes := map[string]Recipe{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		file := filepath.Join(root, entry.Name(), "testee.json")
		data, err := os.ReadFile(file)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var recipe Recipe
		if err := json.Unmarshal(data, &recipe); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		if recipe.Language == "" || len(recipe.Run.Argv) == 0 {
			return nil, fmt.Errorf("%s: a recipe names its language and how its testee runs", file)
		}
		recipe.Dir = filepath.Join(root, entry.Name())
		recipes[recipe.Language] = recipe
	}
	return recipes, nil
}

// Places are what the placeholders of a recipe stand for in one run.
type Places struct {
	Checkout string
	Out      string
	Rendered string
}

func (r Recipe) fill(s string, p Places) string {
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	return strings.NewReplacer("{self}", r.Dir, "{checkout}", p.Checkout, "{out}", p.Out, "{rendered}", p.Rendered, "{exe}", exe).Replace(s)
}

func (r Recipe) command(ctx context.Context, c Command, p Places) *exec.Cmd {
	argv := make([]string, len(c.Argv))
	for i, a := range c.Argv {
		argv[i] = r.fill(a, p)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = r.Dir
	if c.Cwd != "" {
		cmd.Dir = r.fill(c.Cwd, p)
	}
	env := os.Environ()
	keys := make([]string, 0, len(c.Env))
	for key := range c.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		env = append(env, key+"="+r.fill(c.Env[key], p))
	}
	cmd.Env = env
	return cmd
}

// CheckToolchains reports the first of the recipe's toolchains not on the path.
func (r Recipe) CheckToolchains() error {
	for _, program := range r.Toolchains {
		if _, err := exec.LookPath(program); err != nil {
			return fmt.Errorf("the %s testee needs %s on the path: %w", r.Language, program, err)
		}
	}
	return nil
}

// RunBuild runs the recipe's build commands, in order, once.
func (r Recipe) RunBuild(ctx context.Context, p Places, generated bool) error {
	commands := r.Build
	if generated {
		if r.Generated == nil {
			return fmt.Errorf("the %s recipe has no generated testee", r.Language)
		}
		commands = r.Generated.Build
	}
	for _, c := range commands {
		cmd := r.command(ctx, c, p)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %v: %v\n%s", r.Language, cmd.Args, err, output)
		}
	}
	return nil
}

// Testee is one running testee process under the runner's control.
type Testee struct {
	Language string
	Hello    Hello
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   *bufio.Reader
	stderr   *transcript
	next     int
	mu       sync.Mutex
	dead     error
}

// Hello is what a testee answers hello with.
type Hello struct {
	Driver   int      `json:"driver"`
	Language string   `json:"language"`
	Layers   []string `json:"layers"`
	Features []string `json:"features"`
}

// Has reports whether the testee answered hello with the layer or feature.
func (h Hello) Has(need string) bool {
	for _, l := range h.Layers {
		if l == need {
			return true
		}
	}
	for _, f := range h.Features {
		if f == need {
			return true
		}
	}
	return false
}

// Answer is what a testee answered one request with.
type Answer struct {
	OK    any
	Error *DriverError
}

// DriverError is an answer that is an error: the protocol's own code, or
// what the remote answered with. Members is the whole error as answered —
// code, message, and whatever else the op says an error of its carries.
type DriverError struct {
	Code    string
	Message string
	Members map[string]any
}

func (e *DriverError) Error() string {
	if e.Message == "" {
		return e.Code
	}
	return e.Code + ": " + e.Message
}

// Start runs the recipe's testee and greets it.
func Start(ctx context.Context, r Recipe, p Places, generated bool) (*Testee, error) {
	run := r.Run
	if generated {
		if r.Generated == nil {
			return nil, fmt.Errorf("the %s recipe has no generated testee", r.Language)
		}
		run = r.Generated.Run
	}
	cmd := r.command(ctx, run, p)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	errs := &transcript{}
	cmd.Stderr = errs
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start the %s testee: %w", r.Language, err)
	}
	t := &Testee{Language: r.Language, cmd: cmd, stdin: stdin, stdout: bufio.NewReaderSize(stdout, 1<<20), stderr: errs}
	answer, err := t.Request(ctx, "hello", nil, 10*time.Second)
	if err != nil {
		t.Kill()
		return nil, fmt.Errorf("the %s testee did not answer hello: %w\n%s", r.Language, err, errs.String())
	}
	data, _ := json.Marshal(answer.OK)
	if err := json.Unmarshal(data, &t.Hello); err != nil || t.Hello.Driver != 1 {
		t.Kill()
		return nil, fmt.Errorf("the %s testee answered hello with %s", r.Language, data)
	}
	return t, nil
}

// Request sends one op with its arguments and waits for its answer, or
// for the deadline, after which the testee is dead to the runner.
func (t *Testee) Request(ctx context.Context, op string, args map[string]any, within time.Duration) (Answer, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.dead != nil {
		return Answer{}, t.dead
	}
	t.next++
	request := map[string]any{"id": t.next, "op": op}
	for key, value := range args {
		request[key] = value
	}
	line, err := json.Marshal(request)
	if err != nil {
		return Answer{}, err
	}
	if _, err := t.stdin.Write(append(line, '\n')); err != nil {
		t.dead = fmt.Errorf("the %s testee stopped reading: %w", t.Language, err)
		return Answer{}, t.dead
	}
	type read struct {
		line []byte
		err  error
	}
	done := make(chan read, 1)
	go func() {
		l, err := t.stdout.ReadBytes('\n')
		done <- read{l, err}
	}()
	var r read
	select {
	case r = <-done:
	case <-time.After(within):
		t.dead = fmt.Errorf("the %s testee did not answer %s within %s", t.Language, op, within)
		return Answer{}, t.dead
	case <-ctx.Done():
		t.dead = ctx.Err()
		return Answer{}, t.dead
	}
	if r.err != nil {
		t.dead = fmt.Errorf("the %s testee stopped answering: %w", t.Language, r.err)
		return Answer{}, t.dead
	}
	var envelope struct {
		ID    int             `json:"id"`
		OK    json.RawMessage `json:"ok"`
		Error json.RawMessage `json:"error"`
	}
	decoder := json.NewDecoder(bytes.NewReader(r.line))
	decoder.UseNumber()
	if err := decoder.Decode(&envelope); err != nil {
		t.dead = fmt.Errorf("the %s testee wrote a line that is not an answer: %q", t.Language, bytes.TrimSpace(r.line))
		return Answer{}, t.dead
	}
	if envelope.ID != t.next {
		t.dead = fmt.Errorf("the %s testee answered %d to request %d", t.Language, envelope.ID, t.next)
		return Answer{}, t.dead
	}
	if len(envelope.Error) != 0 {
		members, err := decode(envelope.Error)
		object, ok := members.(map[string]any)
		if err != nil || !ok {
			t.dead = fmt.Errorf("the %s testee answered an error that is not an object: %s", t.Language, envelope.Error)
			return Answer{}, t.dead
		}
		code, _ := object["code"].(string)
		message, _ := object["message"].(string)
		if code == "" {
			t.dead = fmt.Errorf("the %s testee answered an error without a code: %s", t.Language, envelope.Error)
			return Answer{}, t.dead
		}
		return Answer{Error: &DriverError{Code: code, Message: message, Members: object}}, nil
	}
	if len(envelope.OK) == 0 {
		return Answer{OK: map[string]any{}}, nil
	}
	ok, err := decode(envelope.OK)
	if err != nil {
		t.dead = fmt.Errorf("the %s testee answered with something that is not JSON: %w", t.Language, err)
		return Answer{}, t.dead
	}
	return Answer{OK: ok}, nil
}

// Reset asks the testee to forget everything between scenarios.
func (t *Testee) Reset(ctx context.Context) error {
	answer, err := t.Request(ctx, "reset", nil, 10*time.Second)
	if err != nil {
		return err
	}
	if answer.Error != nil {
		return fmt.Errorf("the %s testee did not reset: %v", t.Language, answer.Error)
	}
	t.stderr.truncate()
	return nil
}

// Stop says goodbye and waits for the testee to exit.
func (t *Testee) Stop() error {
	if t.dead == nil {
		if _, err := t.Request(context.Background(), "bye", nil, 10*time.Second); err == nil {
			_ = t.stdin.Close()
			done := make(chan error, 1)
			go func() { done <- t.cmd.Wait() }()
			select {
			case err := <-done:
				return err
			case <-time.After(10 * time.Second):
			}
		}
	}
	t.Kill()
	return nil
}

// Kill ends the testee without ceremony.
func (t *Testee) Kill() {
	if t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
		_ = t.cmd.Wait()
	}
	if t.dead == nil {
		t.dead = errors.New("the testee was killed")
	}
}

// Dead reports why the testee is no longer usable, or nil.
func (t *Testee) Dead() error { return t.dead }

// Stderr is what the testee wrote on stderr since its last reset.
func (t *Testee) Stderr() string { return t.stderr.String() }

// transcript keeps a process's stderr, from several goroutines.
type transcript struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (tr *transcript) Write(p []byte) (int, error) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.buf.Write(p)
}

func (tr *transcript) String() string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.buf.String()
}

func (tr *transcript) truncate() {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.buf.Reset()
}
