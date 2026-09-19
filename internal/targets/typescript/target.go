// Package typescript renders a family as TypeScript: one client package
// with the wire types, their validator, the client class and the handler
// of what the server sends. It implements spi.Target and is named nowhere
// but where the tool is composed.
package typescript

import (
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/emit"
	"github.com/Bitspark/nightseam/internal/model"
	"github.com/Bitspark/nightseam/internal/render"
	"github.com/Bitspark/nightseam/internal/spi"
)

// Config places the generated package. Scope is the npm scope it is
// published under and is required; Runtime and Tunnel are the packages it
// binds to and RuntimeVersion their version, Nightseam's own when left
// empty; Layout says where a family's package lives, as a pattern over the
// family's name; Place puts named families elsewhere. Sibling selects how
// generated packages refer to one another: file, workspace or version.
type Config struct {
	Scope          string
	Runtime        string
	Tunnel         string
	Live           string
	RuntimeVersion string
	Layout         string
	Place          map[string]string
	Sibling        string
}

// The packages the generated one binds to unless the config names others,
// and where a family's package lives unless the layout says otherwise.
const (
	DefaultRuntime        = "@nightseam/runtime"
	DefaultTunnel         = "@nightseam/tunnel"
	DefaultLive           = "@nightseam/live"
	DefaultRuntimeVersion = "0.4.0"
	DefaultLayout         = "api/ts/{family}-client"
	DefaultSibling        = "file"
)

// Name is the target's name: what its override file is called after.
const Name = "typescript"

// New returns the TypeScript target with its config.
func New(c Config) spi.Target { return &target{c.settled()} }

type target struct{ config Config }

func (c Config) settled() Config {
	if c.Runtime == "" {
		c.Runtime = DefaultRuntime
	}
	if c.Tunnel == "" {
		c.Tunnel = DefaultTunnel
	}
	if c.Live == "" {
		c.Live = DefaultLive
	}
	if c.RuntimeVersion == "" {
		c.RuntimeVersion = DefaultRuntimeVersion
	}
	if c.Layout == "" {
		c.Layout = DefaultLayout
	}
	if c.Sibling == "" {
		c.Sibling = DefaultSibling
	}
	return c
}

