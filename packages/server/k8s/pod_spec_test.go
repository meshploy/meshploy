package k8s

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// Every pod this package builds turns Kubernetes' service-link variables off.
//
// There are seven pod specs here, and a new one inherits the default unless it
// says otherwise, which is how a container ends up with a variable per service
// in its namespace. Neo4j refused to start over exactly that.
func TestEveryPodSpecDisablesServiceLinks(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	found := 0
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			sel, ok := lit.Type.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "PodSpec" {
				return true
			}
			found++
			for _, element := range lit.Elts {
				if kv, ok := element.(*ast.KeyValueExpr); ok {
					if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "EnableServiceLinks" {
						return true
					}
				}
			}
			t.Errorf("%s: a pod built without EnableServiceLinks. Its containers get a variable per service in the namespace.",
				fset.Position(lit.Pos()))
			return true
		})
	}
	if found < 7 {
		t.Fatalf("found %d pod specs, expected every one in the package", found)
	}
}
