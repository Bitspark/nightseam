package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The surface of a generated Go package is what a consumer can name of it:
// every exported type with its fields and tags, every exported function,
// method, constant and variable with its signature. It is held under
// testdata/surface per generated file, so that a change to how the
// generator spells the packages' API is a diff a reviewer reads, apart from
// the bytes of the files — and so that a second generator can be held to
// the same API as the first, identifier for identifier.
const surfaceRoot = "testdata/surface"

// TestCorpusSurfaceIsGolden: the exported surface of every Go file the corpus
// renders is exactly what testdata/surface holds.
func TestCorpusSurfaceIsGolden(t *testing.T) {
	a := &app{root: corpusRoot, module: module, scope: scope}
	names, err := a.chosen(nil)
	if err != nil {
		t.Fatal(err)
	}
	files, err := a.render(names)
	if err != nil {
		t.Fatal(err)
	}
	surfaces := map[string]string{}
	for p, data := range files {
		if strings.HasSuffix(p, ".go") {
			surfaces[p+".txt"] = strings.Join(goSurface(t, p, string(data)), "\n") + "\n"
		}
	}
	if *update {
		if err := os.RemoveAll(surfaceRoot); err != nil {
			t.Fatal(err)
		}
		for p, text := range surfaces {
			writeFixture(t, surfaceRoot, p, []byte(text))
		}
		t.Logf("rewrote %d surface files under %s", len(surfaces), surfaceRoot)
		return
	}
	for p, text := range surfaces {
		want, err := os.ReadFile(filepath.Join(surfaceRoot, filepath.FromSlash(p)))
		if os.IsNotExist(err) {
			t.Errorf("%s has no surface file; run with -update", p)
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if string(want) != text {
			t.Errorf("the surface of %s changed:\n%s", strings.TrimSuffix(p, ".txt"), diff(string(want), text))
		}
	}
	err = filepath.WalkDir(surfaceRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(surfaceRoot, path)
		if err != nil {
			return err
		}
		if _, rendered := surfaces[filepath.ToSlash(rel)]; !rendered {
			t.Errorf("%s is a surface file nothing renders; run with -update", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// goSurface lists the exported declarations of one Go source file, one line
// each, sorted: types with their type parameters and shape (a struct's
// exported fields with their types and tags), functions and methods with
// their signatures, constants and variables with their types or values.
func goSurface(t *testing.T, name, source string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), name, source, 0)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var out []string
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if !d.Name.IsExported() {
				continue
			}
			receiver := ""
			if d.Recv != nil && len(d.Recv.List) == 1 {
				receiver = "(" + types.ExprString(d.Recv.List[0].Type) + ") "
			}
			out = append(out, "func "+receiver+d.Name.Name+typeParams(d.Type.TypeParams)+strings.TrimPrefix(types.ExprString(d.Type), "func"))
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name.IsExported() {
						out = append(out, "type "+s.Name.Name+typeParams(s.TypeParams)+" "+shape(s.Type))
					}
				case *ast.ValueSpec:
					for i, ident := range s.Names {
						if !ident.IsExported() {
							continue
						}
						line := d.Tok.String() + " " + ident.Name
						if s.Type != nil {
							line += " " + types.ExprString(s.Type)
						}
						if i < len(s.Values) {
							line += " = " + types.ExprString(s.Values[i])
						}
						out = append(out, line)
					}
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// shape spells a declared type: a struct as its exported fields with their
// types and tags, an interface as its methods sorted — a method set has no
// order — anything else as the expression it is.
func shape(expr ast.Expr) string {
	if i, ok := expr.(*ast.InterfaceType); ok {
		var methods []string
		for _, method := range i.Methods.List {
			for _, ident := range method.Names {
				methods = append(methods, ident.Name+strings.TrimPrefix(types.ExprString(method.Type), "func"))
			}
		}
		sort.Strings(methods)
		return "interface{" + strings.Join(methods, "; ") + "}"
	}
	s, ok := expr.(*ast.StructType)
	if !ok {
		return types.ExprString(expr)
	}
	var fields []string
	for _, field := range s.Fields.List {
		tag := ""
		if field.Tag != nil {
			tag = " " + field.Tag.Value
		}
		for _, ident := range field.Names {
			if ident.IsExported() {
				fields = append(fields, ident.Name+" "+types.ExprString(field.Type)+tag)
			}
		}
	}
	return "struct{" + strings.Join(fields, "; ") + "}"
}

func typeParams(list *ast.FieldList) string {
	if list == nil || len(list.List) == 0 {
		return ""
	}
	var params []string
	for _, field := range list.List {
		for _, ident := range field.Names {
			params = append(params, fmt.Sprintf("%s %s", ident.Name, types.ExprString(field.Type)))
		}
	}
	return "[" + strings.Join(params, ", ") + "]"
}
