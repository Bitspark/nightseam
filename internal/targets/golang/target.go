// Package golang renders a family as Go: a protocol package of wire types
// with their validator, a binding package for a server, a client package
// for a caller. It implements spi.Target and is named nowhere but where
// the tool is composed.
//
// Everything the packages declare is planned before a line is emitted —
// every identifier into the namespace it lands in — so that a collision is
// a diagnostic and the emitters write only what the plan holds.
package golang

import (
	"fmt"
	"go/format"
	"path"
	"regexp"
	"strings"

	"github.com/Bitspark/nightseam/internal/diag"
	"github.com/Bitspark/nightseam/internal/emit"
	"github.com/Bitspark/nightseam/internal/render"
	"github.com/Bitspark/nightseam/internal/spi"
)

// Config places the generated packages. Module is the import path they are
// rooted at and is required; Runtime, Seam and Tunnel are the import paths
// of the runtime they bind to, of the seam beneath it and of the tunnel
// over it, Nightseam's own when left empty; Layout says where each
// package of a family lives, as a pattern over the family's name.
type Config struct {
	Module  string
	Runtime string
	Seam    string
	Tunnel  string
	Layout  Layout
	// Place puts named families elsewhere than the layout says, each at
	// its own layout; a family that refers to one finds it there too.
	Place map[string]Layout
}

// Layout is where a family's packages live, relative to the checkout,
// each a pattern in which {family} is the family's name. The protocol
// package of an imported family is found by the same pattern, so that a
// rendering and what refers to it never disagree.
type Layout struct {
	Protocol string
	Binding  string
	Client   string
}

// The packages the generated ones bind to unless the config names others,
// and where a family's packages live unless the layout says otherwise.
const (
	DefaultRuntime = "github.com/Bitspark/nightseam/runtime/go"
	DefaultSeam    = "github.com/Bitspark/nightseam/duplex/go"
	DefaultTunnel  = "github.com/Bitspark/nightseam/tunnel/go"
)

// DefaultLayout is api/go/<family>-protocol, -binding and -client.
var DefaultLayout = Layout{Protocol: "api/go/{family}-protocol", Binding: "api/go/{family}-binding", Client: "api/go/{family}-client"}

// Name is the target's name: what its override file is called after.
const Name = "go"

// New returns the Go target with its config.
func New(c Config) spi.Target { return &target{c.settled()} }

type target struct{ config Config }

func (c Config) settled() Config {
	if c.Runtime == "" {
		c.Runtime = DefaultRuntime
	}
	if c.Seam == "" {
		c.Seam = DefaultSeam
	}
	if c.Tunnel == "" {
		c.Tunnel = DefaultTunnel
	}
	if c.Layout.Protocol == "" {
		c.Layout.Protocol = DefaultLayout.Protocol
	}
	if c.Layout.Binding == "" {
		c.Layout.Binding = DefaultLayout.Binding
	}
	if c.Layout.Client == "" {
		c.Layout.Client = DefaultLayout.Client
	}
	return c
}

var importPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_./~{}-]*$`)

// Validate reports what is wrong with the config: a missing module, an
// import path Go refuses, a layout pattern that escapes the checkout or
// places two packages in one directory.
func (c Config) Validate() error {
	c = c.settled()
	if c.Module == "" {
		return fmt.Errorf("a Go module path is required to root the generated packages")
	}
	for _, s := range []string{c.Module, c.Runtime, c.Seam, c.Tunnel} {
		if !importPattern.MatchString(s) || strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") || strings.Contains(s, "{") {
			return fmt.Errorf("invalid Go import path %q", s)
		}
	}
	patterns := []string{c.Layout.Protocol, c.Layout.Binding, c.Layout.Client}
	for _, placed := range c.Place {
		patterns = append(patterns, placed.Protocol, placed.Binding, placed.Client)
	}
	for _, pattern := range patterns {
		if !strings.Contains(pattern, "{family}") {
			return fmt.Errorf("a layout pattern names the family with {family}: %q", pattern)
		}
		dir := expand(pattern, "x")
		if dir == "." || dir == ".." || strings.Contains(dir, "\\") || strings.Contains(dir, ":") || strings.HasPrefix(dir, "/") || path.Clean(dir) != dir || strings.HasPrefix(dir, "../") || !importPattern.MatchString(dir) {
			return fmt.Errorf("invalid output path pattern %q", pattern)
		}
	}
	return nil
}

func expand(pattern, family string) string { return strings.ReplaceAll(pattern, "{family}", family) }

func (*target) Name() string { return Name }

func (*target) Consumes() []spi.Concern { return []spi.Concern{spi.Protocol, spi.Session} }

// layout is where a family's packages live: its own placement, or the
// checkout's layout.
func (c Config) layout(family string) Layout {
	if placed, ok := c.Place[family]; ok {
		return placed
	}
	return c.Layout
}

func (t *target) Owns(family string) []string {
	l := t.config.layout(family)
	return []string{expand(l.Protocol, family), expand(l.Binding, family), expand(l.Client, family)}
}

// Check reports the names the family would make Go declare that it
// cannot: identifiers Go refuses, names the generated packages declare of
// themselves, and collisions in the protocol package, the enum constants,
// each record's fields and the client's and remote's members.
func (t *target) Check(f *render.Family) []diag.Diagnostic {
	if err := t.config.Validate(); err != nil {
		return []diag.Diagnostic{{Family: f.Name, Code: "invalid_config", Message: err.Error()}}
	}
	_, diagnostics := newPlan(f)
	return diagnostics
}

// Render emits the four Go files, each through gofmt.
func (t *target) Render(f *render.Family) ([]spi.File, error) {
	if err := t.config.Validate(); err != nil {
		return nil, err
	}
	p, diagnostics := newPlan(f)
	if len(diagnostics) != 0 {
		return nil, fmt.Errorf("the family does not pass the Go target's check: %s", diagnostics[0])
	}
	var files []spi.File
	put := func(dir, file string, source string) error {
		formatted, err := format.Source([]byte(source))
		if err != nil {
			return fmt.Errorf("format generated %s: %w\n%s", file, err, source)
		}
		files = append(files, spi.File{Path: path.Join(dir, file), Data: formatted})
		return nil
	}
	protocolDir, bindingDir, clientDir := t.Owns(f.Name)[0], t.Owns(f.Name)[1], t.Owns(f.Name)[2]
	if err := put(protocolDir, "types_generated.go", t.file(p, f, "protocol", emitTypes)); err != nil {
		return nil, err
	}
	if err := put(protocolDir, "validation_generated.go", t.file(p, f, "protocol", emitValidation)); err != nil {
		return nil, err
	}
	if err := put(bindingDir, "binding_generated.go", t.file(p, f, "binding", emitBinding)); err != nil {
		return nil, err
	}
	if err := put(clientDir, "client_generated.go", t.file(p, f, "client", emitClient)); err != nil {
		return nil, err
	}
	return files, nil
}

// file renders one Go file: the header, the package clause, the imports the
// emitter registered, and the body.
func (t *target) file(p *plan, f *render.Family, suffix string, body func(*file)) string {
	ctx := &file{plan: p, family: f, config: t.config, w: emit.NewWriter("\t"), imports: &emit.Imports{}}
	if suffix != "protocol" {
		ctx.prefix = "protocol."
	}
	body(ctx)
	var out strings.Builder
	out.WriteString(spi.Header)
	fmt.Fprintf(&out, "package %s\n\n", pkg(f.Name, suffix))
	if imports := ctx.imports.Sorted(); len(imports) > 0 {
		out.WriteString("import (\n")
		for _, i := range imports {
			fmt.Fprintf(&out, "\t%s %q\n", i.Alias, i.Path)
		}
		out.WriteString(")\n\n")
	}
	out.WriteString(ctx.w.String())
	return out.String()
}

// pkg is a package's clause name: the family's name without its dashes
// and the package's suffix.
func pkg(family, suffix string) string { return strings.ReplaceAll(family, "-", "") + suffix }

// importAlias is the identifier a generated file refers to an imported
// family's protocol package by.
func importAlias(family string) string { return pkg(family, "protocol") }
