// Package modules_test checks the modules' boundaries
// (docs/architecture.md, section 4; docs/migration.md, section 5).
package modules_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const module = "github.com/kiflet/backend/"

// TestModulesKeepToTheirBoundaries: a module's packages may import the
// platform packages (internal/platform, pkg), their own module's packages
// and other modules' root packages, which are their interfaces, and
// nothing else of this repository: not another module's app or store, and
// not the code the modules are being moved out of (internal/service,
// internal/db, internal/model, internal/web, internal/app).
func TestModulesKeepToTheirBoundaries(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	modules := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		modules++
		name := e.Name()
		err := filepath.WalkDir(name, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imp := range f.Imports {
				p, _ := strconv.Unquote(imp.Path.Value)
				if !allowed(name, p) {
					t.Errorf("%s imports %s", filepath.Join("internal/modules", path), p)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if modules == 0 {
		t.Fatal("no modules found")
	}
}

// allowed says whether module name may import path.
func allowed(name, path string) bool {
	rest, ours := strings.CutPrefix(path, module)
	if !ours {
		return true // the standard library and third parties
	}
	switch {
	case strings.HasPrefix(rest, "internal/platform/"), strings.HasPrefix(rest, "pkg/"):
		return true
	case rest == "internal/modules/"+name, strings.HasPrefix(rest, "internal/modules/"+name+"/"):
		return true
	}
	other, isModule := strings.CutPrefix(rest, "internal/modules/")
	return isModule && !strings.Contains(other, "/")
}

func TestAllowed(t *testing.T) {
	for path, want := range map[string]bool{
		"context":                               true,
		"github.com/jackc/pgx/v5":               true,
		module + "internal/platform/database":   true,
		module + "pkg/metrics":                  true,
		module + "internal/modules/audit":       true,
		module + "internal/modules/audit/store": true,
		module + "internal/modules/tenancy":     true,
		module + "internal/modules/tenancy/app": false,
		module + "internal/db":                  false,
		module + "internal/service":             false,
		module + "internal/model":               false,
	} {
		if got := allowed("audit", path); got != want {
			t.Errorf("audit importing %s: %v, want %v", path, got, want)
		}
	}
}
