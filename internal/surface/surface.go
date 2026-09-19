// Package surface describes the exported declarations of generated Go files.
package surface

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strings"
)

// Declarations lists the exported declarations of one Go source file, one line
// each, sorted: types with their type parameters and shape (a struct's
// exported fields with their types and tags), functions and methods with
// their signatures, constants and variables with their types or values.
func Declarations(name, source string) ([]string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), name, source, 0)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
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
	return out, nil
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
