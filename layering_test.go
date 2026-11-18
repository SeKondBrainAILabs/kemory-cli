package layering_test

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// fileImport is one import of an internal package found in a source file.
type fileImport struct {
	File string // slash-separated path relative to the repo root
	Name string // imported package under internal/, e.g. "api"
}

var backticked = regexp.MustCompile("`([^`]+)`")

// parseLayerTable reads the package table under "## Architecture" and returns
// each package with the set of internal packages it may import.
func parseLayerTable(doc string) map[string]map[string]bool {
	allowed := map[string]map[string]bool{}
	inSection, inTable := false, false
	for _, line := range strings.Split(doc, "\n") {
		line = strings.TrimSuffix(line, "\r")
		switch {
		case !inSection:
			inSection = strings.TrimSpace(line) == "## Architecture"
		case inTable:
			if !strings.HasPrefix(line, "| ") {
				return allowed
			}
			addLayerRow(allowed, line)
		case strings.HasPrefix(line, "| --- |"):
			inTable = true
		case strings.HasPrefix(line, "## "):
			return allowed
		}
	}
	return allowed
}

func addLayerRow(allowed map[string]map[string]bool, row string) {
	cells := strings.Split(strings.Trim(strings.TrimSpace(row), "|"), "|")
	if len(cells) != 2 {
		return
	}
	set := map[string]bool{}
	for _, m := range backticked.FindAllStringSubmatch(cells[1], -1) {
		set[m[1]] = true
	}
	for _, m := range backticked.FindAllStringSubmatch(cells[0], -1) {
		allowed[m[1]] = set
	}
}

// layerViolations compares the imports found in each package with the table.
// Imports are keyed by package; the result has one line per finding.
func layerViolations(allowed map[string]map[string]bool, imports map[string][]fileImport) []string {
	var out []string
	for _, pkg := range sortedKeys(imports) {
		set, ok := allowed[pkg]
		if !ok {
			out = append(out, fmt.Sprintf("%s has no row in the CONTRIBUTING.md layer table", displayPath(pkg)))
			continue
		}
		for _, imp := range imports[pkg] {
			if !set[imp.Name] {
				out = append(out, fmt.Sprintf("%s: %s imports %s, which CONTRIBUTING.md does not allow",
					imp.File, path.Base(pkg), imp.Name))
			}
		}
	}
	for _, pkg := range sortedKeys(allowed) {
		if _, ok := imports[pkg]; !ok {
			out = append(out, fmt.Sprintf("CONTRIBUTING.md lists %s, which does not exist", pkg))
		}
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// displayPath turns a table key back into its directory.
func displayPath(pkg string) string {
	if strings.HasPrefix(pkg, "cmd/") {
		return pkg
	}
	return "internal/" + pkg
}

func modulePath(t *testing.T) string {
	t.Helper()
	f, err := os.Open("go.mod")
	if err != nil {
		t.Fatalf("open go.mod (tests run from the repo root): %v", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	t.Fatal("go.mod has no module line")
	return ""
}

// collectImports parses the imports of every .go file under cmd/ and internal/.
// Every directory with Go files appears in the result, even with no internal
// imports.
func collectImports(t *testing.T, internalPrefix string) map[string][]fileImport {
	t.Helper()
	imports := map[string][]fileImport{}
	fset := token.NewFileSet()
	for _, root := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if filepath.Ext(p) != ".go" {
				return nil
			}
			file := filepath.ToSlash(p)
			pkg := strings.TrimPrefix(path.Dir(file), "internal/")
			parsed, err := parser.ParseFile(fset, p, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			imports[pkg] = append(imports[pkg], internalImports(file, parsed.Imports, internalPrefix)...)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	return imports
}

func internalImports(file string, specs []*ast.ImportSpec, internalPrefix string) []fileImport {
	var out []fileImport
	for _, spec := range specs {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		rest, ok := strings.CutPrefix(p, internalPrefix)
		if !ok {
			continue
		}
		name, _, _ := strings.Cut(rest, "/")
		out = append(out, fileImport{File: file, Name: name})
	}
	return out
}

func TestImportsFollowLayerTable(t *testing.T) {
	doc, err := os.ReadFile("CONTRIBUTING.md")
	if err != nil {
		t.Fatalf("read CONTRIBUTING.md (tests run from the repo root): %v", err)
	}
	allowed := parseLayerTable(string(doc))
	if len(allowed) == 0 {
		t.Fatal("no package table found under \"## Architecture\" in CONTRIBUTING.md")
	}
	imports := collectImports(t, modulePath(t)+"/internal/")
	for _, v := range layerViolations(allowed, imports) {
		t.Error(v)
	}
}

func TestLayerCheckReportsForbiddenImport(t *testing.T) {
	table := "## Architecture\r\n\r\n| Package | May import |\r\n| --- | --- |\r\n" +
		"| `cli` | `api`, `hook` |\r\n| `api`, `hook` | nothing |\r\n| `ghost` | nothing |\r\n\r\ntext\r\n"
	allowed := parseLayerTable(table)
	if len(allowed) != 4 || !allowed["cli"]["hook"] || len(allowed["hook"]) != 0 {
		t.Fatalf("parsed table = %v", allowed)
	}

	imports := map[string][]fileImport{
		"cli":  {{File: "internal/cli/cli.go", Name: "api"}},
		"api":  nil,
		"hook": {{File: "internal/hook/hook.go", Name: "api"}},
		"foo":  nil,
	}
	got := layerViolations(allowed, imports)
	want := []string{
		"internal/foo has no row in the CONTRIBUTING.md layer table",
		"internal/hook/hook.go: hook imports api, which CONTRIBUTING.md does not allow",
		"CONTRIBUTING.md lists ghost, which does not exist",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("violations =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
