// Package typescript renders a family as TypeScript: a client package
// with shared types and validators, and a binding package for the server.
// It implements spi.Target and is named nowhere
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

// Config places the generated packages. Scope is the npm scope they are
// published under and is required; Runtime and Tunnel are the packages it
// binds to and RuntimeVersion their version, Nightseam's own when left
// empty; Layout says where a family's packages live, as patterns over the
// family's name; Place puts named families elsewhere. Sibling selects how
// generated packages refer to one another: file, workspace or version.
type Config struct {
	Scope          string
	Runtime        string
	Tunnel         string
	Live           string
	RuntimeVersion string
	Layout         Layout
	Place          map[string]Layout
	Sibling        string
}

// Layout places the two generated roles independently. Both packages share
// the client package's protocol types and live boundary helpers.
type Layout struct {
	Client  string
	Binding string
}

// The packages the generated one binds to unless the config names others,
// and where a family's package lives unless the layout says otherwise.
const (
	DefaultRuntime        = "@nightseam/runtime"
	DefaultTunnel         = "@nightseam/tunnel"
	DefaultLive           = "@nightseam/live"
	DefaultRuntimeVersion = "0.5.0"
	DefaultSibling        = "file"
)

var DefaultLayout = Layout{Client: "api/ts/{family}-client", Binding: "api/ts/{family}-binding"}

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
	if c.Layout.Client == "" {
		c.Layout.Client = DefaultLayout.Client
	}
	if c.Layout.Binding == "" {
		c.Layout.Binding = DefaultLayout.Binding
	}
	if c.Place != nil {
		placed := make(map[string]Layout, len(c.Place))
		for family, layout := range c.Place {
			if layout.Client == "" {
				layout.Client = c.Layout.Client
			}
			if layout.Binding == "" {
				layout.Binding = c.Layout.Binding
			}
			placed[family] = layout
		}
		c.Place = placed
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
	for _, pkg := range []string{c.Runtime, c.Tunnel, c.Live} {
		if !packagePattern.MatchString(pkg) {
			return fmt.Errorf("invalid npm package name %q", pkg)
		}
	}
	patterns := []string{c.Layout.Client, c.Layout.Binding}
	for _, placed := range c.Place {
		patterns = append(patterns, placed.Client, placed.Binding)
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
	for family, layout := range c.Place {
		if expand(layout.Client, family) == expand(layout.Binding, family) {
			return fmt.Errorf("TypeScript client and binding occupy the same directory for %s", family)
		}
	}
	if c.Layout.Client == c.Layout.Binding {
		return fmt.Errorf("TypeScript client and binding occupy the same directory")
	}
	return nil
}

func expand(pattern, family string) string { return strings.ReplaceAll(pattern, "{family}", family) }

func (c Config) dir(family string) string {
	if placed, ok := c.Place[family]; ok {
		return expand(placed.Client, family)
	}
	return expand(c.Layout.Client, family)
}

func (c Config) bindingDir(family string) string {
	if placed, ok := c.Place[family]; ok {
		return expand(placed.Binding, family)
	}
	return expand(c.Layout.Binding, family)
}

func (c Config) bindingPkg(family string) string { return c.Scope + "/" + family + "-binding" }

func (c Config) dependency(from, to string) (string, error) {
	switch c.Sibling {
	case "workspace":
		return "workspace:*", nil
	case "version":
		return "0.0.0", nil
	default:
		relative, err := filepath.Rel(filepath.FromSlash(from), filepath.FromSlash(to))
		if err != nil {
			return "", err
		}
		return "file:" + filepath.ToSlash(relative), nil
	}
}

// pkg is the package a family's client is published as: its name under
// the scope, as -client.
func (c Config) pkg(family string) string { return c.Scope + "/" + family + "-client" }

func (*target) Name() string { return Name }

func (*target) Consumes() []spi.Concern { return []spi.Concern{spi.Model, spi.Protocol, spi.Live} }

func (t *target) Owns(family string) []string {
	return []string{t.config.dir(family), t.config.bindingDir(family)}
}

// Roots is the directory above every family's package, for the layout and
// each placement.
func (t *target) Roots() []string {
	seen := map[string]bool{}
	var roots []string
	add := func(layout Layout) {
		for _, pattern := range []string{layout.Client, layout.Binding} {
			if root := spi.PatternRoot(pattern); !seen[root] {
				seen[root] = true
				roots = append(roots, root)
			}
		}
	}
	add(t.config.Layout)
	for _, placed := range t.config.Place {
		add(placed)
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
	match := func(pattern string) (string, bool) {
		// MatchPattern reads the family from its path segment. The layout
		// may continue below that segment, so also check the entire directory.
		before, after, _ := strings.Cut(pattern, "{family}")
		suffix, _, _ := strings.Cut(after, "/")
		family, ok := spi.MatchPattern(before+"{family}"+suffix, p)
		if !ok {
			return "", false
		}
		dir := expand(pattern, family)
		return family, p == dir || strings.HasPrefix(p, dir+"/")
	}
	for family, placed := range t.config.Place {
		for _, pattern := range []string{placed.Client, placed.Binding} {
			if matched, ok := match(pattern); ok && matched == family {
				return family, true
			}
		}
	}
	for _, pattern := range []string{t.config.Layout.Client, t.config.Layout.Binding} {
		if family, ok := match(pattern); ok {
			if _, placed := t.config.Place[family]; !placed {
				return family, true
			}
		}
	}
	return "", false
}

// Check reports the names the family would make either role declare that
// it cannot: names the generated module declares of itself, identifiers
// TypeScript refuses, and collisions among a role's members.
func (t *target) Check(f *render.Family) []diag.Diagnostic {
	if err := t.config.Validate(); err != nil {
		return []diag.Diagnostic{{Family: f.Name, Code: "invalid_config", Message: err.Error()}}
	}
	_, diagnostics := newPlan(f)
	return diagnostics
}

// Render emits the client and shared types, plus the server binding when
// the family declares a protocol. Each role has its own package manifest.
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
	if f.Live {
		dependencies[t.config.Live] = t.config.RuntimeVersion
	}
	if protocol {
		emitClient(client)
		dependencies["@nightseam/duplex"] = t.config.RuntimeVersion
	} else {
		client.line("export * from './types.ts';")
	}
	for _, family := range p.references() {
		dependency, err := t.config.dependency(dir, t.config.dir(family))
		if err != nil {
			return nil, fmt.Errorf("locate sibling %s: %w", family, err)
		}
		dependencies[t.config.pkg(family)] = dependency
	}
	manifest, _ := json.Marshal(map[string]any{
		"name": t.config.pkg(f.Name), "version": "0.0.0", "private": true, "type": "module", "exports": map[string]string{".": "./src/index.ts", "./types": "./src/types.ts"},
		"scripts": map[string]any{"check": "tsc --noEmit"}, "dependencies": dependencies,
	})
	files := []spi.File{
		{Path: path.Join(dir, "src/types.ts"), Data: []byte(spi.Header + types.w.String())},
		{Path: path.Join(dir, "src/index.ts"), Data: []byte(spi.Header + client.w.String())},
		{Path: path.Join(dir, "package.json"), Data: append(manifest, '\n')},
		{Path: path.Join(dir, "tsconfig.json"), Data: []byte("{\"compilerOptions\":{\"target\":\"ES2022\",\"module\":\"NodeNext\",\"moduleResolution\":\"NodeNext\",\"strict\":true,\"skipLibCheck\":true,\"noEmit\":true,\"allowImportingTsExtensions\":true,\"lib\":[\"ES2022\",\"DOM\"]},\"include\":[\"src/**/*.ts\"]}\n")},
	}
	if protocol {
		binding := &file{plan: p, family: f, config: t.config, w: emit.NewWriter("  "), prefix: "Protocol."}
		emitBinding(binding)
		bindingDir := t.config.bindingDir(f.Name)
		clientDependency, err := t.config.dependency(bindingDir, dir)
		if err != nil {
			return nil, fmt.Errorf("locate binding protocol: %w", err)
		}
		bindingDependencies := map[string]string{t.config.Runtime: t.config.RuntimeVersion, "@nightseam/duplex": t.config.RuntimeVersion, t.config.pkg(f.Name): clientDependency}
		if f.Live {
			bindingDependencies[t.config.Live] = t.config.RuntimeVersion
		}
		for _, family := range p.references() {
			dependency, err := t.config.dependency(bindingDir, t.config.dir(family))
			if err != nil {
				return nil, fmt.Errorf("locate binding sibling %s: %w", family, err)
			}
			bindingDependencies[t.config.pkg(family)] = dependency
		}
		bindingManifest, _ := json.Marshal(map[string]any{
			"name": t.config.bindingPkg(f.Name), "version": "0.0.0", "private": true, "type": "module", "exports": "./src/index.ts",
			"scripts": map[string]any{"check": "tsc --noEmit"}, "dependencies": bindingDependencies,
		})
		files = append(files,
			spi.File{Path: path.Join(bindingDir, "src/index.ts"), Data: []byte(spi.Header + binding.w.String())},
			spi.File{Path: path.Join(bindingDir, "package.json"), Data: append(bindingManifest, '\n')},
			spi.File{Path: path.Join(bindingDir, "tsconfig.json"), Data: files[3].Data})
	}
	return files, nil
}
