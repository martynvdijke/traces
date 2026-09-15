package main

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// TestInternalPackagesAreLeafward enforces the package-architecture invariant:
// the internal/ import graph stays acyclic and leaf-ward. An internal package
// must never import the root main package (module path "traces"), and a cycle
// anywhere in the module would make `go list ./...` fail.
func TestInternalPackagesAreLeafward(t *testing.T) {
	if out, err := exec.Command("go", "list", "./...").CombinedOutput(); err != nil {
		t.Fatalf("go list ./... failed (import cycle?): %v\n%s", err, out)
	}

	out, err := exec.Command("go", "list", "-json", "./internal/...").Output()
	if err != nil {
		t.Fatalf("go list -json ./internal/...: %v", err)
	}

	dec := json.NewDecoder(strings.NewReader(string(out)))
	seen := 0
	for dec.More() {
		var pkg struct {
			ImportPath  string
			Imports     []string
			TestImports []string
		}
		if err := dec.Decode(&pkg); err != nil {
			t.Fatalf("decode go list output: %v", err)
		}
		seen++
		for _, imp := range append(append([]string{}, pkg.Imports...), pkg.TestImports...) {
			if imp == "traces" {
				t.Errorf("%s imports the root main package %q (leaf-ward rule violated)", pkg.ImportPath, imp)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no internal/ packages found; expected at least internal/models")
	}
}
