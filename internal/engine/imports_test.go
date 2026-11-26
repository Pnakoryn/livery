package engine

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The core knows no harness: only internal/engine imports an adapter package, and no file
// outside internal/adapter/<name> names a harness path or file.
func TestCoreKnowsNoHarness(t *testing.T) {
	t.Parallel()
	adapters := map[string]bool{"kiro": true, "claude": true, "opencode": true}
	root := filepath.Join("..", "..", "internal")
	// Files that hold harness names legitimately are listed here only with a reason; none today.
	fset := token.NewFileSet()
	checked := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		dir := filepath.ToSlash(filepath.Dir(rel))
		f, err := parser.ParseFile(fset, p, nil, 0) // comments are dropped
		if err != nil {
			return err
		}
		checked++
		inAdapter := false
		if name, ok := strings.CutPrefix(dir, "adapter/"); ok {
			inAdapter = adapters[strings.SplitN(name, "/", 2)[0]]
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			v, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if imp, ok := strings.CutPrefix(v, "github.com/Pnakoryn/livery/internal/adapter/"); ok && dir != "engine" && !inAdapter {
				if adapters[strings.SplitN(imp, "/", 2)[0]] {
					t.Errorf("%s imports %s: only internal/engine may import an adapter", p, v)
				}
				return true
			}
			if inAdapter {
				return true
			}
			for _, bad := range []string{".kiro", ".claude", "opencode.json"} {
				if strings.Contains(v, bad) {
					t.Errorf("%s: string %q names a harness (%s) outside its adapter package", p, v, bad)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 10 {
		t.Fatalf("only %d files checked: is the walk root right?", checked)
	}
}