var scopePattern = regexp.MustCompile(`^@[a-z0-9][a-z0-9._-]*$`)
var packagePattern = regexp.MustCompile(`^(?:@[a-z0-9][a-z0-9._-]*/)?[a-z0-9][a-z0-9._-]*$`)
var pathPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_./{}-]*$`)

// Validate reports what is wrong with the config.
func (c Config) Validate() error {
	c = c.settled()
	if c.Sibling != "file" && c.Sibling != "workspace" && c.Sibling != "version" {
		return fmt.Errorf("invalid TypeScript sibling resolution %q: use file, workspace or version", c.Sibling)
	}
	if !scopePattern.MatchString(c.Scope) {
		return fmt.Errorf("an npm scope is required to name the generated packages, @scope: %q", c.Scope)
	}
	for _, pkg := range []string{c.Runtime, c.Tunnel} {
		if !packagePattern.MatchString(pkg) {
			return fmt.Errorf("invalid npm package name %q", pkg)
		}
	}
	patterns := []string{c.Layout}
	for _, placed := range c.Place {
		patterns = append(patterns, placed)
	}
	for _, pattern := range patterns {
		if !strings.Contains(pattern, "{family}") {
			return fmt.Errorf("a layout pattern names the family with {family}: %q", pattern)
		}
		dir := expand(pattern, "x")
		if dir == "." || dir == ".." || strings.Contains(dir, "\\") || strings.Contains(dir, ":") || strings.HasPrefix(dir, "/") || path.Clean(dir) != dir || strings.HasPrefix(dir, "../") || !pathPattern.MatchString(dir) {
			return fmt.Errorf("invalid output path pattern %q", pattern)
		}
	}
	return nil
}

func expand(pattern, family string) string { return strings.ReplaceAll(pattern, "{family}", family) }

func (c Config) dir(family string) string {
	if placed, ok := c.Place[family]; ok {
		return expand(placed, family)
	}
	return expand(c.Layout, family)
}

// pkg is the package a family's client is published as: its name under
// the scope, as -client.
func (c Config) pkg(family string) string { return c.Scope + "/" + family + "-client" }

func (*target) Name() string { return Name }

func (*target) Consumes() []spi.Concern { return []spi.Concern{spi.Model, spi.Protocol, spi.Live} }

func (t *target) Owns(family string) []string { return []string{t.config.dir(family)} }

// Roots is the directory above every family's package, for the layout and
// each placement.
func (t *target) Roots() []string {
	seen := map[string]bool{spi.PatternRoot(t.config.Layout): true}
	roots := []string{spi.PatternRoot(t.config.Layout)}
	for _, placed := range t.config.Place {
		if root := spi.PatternRoot(placed); !seen[root] {
			seen[root] = true
			roots = append(roots, root)
		}
	}
	sort.Strings(roots)
	return roots
}

// Family answers a path with the family whose package holds it. What a
// package manager installs beside the package, node_modules, is nobody's.
func (t *target) Family(p string) (string, bool) {
	for _, segment := range strings.Split(p, "/") {
		if segment == "node_modules" {
			return "", false
		}
	}
	for family, placed := range t.config.Place {
		if matched, ok := spi.MatchPattern(placed, p); ok && matched == family {
			return family, true
		}
	}
	if family, ok := spi.MatchPattern(t.config.Layout, p); ok {
		if _, placed := t.config.Place[family]; !placed {
			return family, true
		}
	}
	return "", false
}

// Check reports the names the family would make the client declare that
// it cannot: names the generated module declares of itself, identifiers
// TypeScript refuses, collisions among the client's members.
func (t *target) Check(f *render.Family) []diag.Diagnostic {
	if err := t.config.Validate(); err != nil {
		return []diag.Diagnostic{{Family: f.Name, Code: "invalid_config", Message: err.Error()}}
	}
	_, diagnostics := newPlan(f)
	return diagnostics
}

// Render emits the package: src/types.ts, src/index.ts, package.json and
// tsconfig.json.
func (t *target) Render(f *render.Family) ([]spi.File, error) {
	if err := t.config.Validate(); err != nil {
		return nil, err
	}
	p, diagnostics := newPlan(f)
	if len(diagnostics) != 0 {
		return nil, fmt.Errorf("the family does not pass the TypeScript target's check: %s", diagnostics[0])
	}
	dir := t.config.dir(f.Name)
	types := &file{plan: p, family: f, config: t.config, w: emit.NewWriter("  ")}
	emitTypes(types)
	client := &file{plan: p, family: f, config: t.config, w: emit.NewWriter("  "), prefix: "Protocol."}
	protocol := slices.Contains(f.Files, model.ProtocolFile)
	dependencies := map[string]string{t.config.Runtime: t.config.RuntimeVersion}
	if protocol {
		emitClient(client)
		dependencies[t.config.Tunnel] = t.config.RuntimeVersion
	} else {
		client.line("export * from './types.ts';")
	}
	for _, family := range p.references() {
		var dependency string
		switch t.config.Sibling {
		case "workspace":
			dependency = "workspace:*"
		case "version":
			dependency = "0.0.0"
		default:
			relative, err := filepath.Rel(filepath.FromSlash(dir), filepath.FromSlash(t.config.dir(family)))
			if err != nil {
				return nil, fmt.Errorf("locate sibling %s: %w", family, err)
			}
			dependency = "file:" + filepath.ToSlash(relative)
		}
		dependencies[t.config.pkg(family)] = dependency
	}
	manifest, _ := json.Marshal(map[string]any{
		"name": t.config.pkg(f.Name), "version": "0.0.0", "private": true, "type": "module", "exports": "./src/index.ts",
		"scripts": map[string]any{"check": "tsc --noEmit"}, "dependencies": dependencies,
	})
	return []spi.File{
		{Path: path.Join(dir, "src/types.ts"), Data: []byte(spi.Header + types.w.String())},
		{Path: path.Join(dir, "src/index.ts"), Data: []byte(spi.Header + client.w.String())},
		{Path: path.Join(dir, "package.json"), Data: append(manifest, '\n')},
		{Path: path.Join(dir, "tsconfig.json"), Data: []byte("{\"compilerOptions\":{\"target\":\"ES2022\",\"module\":\"NodeNext\",\"moduleResolution\":\"NodeNext\",\"strict\":true,\"skipLibCheck\":true,\"noEmit\":true,\"allowImportingTsExtensions\":true,\"lib\":[\"ES2022\",\"DOM\"]},\"include\":[\"src/**/*.ts\"]}\n")},
	}, nil
}
