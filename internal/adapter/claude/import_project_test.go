package claude

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/content"
)

func projectEnv(t *testing.T) (adapter.Env, string) {
	t.Helper()
	proj := t.TempDir()
	return adapter.Env{Home: t.TempDir(), Project: proj, Getenv: func(string) string { return "" }}, proj
}

func TestImportProjectSkipsAndServers(t *testing.T) {
	e, proj := projectEnv(t)
	put(t, filepath.Join(proj, "CLAUDE.md"), "notes", 0o644)
	put(t, filepath.Join(proj, ".claude", "settings.json"), "{}", 0o644)
	found, skipped := doImport(t, e)
	if len(found) != 0 || len(skipped) != 2 {
		t.Fatalf("found %v skipped %v", found, skipped)
	}
	if !skippedHas(skipped, "CLAUDE.md", "livery does not manage them") || !skippedHas(skipped, "settings.json", "not supported") {
		t.Errorf("skipped %v", skipped)
	}
	put(t, filepath.Join(proj, ".mcp.json"), `{"mcpServers":{"docs":{"url":"https://d.example/mcp"}}}`, 0o644)
	found, _ = doImport(t, e)
	if find(found, content.MCP, "docs") == nil {
		t.Errorf("found %v", found)
	}
}

func TestImportProjectLinkedSkill(t *testing.T) {
	e, proj := projectEnv(t)
	target := t.TempDir()
	put(t, filepath.Join(target, "SKILL.md"), "s", 0o644)
	if err := os.MkdirAll(filepath.Join(proj, ".claude", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(proj, ".claude", "skills", "linked")); err != nil {
		t.Fatal(err)
	}
	found, skipped := doImport(t, e)
	if len(found) != 0 || !skippedHas(skipped, "linked", "symbolic link") {
		t.Errorf("found %v skipped %v", found, skipped)
	}
}

func TestImportProjectLinkedHarnessFolder(t *testing.T) {
	e, proj := projectEnv(t)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "skills", "s"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "skills", "s", "SKILL.md"), []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(proj, ".claude")); err != nil {
		t.Fatal(err)
	}
	found, skipped := doImport(t, e)
	if len(found) != 0 || len(skipped) != 1 || skipped[0].Reason != symlinkReason {
		t.Errorf("found %v skipped %v", found, skipped)
	}
}
