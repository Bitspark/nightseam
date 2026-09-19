package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/internal/analysis"
	"github.com/Bitspark/nightseam/internal/doc"
	"github.com/Bitspark/nightseam/internal/kernel"
	"github.com/Bitspark/nightseam/internal/render"
	"github.com/Bitspark/nightseam/internal/spi"
	"github.com/Bitspark/nightseam/internal/targets/golang"
	"github.com/Bitspark/nightseam/internal/targets/typescript"
)

// Compile every advertised call in its actual generic scope, using the
// generated method's parameter types. This includes inherited operations,
// both Go directions, events and the complete settled-language proof.
func TestLanguageCallsCompile(t *testing.T) {
	root := repositoryRoot(t)
	tsc := fixture(t, root, "go", "node", "tsc")
	for _, corpus := range []string{"corpus/api/contracts", "families/api/contracts"} {
		t.Run(corpus, func(t *testing.T) {
			directory := t.TempDir()
			targets := []spi.Target{golang.New(golang.Config{Module: module}), typescript.New(typescript.Config{Scope: scope})}
			k := kernel.New(targets...)
			world := k.Load(os.DirFS("testdata"), corpus)
			for _, name := range world.Names {
				result, err := k.Render(world, name)
				if err != nil {
					t.Fatal(err)
				}
				writeAll(t, directory, result.Files)
			}
			for _, name := range world.Names {
				f := render.Build(analysis.Resolve(analysis.World(world.Families), name))
				for _, target := range targets {
					files, err := target.Render(f)
					if err != nil {
						t.Fatal(err)
					}
					calls := languageCalls(f, target.(spi.Speller))
					for _, file := range files {
						source := string(file.Data)
						switch {
						case strings.HasSuffix(file.Path, "binding_generated.go"):
							source = goDocumentCalls(t, source, "remote", "Remote", calls)
						case strings.HasSuffix(file.Path, "client_generated.go"):
							source = goDocumentCalls(t, source, "client", "Client", calls)
						case strings.HasSuffix(file.Path, "src/index.ts") && f.HasProtocol():
							source = tsDocumentCalls(t, source, f, calls)
						default:
							continue
						}
						writeFixture(t, directory, file.Path, []byte(source))
					}
				}
			}
			fixtureModule(t, directory, root)
			runFixture(t, directory, "go", "vet", "./...")
			paths := map[string][]string{scope + "/*": {"./api/ts/*/src/index.ts"}}
			for _, component := range []string{"runtime", "duplex", "tunnel", "live"} {
				copyFixtureTree(t, filepath.Join(root, component, "ts"), filepath.Join(directory, component, "ts"))
				paths["@nightseam/"+component] = []string{"./" + component + "/ts/src/index.ts"}
			}
			config, err := json.Marshal(map[string]any{
				"compilerOptions": map[string]any{"target": "ES2022", "module": "NodeNext", "moduleResolution": "NodeNext", "strict": true, "skipLibCheck": true, "noEmit": true, "allowImportingTsExtensions": true, "paths": paths},
				"include":         []string{"api/ts/**/*.ts"},
			})
			if err != nil {
				t.Fatal(err)
			}
			writeFixture(t, directory, "tsconfig.json", config)
			writeFixture(t, directory, "package.json", []byte(`{"type":"module"}`))
			runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
		})
	}
}

func languageCalls(f *render.Family, s spi.Speller) []string {
	var calls []string
	d := doc.Build(f, map[string]spi.Speller{"language": s})
	for _, side := range []struct {
		name       string
		operations doc.Side
	}{{"server", d.Server}, {"client", d.Client}} {
		for _, m := range side.operations.Methods {
			if call := m.Languages["language"].Invoke.Call; call != "" {
				calls = append(calls, call)
			}
		}
		for _, e := range side.operations.Events {
			if call := e.Languages["language"].Invoke.Call; call != "" {
				calls = append(calls, call)
			}
		}
	}
	return calls
}

func goDocumentCalls(t *testing.T, source, receiver, typ string, calls []string) string {
	t.Helper()
	positions := token.NewFileSet()
	parsed, err := parser.ParseFile(positions, "generated.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	text := func(node ast.Node) string {
		return source[positions.Position(node.Pos()).Offset:positions.Position(node.End()).Offset]
	}
	declaration, arguments := "", ""
	methods := map[string]*ast.FuncDecl{}
	for _, d := range parsed.Decls {
		switch d := d.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if spec, ok := spec.(*ast.TypeSpec); ok && spec.Name.Name == typ && spec.TypeParams != nil {
					declaration = text(spec.TypeParams)
					var names []string
					for _, field := range spec.TypeParams.List {
						for _, name := range field.Names {
							names = append(names, name.Name)
						}
					}
					arguments = "[" + strings.Join(names, ", ") + "]"
				}
			}
		case *ast.FuncDecl:
			if d.Recv != nil {
				methods[d.Name.Name] = d
			}
		}
	}
	var out strings.Builder
	out.WriteString(source)
	for i, call := range calls {
		after, ok := strings.CutPrefix(call, receiver+".")
		if !ok {
			continue
		}
		name, _, _ := strings.Cut(after, "(")
		method := methods[name]
		if method == nil {
			t.Fatalf("document calls absent %s.%s", typ, name)
		}
		params := strings.TrimSuffix(strings.TrimPrefix(text(method.Type.Params), "("), ")")
		fmt.Fprintf(&out, "\nfunc documentCall%d%s(%s *%s%s, %s) { %s }\n", i, declaration, receiver, typ, arguments, params, call)
	}
	return out.String()
}

func tsDocumentCalls(t *testing.T, source string, f *render.Family, calls []string) string {
	t.Helper()
	declaration := ""
	for _, line := range strings.Split(source, "\n") {
		if rest, ok := strings.CutPrefix(line, "export class Client"); ok {
			declaration, _, _ = strings.Cut(rest, " implements ")
		}
	}
	var names []string
	seen := map[string]bool{}
	for _, use := range f.Uses {
		if !seen[use.Parameter] {
			names = append(names, use.Parameter)
			seen[use.Parameter] = true
		}
	}
	arguments := ""
	if len(names) != 0 {
		arguments = "<" + strings.Join(names, ", ") + ">"
	}
	var out strings.Builder
	out.WriteString(source)
	for i, call := range calls {
		after, ok := strings.CutPrefix(strings.TrimPrefix(call, "await "), "client.")
		if !ok {
			t.Fatalf("unexpected call %s", call)
		}
		name, params, _ := strings.Cut(after, "(")
		parameter := strings.TrimSuffix(params, ")")
		extra := ""
		if parameter != "" {
			extra = fmt.Sprintf(", %s: Parameters<Client%s[%q]>[0]", parameter, arguments, name)
		}
		fmt.Fprintf(&out, "\nasync function documentCall%d%s(client: Client%s%s) { %s; }\n", i, declaration, arguments, extra, call)
	}
	return out.String()
}
