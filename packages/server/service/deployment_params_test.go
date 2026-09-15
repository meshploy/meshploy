package service

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// What a service has attached reaches the cluster from every path that builds
// its deployment.
//
// Four places build one, and two of them had gaps. Re-applying dropped the
// service's config files, so a running service lost its projected files the
// moment anything touched it. A rollback dropped its volumes, its probes and
// its pull secret, bringing the service back with no storage and no health
// checks. Both call sites read as complete, which is why this is checked by
// machine rather than by eye.
func TestEveryDeploymentCarriesWhatTheServiceHasAttached(t *testing.T) {
	required := []string{
		"ConfigFiles",
		"VolumeMounts",
		"LivenessProbe",
		"ReadinessProbe",
		"ImagePullSecretName",
		"Env",
		"Ports",
	}
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
			set := map[string]bool{}
			for _, element := range lit.Elts {
				if kv, ok := element.(*ast.KeyValueExpr); ok {
					if key, ok := kv.Key.(*ast.Ident); ok {
						set[key.Name] = true
					}
				}
			}
			for _, field := range required {
				if !set[field] {
					t.Errorf("%s: a workload built without %s. A deploy from here drops it from the running service.",
						fset.Position(lit.Pos()), field)
				}
			}
			return true
		})
	}
	if found < 3 {
		t.Fatalf("found %d workloads to check, expected every deployment path", found)
	}
}
