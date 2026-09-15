package service

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// Every deployment this package sends to the cluster carries the service's
// config files.
//
// Four places build one, and one of them left this out. A running service then
// lost every projected file the moment anything re-applied it, which is how a
// Keycloak lost both its realm import and the script its probes run, while the
// call site looked perfectly reasonable. The omission is invisible by reading,
// so it is checked here instead.
func TestEveryDeploymentCarriesConfigFiles(t *testing.T) {
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
			if !ok || sel.Sel.Name != "WorkloadParams" {
				return true
			}
			found++
			for _, element := range lit.Elts {
				if kv, ok := element.(*ast.KeyValueExpr); ok {
					if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "ConfigFiles" {
						return true
					}
				}
			}
			t.Errorf("%s: a workload built without ConfigFiles. A deploy from here drops the service's projected files.",
				fset.Position(lit.Pos()))
			return true
		})
	}
	if found < 3 {
		t.Fatalf("found %d workloads to check, expected every deployment path", found)
	}
}
