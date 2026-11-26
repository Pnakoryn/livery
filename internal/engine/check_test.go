package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/engine/enginetest"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func findings(t *testing.T, dir string) string {
	t.Helper()
	var out []string
	for _, l := range Check(enginetest.Env(t), dir) {
		if l.Status == "finding" {
			out = append(out, l.Check+": "+l.Detail)
		}
	}
	return strings.Join(out, "\n")
}

func TestCheckBaseNotDirectory(t *testing.T) {
	t.Parallel()
	dir := writeTree(t, map[string]string{"livery.json": `{"schema":1}`, "base": "x"})
	if got := findings(t, dir); !strings.Contains(got, "layer base") || !strings.Contains(got, "not a directory") {
		t.Fatalf("findings = %q", got)
	}
}

func TestCheckAbsentBaseIsOK(t *testing.T) {
	t.Parallel()
	dir := writeTree(t, map[string]string{"livery.json": `{"schema":1}`})
	if got := findings(t, dir); got != "" {
		t.Fatalf("findings = %q", got)
	}
}

func TestCheckNeutralNativeCollision(t *testing.T) {
	t.Parallel()
	dir := writeTree(t, map[string]string{
		"livery.json":                 `{"schema":1}`,
		"base/instructions/style.md":  "a\n",
		"base/kiro/steering/style.md": "b\n",
	})
	got := findings(t, dir)
	if !strings.Contains(got, "render kiro") || !strings.Contains(got, "wanted twice") {
		t.Fatalf("findings = %q", got)
	}
}
