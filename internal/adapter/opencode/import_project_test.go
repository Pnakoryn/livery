package opencode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pnakoryn/livery/internal/adapter"
	"github.com/Pnakoryn/livery/internal/content"
)

func projEnv(t *testing.T) (adapter.Env, string) {
	t.Helper()
	proj := t.TempDir()
	return adapter.Env{Home: t.TempDir(), Project: proj, Getenv: func(string) string { return "" }}, proj
}

func TestImportProjectInstructions(t *testing.T) {
	e, proj := projEnv(t)
	write(t, filepath.Join(proj, "docs", "rules.md"), "rules\n")
	write(t, filepath.Join(proj, "AGENTS.md"), "notes")
	write(t, filepath.Join(proj, "opencode.json"),
		`{"instructions": ["docs/rules.md", "docs/*.md", ".opencode/livery/instructions/*.md", "../out.md"]}`)
	found, skipped, err := runImport(t, e)
	if err != nil {
		t.Fatal(err)
	}
	if names(found, content.InstructionKind) != "rules" || found[0].Item.Instruction.Body != "rules\n" ||
		found[0].Paths[0] != filepath.Join(proj, "docs", "rules.md") {
		t.Errorf("found %+v", found)
	}
	var glob, notes, outside bool
	for _, s := range skipped {
		glob = glob || strings.Contains(s.Reason, "a glob")
		notes = notes || strings.Contains(s.Reason, "livery does not manage them")
		outside = outside || strings.Contains(s.Reason, "outside the project")
	}
	if len(skipped) != 3 || !glob || !notes || !outside {
		t.Errorf("skipped %v", skipped)
	}
}

func TestImportProjectLinkedSkill(t *testing.T) {
	e, proj := projEnv(t)
	target := t.TempDir()
	write(t, filepath.Join(target, "SKILL.md"), "s")
	if err := os.MkdirAll(filepath.Join(proj, ".opencode", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(proj, ".opencode", "skills", "linked")); err != nil {
		t.Fatal(err)
	}
	found, skipped, err := runImport(t, e)
	if err != nil || len(found) != 0 || len(skipped) != 1 || skipped[0].Reason != symlinkReason {
		t.Errorf("found %v skipped %v err %v", found, skipped, err)
	}
}

func TestImportProjectLinkedFolderInstruction(t *testing.T) {
	e, proj := projEnv(t)
	outside := t.TempDir()
	write(t, filepath.Join(outside, "secret.md"), "outside")
	if err := os.Symlink(outside, filepath.Join(proj, "docs")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(proj, "opencode.json"), `{"instructions": ["docs/secret.md"]}`)
	found, skipped, err := runImport(t, e)
	if err != nil || len(found) != 0 || len(skipped) != 1 || skipped[0].Reason != symlinkReason {
		t.Errorf("found %v skipped %v err %v", found, skipped, err)
	}
}

func TestImportProjectHomeInstruction(t *testing.T) {
	e, proj := projEnv(t)
	write(t, filepath.Join(proj, "opencode.json"), `{"instructions": ["~/notes.md"]}`)
	found, skipped, err := runImport(t, e)
	if err != nil || len(found) != 0 || len(skipped) != 1 || !strings.Contains(skipped[0].Reason, "outside the project") {
		t.Errorf("found %v skipped %v err %v", found, skipped, err)
	}
}

func TestImportProjectLinkedHarnessFolder(t *testing.T) {
	e, proj := projEnv(t)
	outside := t.TempDir()
	write(t, filepath.Join(outside, "skills", "s", "SKILL.md"), "s")
	write(t, filepath.Join(outside, "opencode.json"), `{"mcp": {"servers": {"x": {"command": "c"}}}}`)
	if err := os.Symlink(outside, filepath.Join(proj, ".opencode")); err != nil {
		t.Fatal(err)
	}
	found, skipped, err := runImport(t, e)
	if err != nil || len(found) != 0 || len(skipped) != 1 || skipped[0].Reason != symlinkReason {
		t.Errorf("found %v skipped %v err %v", found, skipped, err)
	}
}
