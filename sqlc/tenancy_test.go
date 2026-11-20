// Package sqlc_test checks the queries sqlc generates code from.
package sqlc_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// queryName starts a query: "-- name: GetShipment :one".
var queryName = regexp.MustCompile(`(?m)^-- name: (\w+)`)

// TestEveryQueryIsScopedByTenant (docs/audit-2026-11-19.md, D5): every query
// names tenant_id, or says on the line after its name why it doesn't:
//
//	-- name: GetSessionByToken :one
//	-- tenant: none, a session is found by its token before the company is known.
//
// so that a query that forgot the company can't pass for one that is
// global on purpose.
func TestEveryQueryIsScopedByTenant(t *testing.T) {
	files, err := filepath.Glob("queries/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	modules, err := filepath.Glob("../internal/modules/*/store/queries/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, modules...)
	if len(files) < 20 {
		t.Fatalf("found only %d queries files", len(files))
	}
	queries := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		starts := queryName.FindAllStringSubmatchIndex(src, -1)
		for i, m := range starts {
			end := len(src)
			if i+1 < len(starts) {
				end = starts[i+1][0]
			}
			name, body := src[m[2]:m[3]], src[m[1]:end]
			queries++
			if why, ok := tenantNone(body); ok {
				if len(why) < 10 {
					t.Errorf("%s: %s says it isn't scoped by tenant without saying why", f, name)
				}
				continue
			}
			if !strings.Contains(stripComments(body), "tenant_id") {
				t.Errorf("%s: %s doesn't name tenant_id: scope it, or say why not with \"-- tenant: none, <reason>\" on the line after its name", f, name)
			}
		}
	}
	if queries < 300 {
		t.Errorf("found only %d queries", queries)
	}
}

// tenantNone finds "-- tenant: none, <reason>" on the line after the name.
func tenantNone(body string) (string, bool) {
	_, rest, _ := strings.Cut(body, "\n")
	line, _, _ := strings.Cut(rest, "\n")
	why, ok := strings.CutPrefix(strings.TrimSpace(line), "-- tenant: none,")
	return strings.TrimSpace(why), ok
}

// stripComments drops "--" comments, so a comment that mentions tenant_id
// doesn't count as scoping the query.
func stripComments(sql string) string {
	var b strings.Builder
	for line := range strings.Lines(sql) {
		code, _, _ := strings.Cut(line, "--")
		b.WriteString(code)
		b.WriteString("\n")
	}
	return b.String()
}
