// Package atlas writes a checkout's document as one self-contained,
// interactive HTML page. It knows the document, not the declaration
// resolver or any language target's spelling rules.
package atlas

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"sort"
	"strings"
	"text/template"

	"github.com/Bitspark/nightseam/internal/doc"
	"github.com/Bitspark/nightseam/internal/spi"
)

// Name identifies this document writer and its checkout config section.
const Name = "atlas"

// Config sets the page's title, design tokens and optional webfonts.
// With Webfonts false, the page uses local fonts and makes no requests.
type Config struct {
	Tokens   map[string]string
	Webfonts bool
	Title    string
}

var tokens = strings.Fields(`--bg --surface --hover --ink --ink-2 --hair --hair-2
--lamp --moon --lamp-tint --moon-tint --lamp-line --moon-line --scrim --shadow
--serif --mono --measure --cl --cr`)

// Validate refuses unknown tokens and values that escape a CSS declaration
// or introduce a remote resource. Tokens remain values, never stylesheets.
func (c Config) Validate() error {
	keys := make([]string, 0, len(c.Tokens))
	for key := range c.Tokens {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		known := false
		for _, token := range tokens {
			known = known || key == token
		}
		if !known {
			return fmt.Errorf("unknown atlas token %q", key)
		}
		value := c.Tokens[key]
		lower := strings.ToLower(value)
		if strings.TrimSpace(value) == "" || strings.ContainsAny(value, ";{}<>\\\n\r\x00@") || strings.Contains(lower, "url(") || strings.Contains(lower, "image(") || strings.Contains(lower, "image-set(") || strings.Contains(lower, "expression(") || strings.Contains(lower, "/*") {
			return fmt.Errorf("atlas token %q must be a CSS value without a resource URL", key)
		}
	}
	return nil
}

//go:embed template.html style.css atlas.mjs page.mjs
var assets embed.FS

var page = template.Must(template.New("atlas").Parse(readAsset("template.html")))

func readAsset(name string) string {
	data, err := assets.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// New returns the atlas writer. Config is validated again when it writes.
func New(config Config) doc.Writer { return &writer{config: config} }

type writer struct{ config Config }

func (*writer) Name() string                           { return Name }
func (*writer) Layout() doc.Layout                     { return doc.Layout{Checkout: []string{"api/spec/index.html"}} }
func (*writer) Family(*doc.Family) ([]spi.File, error) { return nil, nil }

func (w *writer) Checkout(checkout *doc.Checkout) ([]spi.File, error) {
	if err := w.config.Validate(); err != nil {
		return nil, err
	}
	// encoding/json escapes every '<', including those in RawMessage, so
	// declarations can never close the application/json script element.
	data, err := json.Marshal(checkout)
	if err != nil {
		return nil, err
	}
	title := w.config.Title
	if title == "" {
		title = "Protocol atlas"
	}
	var overrides strings.Builder
	if len(w.config.Tokens) > 0 {
		overrides.WriteString(":root, :root[data-theme] {\n")
		for _, token := range tokens {
			if value, ok := w.config.Tokens[token]; ok {
				fmt.Fprintf(&overrides, "  %s: %s;\n", token, value)
			}
		}
		overrides.WriteString("}\n")
	}
	fonts := ""
	fontTokens := ""
	if w.config.Webfonts {
		fonts = `<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500&amp;family=Newsreader:ital,wght@0,400;0,500;1,400&amp;display=swap">`
		fontTokens = ":root { --serif: Newsreader, 'Iowan Old Style', Palatino, Georgia, serif; --mono: 'IBM Plex Mono', ui-monospace, Menlo, monospace; }\n"
	}
	var out bytes.Buffer
	err = page.Execute(&out, struct{ Title, Fonts, Style, Tokens, Document, Builders, Page string }{
		html.EscapeString(title), fonts, readAsset("style.css"), fontTokens + overrides.String(), string(data), readAsset("atlas.mjs"), readAsset("page.mjs"),
	})
	if err != nil {
		return nil, err
	}
	return []spi.File{{Path: "api/spec/index.html", Data: out.Bytes()}}, nil
}
