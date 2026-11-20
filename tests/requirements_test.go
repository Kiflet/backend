// Package tests holds what checks the tests themselves; the suites are in
// its directories.
package tests

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "write docs/requirements.md")

var (
	requirementRe = regexp.MustCompile(`(?m)^### (FR-\d{3}): (.+)$`)
	namedRe       = regexp.MustCompile(`\b(?:FR|AC)-(\d{3})\b`)
	notBuiltRe    = regexp.MustCompile(`(?m)^- \*\*FR-(\d{3})`)
	operationsRe  = regexp.MustCompile(`\(operations[;)]`)
)

// A test, and what its doc comment says it covers.
type namedTest struct {
	name, suite string
	covers      []string // FR numbers
	operations  bool
}

// TestRequirements (audit T4): every integration and end-to-end test names
// in its doc comment the requirement it covers, by its number or one of
// its acceptance criteria's, or marks itself as a test of operations,
// which no requirement covers. docs/requirements.md, the table of which tests
// cover each requirement and of those none does, is generated from them:
// make requirements writes it.
func TestRequirements(t *testing.T) {
	doc, err := os.ReadFile("../docs/functional_requirements.md")
	if err != nil {
		t.Fatal(err)
	}
	titles := map[string]string{}
	var order []string
	for _, m := range requirementRe.FindAllStringSubmatch(string(doc), -1) {
		titles[m[1]] = m[2]
		order = append(order, m[1])
	}
	if len(order) == 0 {
		t.Fatal("no requirements found in docs/functional_requirements.md")
	}
	// Appendix D says which are, in part, not built: an entry each.
	notBuilt := map[string]bool{}
	if _, rest, ok := strings.Cut(string(doc), "### Not built"); ok {
		section, _, _ := strings.Cut(rest, "\n### ")
		for _, m := range notBuiltRe.FindAllStringSubmatch(section, -1) {
			notBuilt["FR-"+m[1]] = true
		}
	}

	tests := collectTests(t)
	for _, tc := range tests {
		for _, fr := range tc.covers {
			if titles[fr] == "" {
				t.Errorf("%s names %s, which docs/functional_requirements.md doesn't have", tc.name, fr)
			}
		}
		if (tc.suite == "integration" || tc.suite == "end to end") && len(tc.covers) == 0 && !tc.operations {
			t.Errorf("%s (%s) names no requirement: say in its doc comment which (FR-nnn), or (operations)", tc.name, tc.suite)
		}
	}

	got := requirementsTable(order, titles, notBuilt, tests)
	const path = "../docs/requirements.md"
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil { //nolint:gosec // a document in the repository
			t.Fatal(err)
		}
		return
	}
	if want, err := os.ReadFile(path); err != nil || string(want) != got {
		t.Errorf("docs/requirements.md is out of date: run make requirements")
	}
}

// collectTests reads every test function in the repository, whatever its
// build tags, with what its doc comment names.
func collectTests(t *testing.T) []namedTest {
	t.Helper()
	var tests []namedTest
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		dir := filepath.ToSlash(filepath.Dir(strings.TrimPrefix(path, ".."+string(filepath.Separator))))
		suite := "unit"
		switch {
		case strings.HasPrefix(dir, "tests/integration"):
			suite = "integration"
		case strings.HasPrefix(dir, "tests/e2e"):
			suite = "end to end"
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Test") || fn.Name.Name == "TestMain" {
				continue
			}
			doc := fn.Doc.Text()
			tc := namedTest{name: fn.Name.Name, suite: suite, operations: operationsRe.MatchString(doc)}
			for _, m := range namedRe.FindAllStringSubmatch(doc, -1) {
				if fr := "FR-" + m[1]; !slices.Contains(tc.covers, fr) {
					tc.covers = append(tc.covers, fr)
				}
			}
			tests = append(tests, tc)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tests
}

// requirementsTable is docs/requirements.md.
func requirementsTable(order []string, titles map[string]string, notBuilt map[string]bool, tests []namedTest) string {
	suites := []string{"integration", "end to end", "unit"}
	by := map[string]map[string][]string{} // requirement, suite: tests
	var operations []namedTest
	for _, tc := range tests {
		if tc.operations {
			operations = append(operations, tc)
		}
		for _, fr := range tc.covers {
			if by[fr] == nil {
				by[fr] = map[string][]string{}
			}
			by[fr][tc.suite] = append(by[fr][tc.suite], tc.name)
		}
	}

	var b strings.Builder
	b.WriteString(`# Requirements and their tests

Which tests cover each requirement of
[functional_requirements.md](functional_requirements.md), and which
requirements no test covers. Generated from the tests' doc comments by
` + "`make requirements`" + `; ` + "`go test ./tests/`" + ` fails when it is out of date.

A test names what it covers in its doc comment: a requirement (FR-006) or
one of its acceptance criteria (AC-006.2), or "(operations)" for what no
requirement covers, such as backups and the broker. Every integration and
end-to-end test names one; unit tests may.

## By requirement

| Requirement | Integration | End to end | Unit |
|:--|:--|:--|:--|
`)
	var none []string
	for _, fr := range order {
		title := fr + " " + titles[fr]
		if notBuilt[fr] {
			title += " (in part not built: Appendix D)"
		}
		if by[fr] == nil {
			none = append(none, title)
		}
		cells := []string{title}
		for _, s := range suites {
			names := by[fr][s]
			slices.Sort(names)
			cells = append(cells, strings.Join(names, ", "))
		}
		fmt.Fprintf(&b, "| %s |\n", strings.Join(cells, " | "))
	}

	b.WriteString("\n## Requirements no test names\n\n")
	if len(none) == 0 {
		b.WriteString("None.\n")
	}
	for _, r := range none {
		fmt.Fprintf(&b, "- %s\n", r)
	}

	b.WriteString("\n## Tests of operations\n\n| Test | Suite |\n|:--|:--|\n")
	slices.SortFunc(operations, func(x, y namedTest) int { return strings.Compare(x.name, y.name) })
	for _, tc := range operations {
		fmt.Fprintf(&b, "| %s | %s |\n", tc.name, tc.suite)
	}
	return b.String()
}
